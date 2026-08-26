//go:build e2e

package postgres

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/application"
	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/policy"
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestPhase5ResourceLifecycleWorkflow qualifies the complete delegated-resource
// lifecycle against the real database: allocation, child exhaustion, governed
// extension, terminal settlement, expiry reclaim, and reuse by later siblings.
func TestPhase5ResourceLifecycleWorkflow(t *testing.T) {
	databaseURL := os.Getenv("THINKPIXELAG_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Fatal("THINKPIXELAG_TEST_DATABASE_URL is required for the Phase 5 end-to-end suite")
	}
	ctx := context.Background()
	connection, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	migrator, err := NewMigrator(ctx, connection, os.DirFS(projectMigrationsDir(t)))
	if err != nil {
		t.Fatal(err)
	}
	if err := migrator.Up(ctx); err != nil {
		t.Fatal(err)
	}
	if err := connection.Close(ctx); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	now := time.Now().UTC().Truncate(time.Microsecond)
	tenant, requester, governor, worker, system := newE2EID(t), newE2EID(t), newE2EID(t), newE2EID(t), newE2EID(t)
	agentID, versionID, approvalID, dimensionID := newE2EID(t), newE2EID(t), newE2EID(t), newE2EID(t)
	if _, err := pool.Exec(ctx, `INSERT INTO tenants(id,slug,display_name,created_at,updated_at) VALUES($1,$2,$2,$3,$3)`, tenant.String(), "res013-"+tenant.String(), now); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO resource_dimensions(id,tenant_id,name,class,unit,scale,minimum_value,maximum_value,aggregation,created_at) VALUES($1,$2,'llm_tokens','CONSUMABLE','llm_tokens',0,0,1000,'SUM',$3)`, dimensionID.String(), tenant.String(), now); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"active_children", "total_children", "delegation_depth"} {
		if _, err := pool.Exec(ctx, `INSERT INTO resource_dimensions(id,tenant_id,name,class,unit,scale,minimum_value,maximum_value,aggregation,created_at) VALUES($1,$2,$3,'STRUCTURAL','children',0,0,1000,'MAX',$4)`, newE2EID(t).String(), tenant.String(), name, now); err != nil {
			t.Fatal(err)
		}
	}
	for _, principal := range []struct {
		id   domain.ID
		kind string
	}{{requester, "HUMAN"}, {governor, "HUMAN"}, {worker, "WORKLOAD"}, {system, "SYSTEM"}} {
		if _, err := pool.Exec(ctx, `INSERT INTO principals(id,tenant_id,external_issuer,external_subject,principal_type,created_at) VALUES($1,$2,'https://res013.test',$3,$4,$5)`, principal.id.String(), tenant.String(), principal.id.String(), principal.kind, now); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO agents(id,tenant_id,name,owner_principal_id,sponsor_principal_id,risk_class,created_at,updated_at) VALUES($1,$2,$3,$4,$5,'MEDIUM',$6,$6)`, agentID.String(), tenant.String(), "res013-"+agentID.String(), requester.String(), governor.String(), now); err != nil {
		t.Fatal(err)
	}
	repositories, err := NewRepositories(pool)
	if err != nil {
		t.Fatal(err)
	}
	repository, err := repositories.ForTenant(tenant)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := domain.NewAgentManifest("registry.example/res013@sha256:"+strings.Repeat("a", 64), nil, nil, nil, nil, domain.AgentLimits{})
	if err != nil {
		t.Fatal(err)
	}
	digest, _ := manifest.ContentDigest()
	if err := repository.RegisterAgentVersion(ctx, domain.AgentVersion{ID: versionID, TenantID: tenant, AgentID: agentID, ContentDigest: digest, ImageDigest: "sha256:" + strings.Repeat("a", 64), Manifest: manifest, CreatedBy: requester, CreatedAt: now}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO agent_version_approvals(id,tenant_id,agent_id,agent_version_id,decision,actor_principal_id,policy_decision_id,reason_code,created_at) VALUES($1,$2,$3,$4,'APPROVED',$5,$6,'registry.version.approved',$7)`, approvalID.String(), tenant.String(), agentID.String(), versionID.String(), requester.String(), newE2EID(t).String(), now); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM outbox_messages WHERE tenant_id=$1`, tenant.String())
	})

	makeAdmission := func(constraints map[string]any, at time.Time) (domain.RunAdmission, domain.RunVersionResolution, ports.RunAdmissionEvidence) {
		runID, envelopeID, decisionID := newE2EID(t), newE2EID(t), newE2EID(t)
		admission := domain.RunAdmission{RunID: runID, EnvelopeID: envelopeID, TenantID: tenant, AgentID: agentID, AgentVersionID: versionID, AgentVersionDigest: digest, RequestedBy: requester, PolicyDecisionID: decisionID, State: domain.RunAdmitted, StateVersion: 1, Constraints: constraints, CreatedAt: at, UpdatedAt: at}
		resolution := domain.RunVersionResolution{RunID: runID, TenantID: tenant, AgentID: agentID, AgentVersionID: versionID, ApprovalID: approvalID, AgentContentDigest: digest, PolicyBundleDigest: "sha256:" + strings.Repeat("f", 64), PolicyActivationVersion: 1, Mode: domain.ResolutionAutomatic, InvocationDecisionID: decisionID, ResolvedConstraints: constraints, ResolvedAt: at}
		evidence := ports.RunAdmissionEvidence{EventID: newE2EID(t), AuditID: newE2EID(t), OutboxID: newE2EID(t), RequestID: newE2EID(t), ReasonCodes: []string{"agent.invoke.allowed"}}
		return admission, resolution, evidence
	}
	rootConstraints := map[string]any{"max_llm_tokens": float64(100), "max_active_children": float64(3), "max_total_children": float64(4), "max_delegation_depth": float64(1)}
	root, rootResolution, rootEvidence := makeAdmission(rootConstraints, now.Add(-time.Minute))
	if err := repository.AdmitRun(ctx, root, rootResolution, rootEvidence); err != nil {
		t.Fatal(err)
	}
	makeChild := func(amount int64, expiresAt *time.Time, offset time.Duration) (domain.RunAdmission, domain.ResourceReservation) {
		constraints := map[string]any{"max_active_children": float64(1), "max_total_children": float64(1), "max_delegation_depth": float64(1)}
		child, resolution, evidence := makeAdmission(constraints, now.Add(offset))
		reservation := domain.ResourceReservation{ID: newE2EID(t), TenantID: tenant, ParentEnvelopeID: root.EnvelopeID, ChildEnvelopeID: child.EnvelopeID, ChildRunID: child.RunID, Amounts: []domain.ResourceReservationAmount{{DimensionID: dimensionID, Coefficient: amount}}, ExpiresAt: expiresAt, CreatedAt: now.Add(offset)}
		if _, err := repository.AdmitChildRun(ctx, child, resolution, evidence, reservation); err != nil {
			t.Fatal(err)
		}
		return child, reservation
	}

	child, reservation := makeChild(60, nil, -50*time.Second)
	if _, err := pool.Exec(ctx, `UPDATE runs SET state='RUNNING',state_version=2,updated_at=$3,lease_id=$4,lease_expires_at=$5,fencing_token=1 WHERE tenant_id=$1 AND id=$2`, tenant.String(), child.RunID.String(), now.Add(-40*time.Second), newE2EID(t).String(), now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	clock := fixedE2EClock{now: now}
	evaluator := phase5PolicyEvaluator{}
	usageService, err := application.NewTrustedUsageService(repository, evaluator, clock)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := usageService.Record(ctx, application.RecordTrustedUsage{TenantID: tenant, ProducerID: worker, RequestID: newE2EID(t), RunID: child.RunID, Roles: []string{"trusted-workload"}, Issuer: "https://res013.test", SourceEventID: "child-exhaustion", ResourceName: "llm_tokens", Unit: "llm_tokens", Quantity: 60, ObservedAt: now.Add(-time.Second), SecurityState: policy.SecurityState{Authoritative: true}})
	if err != nil || receipt.Duplicate {
		t.Fatalf("exhaustion receipt=%+v error=%v", receipt, err)
	}
	assertPhase5State(t, pool, tenant, root.EnvelopeID, child.RunID, dimensionID, "PAUSED_FOR_BUDGET", 40, 0, 60)

	addition, _ := domain.NewDecimal(20, 0)
	quantity, _ := domain.NewQuantity(addition, "llm_tokens")
	extensionService, err := application.NewResourceExtensionService(repository, evaluator, clock)
	if err != nil {
		t.Fatal(err)
	}
	extended, err := extensionService.Extend(ctx, application.ExtendResources{TenantID: tenant, PrincipalID: governor, RequestID: newE2EID(t), RunID: child.RunID, Roles: []string{"resource-governor"}, Issuer: "https://res013.test", IdempotencyKey: "res013-extension", ReasonCode: "budget.increase", ApprovalReference: "CAB-RES013", Additions: []domain.ResourceExtensionAmount{{Name: "llm_tokens", Quantity: quantity}}, SecurityState: policy.SecurityState{Authoritative: true}})
	if err != nil || !extended.Resumed || extended.EnvelopeVersion != 2 {
		t.Fatalf("extension=%+v error=%v", extended, err)
	}
	if _, err := usageService.Record(ctx, application.RecordTrustedUsage{TenantID: tenant, ProducerID: worker, RequestID: newE2EID(t), RunID: child.RunID, Roles: []string{"trusted-workload"}, Issuer: "https://res013.test", SourceEventID: "child-extension-consumption", ResourceName: "llm_tokens", Unit: "llm_tokens", Quantity: 20, ObservedAt: now, SecurityState: policy.SecurityState{Authoritative: true}}); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE runs SET state='COMPLETED',state_version=state_version+1,updated_at=$3,terminal_at=$3 WHERE tenant_id=$1 AND id=$2 AND state='PAUSED_FOR_BUDGET'`, tenant.String(), child.RunID.String(), now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	settlementService, err := application.NewResourceSettlementService(repository, evaluator, fixedE2EClock{now: now.Add(time.Second)})
	if err != nil {
		t.Fatal(err)
	}
	settled, err := settlementService.Settle(ctx, application.SettleReservation{TenantID: tenant, PrincipalID: worker, RequestID: newE2EID(t), ReservationID: reservation.ID, Roles: []string{"trusted-workload"}, Issuer: "https://res013.test", IdempotencyKey: "res013-settlement", TerminalRunState: "COMPLETED", SecurityState: policy.SecurityState{Authoritative: true}})
	if err != nil || settled.Duplicate || len(settled.Consumed) != 1 || settled.Consumed[0].Value != 60 || settled.Returned[0].Value != 0 {
		t.Fatalf("settlement=%+v error=%v", settled, err)
	}
	assertPhase5ParentBalance(t, pool, tenant, root.EnvelopeID, dimensionID, 40, 60, 0)

	expiresAt := now.Add(2 * time.Second)
	expiredChild, expiredReservation := makeChild(40, &expiresAt, -10*time.Second)
	if _, err := pool.Exec(ctx, `UPDATE runs SET state='RUNNING',state_version=2,updated_at=$3,lease_id=$4,lease_expires_at=$5,fencing_token=1 WHERE tenant_id=$1 AND id=$2`, tenant.String(), expiredChild.RunID.String(), now, newE2EID(t).String(), now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	assertPhase5ParentBalance(t, pool, tenant, root.EnvelopeID, dimensionID, 0, 60, 40)
	reconciliation, err := application.NewResourceReconciliationService(repositories, fixedE2EClock{now: now.Add(3 * time.Second)})
	if err != nil {
		t.Fatal(err)
	}
	reclaimed, err := reconciliation.Reconcile(ctx, tenant, system, 1)
	if err != nil || len(reclaimed) != 1 || !reclaimed[0].Expired || reclaimed[0].Settlement.ReservationID != expiredReservation.ID {
		t.Fatalf("reconciliation=%+v error=%v", reclaimed, err)
	}
	assertPhase5ParentBalance(t, pool, tenant, root.EnvelopeID, dimensionID, 40, 60, 0)

	_, reusedReservation := makeChild(40, nil, 4*time.Second)
	assertPhase5ParentBalance(t, pool, tenant, root.EnvelopeID, dimensionID, 0, 60, 40)
	var state string
	if err := pool.QueryRow(ctx, `SELECT state FROM resource_reservations WHERE tenant_id=$1 AND id=$2`, tenant.String(), reusedReservation.ID.String()).Scan(&state); err != nil || state != "OPEN" {
		t.Fatalf("sibling reuse state=%s error=%v", state, err)
	}
}

type phase5PolicyEvaluator struct{}

func (phase5PolicyEvaluator) Decide(_ context.Context, input policy.Input) (policy.Result, error) {
	return policy.Result{Decision: policy.Decision{ContractVersion: policy.ContractVersion, DecisionID: input.DecisionID, Allow: true, ReasonCodes: []string{"resource.operation.allowed"}, Obligations: []policy.Obligation{{Type: "budget.pause_on_exhaustion"}}}, Metadata: policy.Metadata{PolicyDigest: "sha256:" + strings.Repeat("f", 64), PolicyVersion: 1}}, nil
}

func assertPhase5State(t *testing.T, pool *pgxpool.Pool, tenant, parentEnvelope, runID, dimension domain.ID, state string, available, consumed, allocated int64) {
	t.Helper()
	var gotState string
	if err := pool.QueryRow(context.Background(), `SELECT state FROM runs WHERE tenant_id=$1 AND id=$2`, tenant.String(), runID.String()).Scan(&gotState); err != nil || gotState != state {
		t.Fatalf("run state=%s want=%s error=%v", gotState, state, err)
	}
	assertPhase5ParentBalance(t, pool, tenant, parentEnvelope, dimension, available, consumed, allocated)
}

func assertPhase5ParentBalance(t *testing.T, pool *pgxpool.Pool, tenant, envelope, dimension domain.ID, available, consumed, allocated int64) {
	t.Helper()
	var gotAvailable, gotConsumed, gotAllocated int64
	if err := pool.QueryRow(context.Background(), `SELECT available_value,direct_consumed_value,allocated_open_value FROM resource_balances WHERE tenant_id=$1 AND envelope_id=$2 AND dimension_id=$3`, tenant.String(), envelope.String(), dimension.String()).Scan(&gotAvailable, &gotConsumed, &gotAllocated); err != nil || gotAvailable != available || gotConsumed != consumed || gotAllocated != allocated {
		t.Fatalf("parent balance=%d/%d/%d want=%d/%d/%d error=%v", gotAvailable, gotConsumed, gotAllocated, available, consumed, allocated, err)
	}
}

var _ policy.Evaluator = phase5PolicyEvaluator{}
