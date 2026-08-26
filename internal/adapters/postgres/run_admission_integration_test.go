//go:build integration

package postgres

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRunAdmissionCommitsCompleteAggregateAndRollsBackAtomically(t *testing.T) {
	databaseURL := os.Getenv("THINKPIXELAG_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("THINKPIXELAG_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	connection, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	migrator, _ := NewMigrator(ctx, connection, os.DirFS(projectMigrationsDir(t)))
	if err := migrator.Up(ctx); err != nil {
		t.Fatal(err)
	}
	_ = connection.Close(ctx)
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	tenant, principal, sponsor, systemPrincipal, agentID, versionID, approvalID := mustNewRepositoryID(t), mustNewRepositoryID(t), mustNewRepositoryID(t), mustNewRepositoryID(t), mustNewRepositoryID(t), mustNewRepositoryID(t), mustNewRepositoryID(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	_, err = pool.Exec(ctx, `INSERT INTO tenants(id,slug,display_name,created_at,updated_at)VALUES($1,$2,$2,$3,$3)`, tenant.String(), "run002-"+tenant.String(), now)
	if err != nil {
		t.Fatal(err)
	}
	llmDimension, toolDimension, toolRateDimension := mustNewRepositoryID(t), mustNewRepositoryID(t), mustNewRepositoryID(t)
	if _, err := pool.Exec(ctx, `INSERT INTO resource_dimensions(id,tenant_id,name,class,unit,scale,minimum_value,maximum_value,aggregation,created_at) VALUES($1,$2,'llm_tokens','CONSUMABLE','llm_tokens',0,0,1000,'SUM',$3)`, llmDimension.String(), tenant.String(), now); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO resource_dimensions(id,tenant_id,name,class,unit,scale,minimum_value,maximum_value,aggregation,created_at) VALUES($1,$2,'tool_calls','CONSUMABLE','calls',0,0,1000,'SUM',$3)`, toolDimension.String(), tenant.String(), now); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO resource_dimensions(id,tenant_id,name,class,unit,scale,minimum_value,maximum_value,aggregation,created_at) VALUES($1,$2,'tool_calls_per_minute','STRUCTURAL','calls_per_minute',0,0,1000,'MAX',$3)`, toolRateDimension.String(), tenant.String(), now); err != nil {
		t.Fatal(err)
	}
	for _, definition := range []struct {
		id   domain.ID
		name string
	}{{mustNewRepositoryID(t), "active_children"}, {mustNewRepositoryID(t), "total_children"}, {mustNewRepositoryID(t), "delegation_depth"}} {
		if _, err := pool.Exec(ctx, `INSERT INTO resource_dimensions(id,tenant_id,name,class,unit,scale,minimum_value,maximum_value,aggregation,created_at) VALUES($1,$2,$3,'STRUCTURAL','children',0,0,1000,'MAX',$4)`, definition.id.String(), tenant.String(), definition.name, now); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []domain.ID{principal, sponsor} {
		if _, err := pool.Exec(ctx, `INSERT INTO principals(id,tenant_id,external_issuer,external_subject,principal_type,created_at)VALUES($1,$2,'https://run002.test',$3,'HUMAN',$4)`, id.String(), tenant.String(), id.String(), now); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO principals(id,tenant_id,external_issuer,external_subject,principal_type,created_at)VALUES($1,$2,'https://run002.test',$3,'SYSTEM',$4)`, systemPrincipal.String(), tenant.String(), systemPrincipal.String(), now); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO agents(id,tenant_id,name,owner_principal_id,sponsor_principal_id,risk_class,created_at,updated_at)VALUES($1,$2,$3,$4,$5,'HIGH',$6,$6)`, agentID.String(), tenant.String(), "admission-"+agentID.String(), principal.String(), sponsor.String(), now); err != nil {
		t.Fatal(err)
	}
	repositories, _ := NewRepositories(pool)
	repository, _ := repositories.ForTenant(tenant)
	manifest, _ := domain.NewAgentManifest("registry.example/agent@sha256:"+strings.Repeat("a", 64), nil, nil, nil, nil, domain.AgentLimits{})
	digest, _ := manifest.ContentDigest()
	version := domain.AgentVersion{ID: versionID, TenantID: tenant, AgentID: agentID, ContentDigest: digest, ImageDigest: "sha256:" + strings.Repeat("a", 64), Manifest: manifest, CreatedBy: principal, CreatedAt: now}
	if err := repository.RegisterAgentVersion(ctx, version, nil); err != nil {
		t.Fatal(err)
	}
	approvalPolicyID := mustNewRepositoryID(t)
	if _, err := pool.Exec(ctx, `INSERT INTO agent_version_approvals(id,tenant_id,agent_id,agent_version_id,decision,actor_principal_id,policy_decision_id,reason_code,created_at)VALUES($1,$2,$3,$4,'APPROVED',$5,$6,'registry.version.approved',$7)`, approvalID.String(), tenant.String(), agentID.String(), versionID.String(), principal.String(), approvalPolicyID.String(), now); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM outbox_messages WHERE tenant_id=$1`, tenant.String())
	})
	makeAdmission := func() (domain.RunAdmission, domain.RunVersionResolution, ports.RunAdmissionEvidence) {
		runID, envelopeID, decisionID := mustNewRepositoryID(t), mustNewRepositoryID(t), mustNewRepositoryID(t)
		constraints := map[string]any{"max_execution_time_seconds": float64(60), "max_llm_tokens": float64(100), "max_tool_calls": float64(50), "max_tool_calls_per_minute": float64(5), "max_active_children": float64(10), "max_total_children": float64(20), "max_delegation_depth": float64(4)}
		deadline := now.Add(time.Minute)
		admission := domain.RunAdmission{RunID: runID, EnvelopeID: envelopeID, TenantID: tenant, AgentID: agentID, AgentVersionID: versionID, AgentVersionDigest: digest, RequestedBy: principal, PolicyDecisionID: decisionID, State: domain.RunAdmitted, StateVersion: 1, Constraints: constraints, DeadlineAt: &deadline, CreatedAt: now, UpdatedAt: now}
		resolution := domain.RunVersionResolution{RunID: runID, TenantID: tenant, AgentID: agentID, AgentVersionID: versionID, ApprovalID: approvalID, AgentContentDigest: digest, PolicyBundleDigest: "sha256:" + strings.Repeat("f", 64), PolicyActivationVersion: 3, Mode: domain.ResolutionAutomatic, InvocationDecisionID: decisionID, ResolvedConstraints: constraints, ResolvedAt: now}
		evidence := ports.RunAdmissionEvidence{EventID: mustNewRepositoryID(t), AuditID: mustNewRepositoryID(t), OutboxID: mustNewRepositoryID(t), RequestID: mustNewRepositoryID(t), ReasonCodes: []string{"agent.invoke.allowed"}}
		return admission, resolution, evidence
	}
	admission, resolution, evidence := makeAdmission()
	if err := repository.AdmitRun(ctx, admission, resolution, evidence); err != nil {
		t.Fatal(err)
	}
	var state string
	var stateVersion, events, envelopes, resolutions, audits, outbox int
	if err := pool.QueryRow(ctx, `SELECT state,state_version FROM runs WHERE tenant_id=$1 AND id=$2`, tenant.String(), admission.RunID.String()).Scan(&state, &stateVersion); err != nil {
		t.Fatal(err)
	}
	if state != "ADMITTED" || stateVersion != 1 {
		t.Fatalf("state=%s version=%d", state, stateVersion)
	}
	projection, err := repository.GetRun(ctx, admission.RunID)
	if err != nil || projection.Run.ID != admission.RunID || projection.Run.TenantID != tenant || projection.Run.VersionDigest != digest || projection.Run.EnvelopeVersion != 1 || projection.AgentRiskClass != domain.AgentRiskHigh || projection.AgentOwnerID != principal {
		t.Fatalf("run projection=%+v error=%v", projection, err)
	}
	otherTenant := mustNewRepositoryID(t)
	if _, err := pool.Exec(ctx, `INSERT INTO tenants(id,slug,display_name,created_at,updated_at)VALUES($1,$2,$2,$3,$3)`, otherTenant.String(), "run-query-"+otherTenant.String(), now); err != nil {
		t.Fatal(err)
	}
	otherRepository, _ := repositories.ForTenant(otherTenant)
	if _, err := otherRepository.GetRun(ctx, admission.RunID); domain.ErrorCodeOf(err) != domain.CodeNotFound {
		t.Fatalf("cross-tenant run query error=%v", err)
	}
	for query, target := range map[string]*int{
		`SELECT count(*) FROM run_events WHERE tenant_id=$1 AND run_id=$2`:                                     &events,
		`SELECT count(*) FROM resource_envelopes WHERE tenant_id=$1 AND run_id=$2`:                             &envelopes,
		`SELECT count(*) FROM run_version_resolutions WHERE tenant_id=$1 AND run_id=$2`:                        &resolutions,
		`SELECT count(*) FROM audit_events WHERE tenant_id=$1 AND resource_type='run' AND resource_id=$2`:      &audits,
		`SELECT count(*) FROM outbox_messages WHERE tenant_id=$1 AND aggregate_type='run' AND aggregate_id=$2`: &outbox,
	} {
		if err := pool.QueryRow(ctx, query, tenant.String(), admission.RunID.String()).Scan(target); err != nil {
			t.Fatal(err)
		}
	}
	if events != 1 || envelopes != 1 || resolutions != 1 || audits != 1 || outbox != 1 {
		t.Fatalf("events=%d envelopes=%d resolutions=%d audits=%d outbox=%d", events, envelopes, resolutions, audits, outbox)
	}
	var granted, available, consumed, allocated, balanceVersion int64
	var grantUnit string
	var grantScale int16
	if err := pool.QueryRow(ctx, `SELECT g.granted_value,g.unit,g.scale,b.available_value,b.direct_consumed_value,b.allocated_open_value,b.state_version FROM resource_envelope_grants g JOIN resource_balances b USING(tenant_id,envelope_id,dimension_id) WHERE g.tenant_id=$1 AND g.envelope_id=$2 AND g.dimension_id=$3`, tenant.String(), admission.EnvelopeID.String(), llmDimension.String()).Scan(&granted, &grantUnit, &grantScale, &available, &consumed, &allocated, &balanceVersion); err != nil {
		t.Fatal(err)
	}
	if granted != 100 || available != granted || consumed != 0 || allocated != 0 || balanceVersion != 1 || grantUnit != "llm_tokens" || grantScale != 0 {
		t.Fatalf("grant=%d %s/%d balance=%d/%d/%d v%d", granted, grantUnit, grantScale, available, consumed, allocated, balanceVersion)
	}
	if _, err := pool.Exec(ctx, `UPDATE resource_envelope_grants SET granted_value=101 WHERE tenant_id=$1 AND envelope_id=$2 AND dimension_id=$3`, tenant.String(), admission.EnvelopeID.String(), llmDimension.String()); err == nil {
		t.Fatal("immutable root grant accepted an update")
	}
	meterAdmission, meterResolution, meterEvidence := makeAdmission()
	if err := repository.AdmitRun(ctx, meterAdmission, meterResolution, meterEvidence); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE runs SET state='RUNNING',state_version=2,lease_id=$3,lease_expires_at=$4,fencing_token=1 WHERE tenant_id=$1 AND id=$2`, tenant.String(), meterAdmission.RunID.String(), mustNewRepositoryID(t).String(), now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	usageAmount, _ := domain.NewDecimal(7, 0)
	usageQuantity, _ := domain.NewQuantity(usageAmount, "llm_tokens")
	usage := domain.TrustedUsage{ID: mustNewRepositoryID(t), TenantID: tenant, RunID: meterAdmission.RunID, ProducerID: principal, SourceEventID: "meter-event-1", ResourceName: "llm_tokens", Quantity: usageQuantity, ObservedAt: now, RecordedAt: now.Add(time.Second)}
	receipt, err := repository.RecordTrustedUsage(ctx, usage, domain.ThroughputHint{}, domain.ExhaustionFail)
	if err != nil || receipt.Duplicate || receipt.UsageID != usage.ID {
		t.Fatalf("trusted usage receipt=%+v error=%v", receipt, err)
	}
	usageReplay, err := repository.RecordTrustedUsage(ctx, usage, domain.ThroughputHint{}, domain.ExhaustionFail)
	if err != nil || !usageReplay.Duplicate || usageReplay.UsageID != usage.ID {
		t.Fatalf("trusted usage replay=%+v error=%v", usageReplay, err)
	}
	changed := usage
	changed.ID = mustNewRepositoryID(t)
	changedAmount, _ := domain.NewDecimal(8, 0)
	changed.Quantity, _ = domain.NewQuantity(changedAmount, "llm_tokens")
	if _, err := repository.RecordTrustedUsage(ctx, changed, domain.ThroughputHint{}, domain.ExhaustionFail); domain.ErrorCodeOf(err) != domain.CodeConflict {
		t.Fatalf("mismatched trusted usage replay error=%v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT available_value,direct_consumed_value,state_version FROM resource_balances WHERE tenant_id=$1 AND envelope_id=$2 AND dimension_id=$3`, tenant.String(), meterAdmission.EnvelopeID.String(), llmDimension.String()).Scan(&available, &consumed, &balanceVersion); err != nil {
		t.Fatal(err)
	}
	if available != 93 || consumed != 7 || balanceVersion != 2 {
		t.Fatalf("metered balance available=%d consumed=%d version=%d", available, consumed, balanceVersion)
	}
	overAmount, _ := domain.NewDecimal(94, 0)
	overQuantity, _ := domain.NewQuantity(overAmount, "llm_tokens")
	over := domain.TrustedUsage{ID: mustNewRepositoryID(t), TenantID: tenant, RunID: meterAdmission.RunID, ProducerID: principal, SourceEventID: "meter-event-over", ResourceName: "llm_tokens", Quantity: overQuantity, ObservedAt: now, RecordedAt: now.Add(2 * time.Second)}
	if _, err := repository.RecordTrustedUsage(ctx, over, domain.ThroughputHint{}, domain.ExhaustionFail); domain.ErrorCodeOf(err) != domain.CodeConflict {
		t.Fatalf("usage beyond grant error=%v", err)
	}
	var usageRows int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM trusted_usage_entries WHERE tenant_id=$1 AND run_id=$2`, tenant.String(), meterAdmission.RunID.String()).Scan(&usageRows); err != nil || usageRows != 1 {
		t.Fatalf("trusted usage rows=%d error=%v", usageRows, err)
	}
	toolAmount, _ := domain.NewDecimal(3, 0)
	toolQuantity, _ := domain.NewQuantity(toolAmount, "calls")
	toolUsage := domain.TrustedUsage{ID: mustNewRepositoryID(t), TenantID: tenant, RunID: meterAdmission.RunID, ProducerID: principal, SourceEventID: "tool-rate-1", ResourceName: "tool_calls", Quantity: toolQuantity, ObservedAt: now, RecordedAt: now.Add(3 * time.Second)}
	if _, err := repository.RecordTrustedUsage(ctx, toolUsage, domain.ThroughputHint{}, domain.ExhaustionFail); err != nil {
		t.Fatalf("first rate-governed usage: %v", err)
	}
	secondToolUsage := toolUsage
	secondToolUsage.ID, secondToolUsage.SourceEventID = mustNewRepositoryID(t), "tool-rate-2"
	if _, err := repository.RecordTrustedUsage(ctx, secondToolUsage, domain.ThroughputHint{}, domain.ExhaustionFail); !errors.Is(err, domain.ErrStructuralThroughputExceeded) {
		t.Fatalf("throughput excess error=%v", err)
	}
	if replay, err := repository.RecordTrustedUsage(ctx, toolUsage, domain.ThroughputHint{DimensionName: "tool_calls_per_minute", Blocked: true}, domain.ExhaustionFail); err != nil || !replay.Duplicate {
		t.Fatalf("blocked-hint replay=%+v error=%v", replay, err)
	}
	nextWindow := toolUsage
	nextWindow.ID, nextWindow.SourceEventID = mustNewRepositoryID(t), "tool-rate-next-window"
	nextWindow.RecordedAt = toolUsage.RecordedAt.Truncate(time.Minute).Add(time.Minute)
	if _, err := repository.RecordTrustedUsage(ctx, nextWindow, domain.ThroughputHint{}, domain.ExhaustionFail); err != nil {
		t.Fatalf("next throughput window: %v", err)
	}
	var rateUsed int64
	if err := pool.QueryRow(ctx, `SELECT sum(used_value) FROM resource_rate_windows WHERE tenant_id=$1 AND envelope_id=$2 AND dimension_id=$3`, tenant.String(), meterAdmission.EnvelopeID.String(), toolRateDimension.String()).Scan(&rateUsed); err != nil || rateUsed != 6 {
		t.Fatalf("rate usage=%d error=%v", rateUsed, err)
	}
	concurrentRateAdmission, concurrentRateResolution, concurrentRateEvidence := makeAdmission()
	if err := repository.AdmitRun(ctx, concurrentRateAdmission, concurrentRateResolution, concurrentRateEvidence); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE runs SET state='RUNNING',state_version=2,lease_id=$3,lease_expires_at=$4,fencing_token=1 WHERE tenant_id=$1 AND id=$2`, tenant.String(), concurrentRateAdmission.RunID.String(), mustNewRepositoryID(t).String(), now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	oneAmount, _ := domain.NewDecimal(1, 0)
	oneToolCall, _ := domain.NewQuantity(oneAmount, "calls")
	var rateGroup sync.WaitGroup
	rateResults := make(chan error, 10)
	rateUsageIDs := make([]domain.ID, 10)
	for i := range rateUsageIDs {
		rateUsageIDs[i] = mustNewRepositoryID(t)
	}
	for i := range 10 {
		rateGroup.Add(1)
		go func(index int) {
			defer rateGroup.Done()
			candidate := domain.TrustedUsage{ID: rateUsageIDs[index], TenantID: tenant, RunID: concurrentRateAdmission.RunID, ProducerID: principal, SourceEventID: fmt.Sprintf("tool-rate-concurrent-%02d", index), ResourceName: "tool_calls", Quantity: oneToolCall, ObservedAt: now, RecordedAt: now.Add(10 * time.Second)}
			_, recordErr := repository.RecordTrustedUsage(ctx, candidate, domain.ThroughputHint{}, domain.ExhaustionFail)
			rateResults <- recordErr
		}(i)
	}
	rateGroup.Wait()
	close(rateResults)
	acceptedRate, deniedRate := 0, 0
	for rateErr := range rateResults {
		switch {
		case rateErr == nil:
			acceptedRate++
		case errors.Is(rateErr, domain.ErrStructuralThroughputExceeded):
			deniedRate++
		default:
			t.Fatalf("concurrent throughput error=%v", rateErr)
		}
	}
	if acceptedRate != 5 || deniedRate != 5 {
		t.Fatalf("concurrent throughput accepted=%d denied=%d", acceptedRate, deniedRate)
	}
	if err := pool.QueryRow(ctx, `SELECT used_value FROM resource_rate_windows WHERE tenant_id=$1 AND envelope_id=$2 AND dimension_id=$3`, tenant.String(), concurrentRateAdmission.EnvelopeID.String(), toolRateDimension.String()).Scan(&rateUsed); err != nil || rateUsed != 5 {
		t.Fatalf("concurrent rate usage=%d error=%v", rateUsed, err)
	}
	for _, exhaustion := range []struct {
		name        string
		disposition domain.ExhaustionDisposition
		wantState   string
		wantEvent   string
	}{{"pause", domain.ExhaustionPause, "PAUSED_FOR_BUDGET", "run.paused_for_budget"}, {"fail", domain.ExhaustionFail, "FAILED_BUDGET", "run.failed_budget"}} {
		t.Run("budget exhaustion "+exhaustion.name, func(t *testing.T) {
			exhaustionAdmission, exhaustionResolution, exhaustionEvidence := makeAdmission()
			if err := repository.AdmitRun(ctx, exhaustionAdmission, exhaustionResolution, exhaustionEvidence); err != nil {
				t.Fatal(err)
			}
			leaseID := mustNewRepositoryID(t)
			if _, err := pool.Exec(ctx, `UPDATE runs SET state='RUNNING',state_version=2,lease_id=$3,lease_expires_at=$4,fencing_token=7 WHERE tenant_id=$1 AND id=$2`, tenant.String(), exhaustionAdmission.RunID.String(), leaseID.String(), now.Add(time.Minute)); err != nil {
				t.Fatal(err)
			}
			fullAmount, _ := domain.NewDecimal(100, 0)
			fullQuantity, _ := domain.NewQuantity(fullAmount, "llm_tokens")
			fullUsage := domain.TrustedUsage{ID: mustNewRepositoryID(t), TenantID: tenant, RunID: exhaustionAdmission.RunID, ProducerID: principal, SourceEventID: "exhaust-" + exhaustion.name, ResourceName: "llm_tokens", Quantity: fullQuantity, ObservedAt: now, RecordedAt: now.Add(20 * time.Second)}
			if _, err := repository.RecordTrustedUsage(ctx, fullUsage, domain.ThroughputHint{}, exhaustion.disposition); err != nil {
				t.Fatal(err)
			}
			var gotState string
			var gotVersion, gotFence, exhaustedEvents, dispositionEvents, exhaustionOutbox, exhaustionAudits int64
			var gotLease *string
			if err := pool.QueryRow(ctx, `SELECT state,state_version,fencing_token,lease_id::text FROM runs WHERE tenant_id=$1 AND id=$2`, tenant.String(), exhaustionAdmission.RunID.String()).Scan(&gotState, &gotVersion, &gotFence, &gotLease); err != nil {
				t.Fatal(err)
			}
			if err := pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE event_type='run.budget_exhausted'),count(*) FILTER (WHERE event_type=$3) FROM run_events WHERE tenant_id=$1 AND run_id=$2`, tenant.String(), exhaustionAdmission.RunID.String(), exhaustion.wantEvent).Scan(&exhaustedEvents, &dispositionEvents); err != nil {
				t.Fatal(err)
			}
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM outbox_messages WHERE tenant_id=$1 AND aggregate_id=$2 AND event_type IN ('run.budget_exhausted',$3)`, tenant.String(), exhaustionAdmission.RunID.String(), exhaustion.wantEvent).Scan(&exhaustionOutbox); err != nil {
				t.Fatal(err)
			}
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE tenant_id=$1 AND resource_id=$2 AND action='resources.exhaust'`, tenant.String(), exhaustionAdmission.RunID.String()).Scan(&exhaustionAudits); err != nil {
				t.Fatal(err)
			}
			if gotState != exhaustion.wantState || gotVersion != 4 || gotFence != 8 || gotLease != nil || exhaustedEvents != 1 || dispositionEvents != 1 || exhaustionOutbox != 2 || exhaustionAudits != 1 {
				t.Fatalf("state=%s version=%d fence=%d lease=%v exhausted=%d disposition=%d outbox=%d audits=%d", gotState, gotVersion, gotFence, gotLease, exhaustedEvents, dispositionEvents, exhaustionOutbox, exhaustionAudits)
			}
			if replay, err := repository.RecordTrustedUsage(ctx, fullUsage, domain.ThroughputHint{}, exhaustion.disposition); err != nil || !replay.Duplicate {
				t.Fatalf("exhaustion replay=%+v error=%v", replay, err)
			}
			late := fullUsage
			late.ID, late.SourceEventID = mustNewRepositoryID(t), "late-"+exhaustion.name
			if _, err := repository.RecordTrustedUsage(ctx, late, domain.ThroughputHint{}, exhaustion.disposition); domain.ErrorCodeOf(err) != domain.CodeConflict {
				t.Fatalf("post-exhaustion usage error=%v", err)
			}
			if exhaustion.disposition == domain.ExhaustionPause {
				addedAmount, _ := domain.NewDecimal(25, 0)
				addedQuantity, _ := domain.NewQuantity(addedAmount, "llm_tokens")
				extension := domain.ResourceExtension{ID: mustNewRepositoryID(t), TenantID: tenant, RunID: exhaustionAdmission.RunID, ActorPrincipalID: sponsor, PolicyDecisionID: mustNewRepositoryID(t), IdempotencyKey: "extend-paused-budget", ReasonCode: "budget.increase", ApprovalReference: "CAB-42", Additions: []domain.ResourceExtensionAmount{{Name: "llm_tokens", Quantity: addedQuantity}}, DeadlineExtensionSeconds: 30, CreatedAt: now.Add(40 * time.Second)}
				extensionEvidence := ports.ResourceExtensionEvidence{AuditID: mustNewRepositoryID(t), OutboxID: mustNewRepositoryID(t), EventID: mustNewRepositoryID(t), RequestID: mustNewRepositoryID(t), ReasonCodes: []string{"resource.extension.approved"}}
				extended, err := repository.ExtendResources(ctx, extension, extensionEvidence)
				if err != nil || !extended.Resumed || extended.EnvelopeVersion != 2 || extended.DeadlineAt == nil || !extended.DeadlineAt.Equal(now.Add(90*time.Second)) {
					t.Fatalf("extension=%+v error=%v", extended, err)
				}
				var extensionState string
				var extensionStateVersion, extensionEnvelopeVersion, immutableGrant, extensionAvailable, extensionConsumed, priorGrant, newGrant, extensionEvents, extensionAudits, extensionOutbox int64
				if err := pool.QueryRow(ctx, `SELECT r.state,r.state_version,e.version,g.granted_value,b.available_value,b.direct_consumed_value,i.prior_granted_value,i.new_granted_value,(SELECT count(*) FROM run_events WHERE tenant_id=r.tenant_id AND run_id=r.id AND event_type='run.resources_extended'),(SELECT count(*) FROM audit_events WHERE tenant_id=r.tenant_id AND resource_id=e.id::text AND action='resources.extend'),(SELECT count(*) FROM outbox_messages WHERE tenant_id=r.tenant_id AND aggregate_id=r.id::text AND event_type='run.resources_extended') FROM runs r JOIN resource_envelopes e ON e.tenant_id=r.tenant_id AND e.run_id=r.id JOIN resource_envelope_grants g ON g.tenant_id=e.tenant_id AND g.envelope_id=e.id JOIN resource_balances b ON b.tenant_id=g.tenant_id AND b.envelope_id=g.envelope_id AND b.dimension_id=g.dimension_id JOIN resource_extension_items i ON i.tenant_id=g.tenant_id AND i.dimension_id=g.dimension_id JOIN resource_extensions x ON x.tenant_id=i.tenant_id AND x.id=i.extension_id AND x.envelope_id=e.id WHERE r.tenant_id=$1 AND r.id=$2 AND g.dimension_id=$3`, tenant.String(), exhaustionAdmission.RunID.String(), llmDimension.String()).Scan(&extensionState, &extensionStateVersion, &extensionEnvelopeVersion, &immutableGrant, &extensionAvailable, &extensionConsumed, &priorGrant, &newGrant, &extensionEvents, &extensionAudits, &extensionOutbox); err != nil {
					t.Fatal(err)
				}
				if extensionState != "RUNNING" || extensionStateVersion != 5 || extensionEnvelopeVersion != 2 || immutableGrant != 100 || extensionAvailable != 25 || extensionConsumed != 100 || priorGrant != 100 || newGrant != 125 || extensionEvents != 1 || extensionAudits != 1 || extensionOutbox != 1 {
					t.Fatalf("extension state=%s/%d envelope=%d grant=%d balance=%d/%d ledger=%d/%d evidence=%d/%d/%d", extensionState, extensionStateVersion, extensionEnvelopeVersion, immutableGrant, extensionAvailable, extensionConsumed, priorGrant, newGrant, extensionEvents, extensionAudits, extensionOutbox)
				}
				replay, err := repository.ExtendResources(ctx, extension, extensionEvidence)
				if err != nil || replay.ID != extension.ID || replay.EnvelopeVersion != 2 || !replay.Resumed {
					t.Fatalf("extension replay=%+v error=%v", replay, err)
				}
				extension.DeadlineExtensionSeconds = 31
				if _, err := repository.ExtendResources(ctx, extension, extensionEvidence); domain.ErrorCodeOf(err) != domain.CodeConflict {
					t.Fatalf("mismatched extension replay error=%v", err)
				}
				// Keep this focused fixture out of the later generic claim-drain loop;
				// worker lease/recovery behavior is qualified independently below.
				if _, err := pool.Exec(ctx, `UPDATE runs SET state='COMPLETED',state_version=state_version+1,terminal_at=$3,updated_at=$3 WHERE tenant_id=$1 AND id=$2`, tenant.String(), exhaustionAdmission.RunID.String(), now.Add(50*time.Second)); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
	t.Run("concurrent usage exhausts exactly once", func(t *testing.T) {
		exhaustionAdmission, exhaustionResolution, exhaustionEvidence := makeAdmission()
		if err := repository.AdmitRun(ctx, exhaustionAdmission, exhaustionResolution, exhaustionEvidence); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `UPDATE runs SET state='RUNNING',state_version=2,lease_id=$3,lease_expires_at=$4,fencing_token=1 WHERE tenant_id=$1 AND id=$2`, tenant.String(), exhaustionAdmission.RunID.String(), mustNewRepositoryID(t).String(), now.Add(time.Minute)); err != nil {
			t.Fatal(err)
		}
		results := make(chan error, 2)
		var group sync.WaitGroup
		for index, coefficient := range []int64{40, 60} {
			group.Add(1)
			go func(index int, coefficient int64) {
				defer group.Done()
				amount, _ := domain.NewDecimal(coefficient, 0)
				quantity, _ := domain.NewQuantity(amount, "llm_tokens")
				candidate := domain.TrustedUsage{ID: mustNewRepositoryID(t), TenantID: tenant, RunID: exhaustionAdmission.RunID, ProducerID: principal, SourceEventID: fmt.Sprintf("concurrent-exhaust-%d", index), ResourceName: "llm_tokens", Quantity: quantity, ObservedAt: now, RecordedAt: now.Add(30 * time.Second)}
				_, err := repository.RecordTrustedUsage(ctx, candidate, domain.ThroughputHint{}, domain.ExhaustionPause)
				results <- err
			}(index, coefficient)
		}
		group.Wait()
		close(results)
		for err := range results {
			if err != nil {
				t.Fatalf("concurrent exhaustion error=%v", err)
			}
		}
		var gotState string
		var available, consumed, exhaustionEvents int64
		if err := pool.QueryRow(ctx, `SELECT r.state,b.available_value,b.direct_consumed_value,(SELECT count(*) FROM run_events WHERE tenant_id=$1 AND run_id=$2 AND event_type='run.budget_exhausted') FROM runs r JOIN resource_envelopes e ON e.tenant_id=r.tenant_id AND e.run_id=r.id JOIN resource_balances b ON b.tenant_id=e.tenant_id AND b.envelope_id=e.id WHERE r.tenant_id=$1 AND r.id=$2 AND b.dimension_id=$3`, tenant.String(), exhaustionAdmission.RunID.String(), llmDimension.String()).Scan(&gotState, &available, &consumed, &exhaustionEvents); err != nil {
			t.Fatal(err)
		}
		if gotState != "PAUSED_FOR_BUDGET" || available != 0 || consumed != 100 || exhaustionEvents != 1 {
			t.Fatalf("state=%s available=%d consumed=%d exhaustion_events=%d", gotState, available, consumed, exhaustionEvents)
		}
	})
	makeChild := func(parent domain.ID) (domain.RunAdmission, domain.RunVersionResolution, ports.RunAdmissionEvidence) {
		child, childResolution, childEvidence := makeAdmission()
		child.Constraints = map[string]any{"max_active_children": float64(10), "max_total_children": float64(20), "max_delegation_depth": float64(4)}
		childResolution.ResolvedConstraints = child.Constraints
		if err := repository.AdmitRun(ctx, child, childResolution, childEvidence); err != nil {
			t.Fatal(err)
		}
		if tag, err := pool.Exec(ctx, `UPDATE resource_envelopes SET parent_envelope_id=$1 WHERE tenant_id=$2 AND id=$3`, parent.String(), tenant.String(), child.EnvelopeID.String()); err != nil || tag.RowsAffected() != 1 {
			t.Fatalf("link child envelope: tag=%v err=%v", tag, err)
		}
		return child, childResolution, childEvidence
	}
	child, _, _ := makeChild(admission.EnvelopeID)
	reservation := domain.ResourceReservation{
		ID: mustNewRepositoryID(t), TenantID: tenant, ParentEnvelopeID: admission.EnvelopeID, ChildEnvelopeID: child.EnvelopeID, ChildRunID: child.RunID, CreatedAt: now.Add(time.Second),
		Amounts: []domain.ResourceReservationAmount{{DimensionID: toolDimension, Coefficient: 20}, {DimensionID: llmDimension, Coefficient: 40}},
	}
	reserved, err := repository.ReserveChildResources(ctx, reservation)
	if err != nil || reserved.Amounts[0].DimensionID.String() > reserved.Amounts[1].DimensionID.String() {
		t.Fatalf("reserve child resources=%+v error=%v", reserved, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE resource_reservation_items SET reserved_value=reserved_value+1 WHERE tenant_id=$1 AND reservation_id=$2 AND dimension_id=$3`, tenant.String(), reservation.ID.String(), llmDimension.String()); err == nil {
		t.Fatal("immutable reservation item accepted an update")
	}
	for _, expected := range []struct {
		dimension                          domain.ID
		amount, parentAvailable, allocated int64
	}{{llmDimension, 40, 60, 40}, {toolDimension, 20, 30, 20}} {
		var reservedValue, childGranted, childAvailable, parentAvailable, allocated int64
		err := pool.QueryRow(ctx, `SELECT i.reserved_value,g.granted_value,child.available_value,parent.available_value,parent.allocated_open_value FROM resource_reservation_items i JOIN resource_envelope_grants g ON g.tenant_id=i.tenant_id AND g.envelope_id=$3 AND g.dimension_id=i.dimension_id JOIN resource_balances child ON child.tenant_id=g.tenant_id AND child.envelope_id=g.envelope_id AND child.dimension_id=g.dimension_id JOIN resource_balances parent ON parent.tenant_id=i.tenant_id AND parent.envelope_id=$4 AND parent.dimension_id=i.dimension_id WHERE i.tenant_id=$1 AND i.reservation_id=$2 AND i.dimension_id=$5`, tenant.String(), reservation.ID.String(), child.EnvelopeID.String(), admission.EnvelopeID.String(), expected.dimension.String()).Scan(&reservedValue, &childGranted, &childAvailable, &parentAvailable, &allocated)
		if err != nil || reservedValue != expected.amount || childGranted != expected.amount || childAvailable != expected.amount || parentAvailable != expected.parentAvailable || allocated != expected.allocated {
			t.Fatalf("dimension %s reservation=%d child=%d/%d parent=%d/%d err=%v", expected.dimension, reservedValue, childGranted, childAvailable, parentAvailable, allocated, err)
		}
	}

	insufficientChild, _, _ := makeChild(admission.EnvelopeID)
	insufficient := domain.ResourceReservation{ID: mustNewRepositoryID(t), TenantID: tenant, ParentEnvelopeID: admission.EnvelopeID, ChildEnvelopeID: insufficientChild.EnvelopeID, ChildRunID: insufficientChild.RunID, CreatedAt: now.Add(2 * time.Second), Amounts: []domain.ResourceReservationAmount{{DimensionID: toolDimension, Coefficient: 10}, {DimensionID: llmDimension, Coefficient: 61}}}
	if _, err := repository.ReserveChildResources(ctx, insufficient); domain.ErrorCodeOf(err) != domain.CodeConflict {
		t.Fatalf("insufficient multi-resource reservation error=%v", err)
	}
	var failedReservations, failedItems, failedGrants int
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM resource_reservations WHERE tenant_id=$1 AND id=$2),(SELECT count(*) FROM resource_reservation_items WHERE tenant_id=$1 AND reservation_id=$2),(SELECT count(*) FROM resource_envelope_grants g JOIN resource_dimensions d ON d.tenant_id=g.tenant_id AND d.id=g.dimension_id WHERE g.tenant_id=$1 AND g.envelope_id=$3 AND d.class='CONSUMABLE')`, tenant.String(), insufficient.ID.String(), insufficientChild.EnvelopeID.String()).Scan(&failedReservations, &failedItems, &failedGrants); err != nil {
		t.Fatal(err)
	}
	var toolAvailable int64
	if err := pool.QueryRow(ctx, `SELECT available_value FROM resource_balances WHERE tenant_id=$1 AND envelope_id=$2 AND dimension_id=$3`, tenant.String(), admission.EnvelopeID.String(), toolDimension.String()).Scan(&toolAvailable); err != nil {
		t.Fatal(err)
	}
	if failedReservations != 0 || failedItems != 0 || failedGrants != 0 || toolAvailable != 30 {
		t.Fatalf("failed vector left reservation=%d items=%d grants=%d tool_available=%d", failedReservations, failedItems, failedGrants, toolAvailable)
	}

	parallelChildren := make([]domain.RunAdmission, 2)
	parallelReservationIDs := make([]domain.ID, 2)
	for index := range parallelChildren {
		parallelChildren[index], _, _ = makeChild(admission.EnvelopeID)
		parallelReservationIDs[index] = mustNewRepositoryID(t)
	}
	parallelErrors := make(chan error, len(parallelChildren))
	var reservationWait sync.WaitGroup
	for index, parallelChild := range parallelChildren {
		reservationWait.Add(1)
		go func(index int, child domain.RunAdmission) {
			defer reservationWait.Done()
			amounts := []domain.ResourceReservationAmount{{DimensionID: llmDimension, Coefficient: 40}, {DimensionID: toolDimension, Coefficient: 20}}
			if index == 1 {
				amounts[0], amounts[1] = amounts[1], amounts[0]
			}
			_, err := repository.ReserveChildResources(ctx, domain.ResourceReservation{ID: parallelReservationIDs[index], TenantID: tenant, ParentEnvelopeID: admission.EnvelopeID, ChildEnvelopeID: child.EnvelopeID, ChildRunID: child.RunID, CreatedAt: now.Add(time.Duration(3+index) * time.Second), Amounts: amounts})
			parallelErrors <- err
		}(index, parallelChild)
	}
	reservationWait.Wait()
	close(parallelErrors)
	var reservationSuccesses, reservationConflicts int
	for err := range parallelErrors {
		if err == nil {
			reservationSuccesses++
			continue
		}
		switch domain.ErrorCodeOf(err) {
		case domain.CodeConflict:
			reservationConflicts++
		default:
			t.Fatalf("parallel reservation error=%v", err)
		}
	}
	var llmAvailable, llmAllocated, parallelToolAvailable, parallelToolAllocated int64
	if err := pool.QueryRow(ctx, `SELECT llm.available_value,llm.allocated_open_value,tool.available_value,tool.allocated_open_value FROM resource_balances llm JOIN resource_balances tool ON tool.tenant_id=llm.tenant_id AND tool.envelope_id=llm.envelope_id WHERE llm.tenant_id=$1 AND llm.envelope_id=$2 AND llm.dimension_id=$3 AND tool.dimension_id=$4`, tenant.String(), admission.EnvelopeID.String(), llmDimension.String(), toolDimension.String()).Scan(&llmAvailable, &llmAllocated, &parallelToolAvailable, &parallelToolAllocated); err != nil {
		t.Fatal(err)
	}
	if reservationSuccesses != 1 || reservationConflicts != 1 || llmAvailable != 20 || llmAllocated != 80 || parallelToolAvailable != 10 || parallelToolAllocated != 40 {
		t.Fatalf("parallel successes=%d conflicts=%d llm=%d/%d tool=%d/%d", reservationSuccesses, reservationConflicts, llmAvailable, llmAllocated, parallelToolAvailable, parallelToolAllocated)
	}
	if _, err := pool.Exec(ctx, `UPDATE runs SET state='RUNNING',state_version=2,updated_at=$3 WHERE tenant_id=$1 AND id=$2`, tenant.String(), child.RunID.String(), now.Add(10*time.Second)); err != nil {
		t.Fatal(err)
	}
	settlementUsageAmount, _ := domain.NewDecimal(10, 0)
	settlementUsageQuantity, _ := domain.NewQuantity(settlementUsageAmount, "llm_tokens")
	settlementUsage := domain.TrustedUsage{ID: mustNewRepositoryID(t), TenantID: tenant, RunID: child.RunID, ProducerID: principal, SourceEventID: "settlement-usage", ResourceName: "llm_tokens", Quantity: settlementUsageQuantity, ObservedAt: now.Add(10 * time.Second), RecordedAt: now.Add(10 * time.Second)}
	if _, err := repository.RecordTrustedUsage(ctx, settlementUsage, domain.ThroughputHint{}, domain.ExhaustionFail); err != nil {
		t.Fatal(err)
	}
	settlementAt := now.Add(11 * time.Second)
	if _, err := pool.Exec(ctx, `UPDATE runs SET state='COMPLETED',state_version=3,updated_at=$3,terminal_at=$3 WHERE tenant_id=$1 AND id=$2`, tenant.String(), child.RunID.String(), settlementAt); err != nil {
		t.Fatal(err)
	}
	concurrentSettlement := domain.ResourceSettlement{ID: mustNewRepositoryID(t), TenantID: tenant, ReservationID: reservation.ID, ActorPrincipalID: principal, PolicyDecisionID: mustNewRepositoryID(t), IdempotencyKey: "settle-concurrent", TerminalRunState: "COMPLETED", FinalUsageEventIDs: []domain.ID{settlementUsage.ID}, SettledAt: settlementAt}
	concurrentEvidence := ports.ResourceSettlementEvidence{AuditID: mustNewRepositoryID(t), OutboxID: mustNewRepositoryID(t), RequestID: mustNewRepositoryID(t), ReasonCodes: []string{"workload.operation.allowed"}}
	settlementResults := make(chan domain.ResourceSettlementResult, 2)
	settlementErrors := make(chan error, 2)
	for range 2 {
		go func() {
			got, settleErr := repository.SettleReservation(ctx, concurrentSettlement, concurrentEvidence)
			settlementResults <- got
			settlementErrors <- settleErr
		}()
	}
	var duplicateSettlements int
	for range 2 {
		got, settleErr := <-settlementResults, <-settlementErrors
		if settleErr != nil {
			t.Fatalf("concurrent settlement: %v", settleErr)
		}
		if got.Duplicate {
			duplicateSettlements++
		}
		if len(got.Consumed) != 2 || len(got.Returned) != 2 {
			t.Fatalf("settlement vector=%+v", got)
		}
	}
	if duplicateSettlements != 1 {
		t.Fatalf("duplicate settlements=%d", duplicateSettlements)
	}
	var llmConsumed, parallelToolConsumed int64
	if err := pool.QueryRow(ctx, `SELECT llm.available_value,llm.direct_consumed_value,llm.allocated_open_value,tool.available_value,tool.direct_consumed_value,tool.allocated_open_value FROM resource_balances llm JOIN resource_balances tool ON tool.tenant_id=llm.tenant_id AND tool.envelope_id=llm.envelope_id WHERE llm.tenant_id=$1 AND llm.envelope_id=$2 AND llm.dimension_id=$3 AND tool.dimension_id=$4`, tenant.String(), admission.EnvelopeID.String(), llmDimension.String(), toolDimension.String()).Scan(&llmAvailable, &llmConsumed, &llmAllocated, &parallelToolAvailable, &parallelToolConsumed, &parallelToolAllocated); err != nil {
		t.Fatal(err)
	}
	if llmAvailable != 50 || llmConsumed != 10 || llmAllocated != 40 || parallelToolAvailable != 30 || parallelToolConsumed != 0 || parallelToolAllocated != 20 {
		t.Fatalf("settled parent llm=%d/%d/%d tool=%d/%d/%d", llmAvailable, llmConsumed, llmAllocated, parallelToolAvailable, parallelToolConsumed, parallelToolAllocated)
	}
	if llmAvailable+llmConsumed+llmAllocated != 100 || parallelToolAvailable+parallelToolConsumed+parallelToolAllocated != 50 {
		t.Fatalf("settlement violated conservation: llm=%d tool=%d", llmAvailable+llmConsumed+llmAllocated, parallelToolAvailable+parallelToolConsumed+parallelToolAllocated)
	}

	// Reconciliation uses the reservation as its exactly-once boundary. Two
	// replicas racing one terminal orphan produce one settlement and one no-work
	// result; replay observes no candidate and cannot credit the parent again.
	orphan, _, _ := makeChild(admission.EnvelopeID)
	orphanReservation := domain.ResourceReservation{ID: mustNewRepositoryID(t), TenantID: tenant, ParentEnvelopeID: admission.EnvelopeID, ChildEnvelopeID: orphan.EnvelopeID, ChildRunID: orphan.RunID, CreatedAt: now.Add(12 * time.Second), Amounts: []domain.ResourceReservationAmount{{DimensionID: llmDimension, Coefficient: 5}, {DimensionID: toolDimension, Coefficient: 2}}}
	if _, err := repository.ReserveChildResources(ctx, orphanReservation); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE runs SET state='FAILED',state_version=2,updated_at=$3,terminal_at=$3 WHERE tenant_id=$1 AND id=$2`, tenant.String(), orphan.RunID.String(), now.Add(13*time.Second)); err != nil {
		t.Fatal(err)
	}
	reconcileEvidence := func() ports.ResourceReconciliationEvidence {
		return ports.ResourceReconciliationEvidence{SettlementID: mustNewRepositoryID(t), PolicyDecisionID: mustNewRepositoryID(t), AuditID: mustNewRepositoryID(t), OutboxID: mustNewRepositoryID(t), RunEventID: mustNewRepositoryID(t)}
	}
	failedReconciliationEvidence := reconcileEvidence()
	failedReconciliationEvidence.OutboxID = evidence.OutboxID // force a late unique failure, like a worker crash before commit
	if _, err := repositories.ReconcileNextReservation(ctx, tenant, systemPrincipal, now.Add(40*time.Second), failedReconciliationEvidence); err == nil {
		t.Fatal("reconciliation with conflicting evidence unexpectedly committed")
	}
	var afterFailureState string
	var afterFailureSettlements int
	if err := pool.QueryRow(ctx, `SELECT state,(SELECT count(*) FROM resource_settlements WHERE tenant_id=$1 AND reservation_id=$2) FROM resource_reservations WHERE tenant_id=$1 AND id=$2`, tenant.String(), orphanReservation.ID.String()).Scan(&afterFailureState, &afterFailureSettlements); err != nil {
		t.Fatal(err)
	}
	if afterFailureState != "OPEN" || afterFailureSettlements != 0 {
		t.Fatalf("failed reconciliation left state=%s settlements=%d", afterFailureState, afterFailureSettlements)
	}
	type reconciliationAttempt struct {
		result domain.ResourceReconciliationResult
		err    error
	}
	reconcileAttempts := make(chan reconciliationAttempt, 2)
	for range 2 {
		go func() {
			got, reconcileErr := repositories.ReconcileNextReservation(ctx, tenant, systemPrincipal, now.Add(40*time.Second), reconcileEvidence())
			reconcileAttempts <- reconciliationAttempt{result: got, err: reconcileErr}
		}()
	}
	var reconciled, unavailable int
	for range 2 {
		attempt := <-reconcileAttempts
		got, reconcileErr := attempt.result, attempt.err
		if reconcileErr == nil {
			reconciled++
			if got.Expired || len(got.Settlement.Returned) != 2 {
				t.Fatalf("terminal reconciliation=%+v", got)
			}
		} else if errors.Is(reconcileErr, domain.ErrResourceReconciliationUnavailable) {
			unavailable++
		} else {
			t.Fatalf("terminal reconciliation error=%v", reconcileErr)
		}
	}
	if reconciled != 1 || unavailable != 1 {
		t.Fatalf("terminal reconciliation results=%d unavailable=%d", reconciled, unavailable)
	}

	// An expired nonterminal child is first fenced and durably timed out, then
	// its authoritative remaining balances are reclaimed in the same commit.
	expiredChild, _, _ := makeChild(admission.EnvelopeID)
	expiresAt := now.Add(15 * time.Second)
	expiredReservation := domain.ResourceReservation{ID: mustNewRepositoryID(t), TenantID: tenant, ParentEnvelopeID: admission.EnvelopeID, ChildEnvelopeID: expiredChild.EnvelopeID, ChildRunID: expiredChild.RunID, CreatedAt: now.Add(14 * time.Second), ExpiresAt: &expiresAt, Amounts: []domain.ResourceReservationAmount{{DimensionID: llmDimension, Coefficient: 5}, {DimensionID: toolDimension, Coefficient: 2}}}
	if _, err := repository.ReserveChildResources(ctx, expiredReservation); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE runs SET state='RUNNING',state_version=2,updated_at=$3,lease_id=$4,lease_expires_at=$5,fencing_token=7 WHERE tenant_id=$1 AND id=$2`, tenant.String(), expiredChild.RunID.String(), now.Add(14*time.Second), mustNewRepositoryID(t).String(), now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	expiredResult, err := repositories.ReconcileNextReservation(ctx, tenant, systemPrincipal, now.Add(40*time.Second), reconcileEvidence())
	if err != nil || !expiredResult.Expired {
		t.Fatalf("expired reconciliation=%+v error=%v", expiredResult, err)
	}
	if _, err := repositories.ReconcileNextReservation(ctx, tenant, systemPrincipal, now.Add(40*time.Second), reconcileEvidence()); !errors.Is(err, domain.ErrResourceReconciliationUnavailable) {
		t.Fatalf("reconciliation replay error=%v", err)
	}
	var orphanSettlements, expiredSettlements, reconciliationAudits, reconciliationOutbox, timeoutEvents int
	var expiredReservationState, expiredRunState string
	var expiredFence int64
	if err := pool.QueryRow(ctx, `SELECT
(SELECT count(*) FROM resource_settlements WHERE tenant_id=$1 AND reservation_id=$2),
(SELECT count(*) FROM resource_settlements WHERE tenant_id=$1 AND reservation_id=$3),
(SELECT count(*) FROM audit_events WHERE tenant_id=$1 AND action='resources.reconcile' AND resource_id IN ($2::text,$3::text)),
(SELECT count(*) FROM outbox_messages WHERE tenant_id=$1 AND event_type='resource.reservation.reconciled' AND aggregate_id IN ($2::text,$3::text)),
(SELECT count(*) FROM run_events WHERE tenant_id=$1 AND run_id=$4 AND event_type='run.timed_out'),
(SELECT state FROM resource_reservations WHERE tenant_id=$1 AND id=$3),
(SELECT state FROM runs WHERE tenant_id=$1 AND id=$4),
(SELECT fencing_token FROM runs WHERE tenant_id=$1 AND id=$4)`, tenant.String(), orphanReservation.ID.String(), expiredReservation.ID.String(), expiredChild.RunID.String()).Scan(&orphanSettlements, &expiredSettlements, &reconciliationAudits, &reconciliationOutbox, &timeoutEvents, &expiredReservationState, &expiredRunState, &expiredFence); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT llm.available_value,llm.allocated_open_value,tool.available_value,tool.allocated_open_value FROM resource_balances llm JOIN resource_balances tool ON tool.tenant_id=llm.tenant_id AND tool.envelope_id=llm.envelope_id WHERE llm.tenant_id=$1 AND llm.envelope_id=$2 AND llm.dimension_id=$3 AND tool.dimension_id=$4`, tenant.String(), admission.EnvelopeID.String(), llmDimension.String(), toolDimension.String()).Scan(&llmAvailable, &llmAllocated, &parallelToolAvailable, &parallelToolAllocated); err != nil {
		t.Fatal(err)
	}
	if orphanSettlements != 1 || expiredSettlements != 1 || reconciliationAudits != 2 || reconciliationOutbox != 2 || timeoutEvents != 1 || expiredReservationState != "EXPIRED_SETTLED" || expiredRunState != "TIMED_OUT" || expiredFence != 8 || llmAvailable != 50 || llmAllocated != 40 || parallelToolAvailable != 30 || parallelToolAllocated != 20 {
		t.Fatalf("reconciliation settlements=%d/%d evidence=%d/%d timeout=%d reservation=%s run=%s fence=%d parent=%d/%d,%d/%d", orphanSettlements, expiredSettlements, reconciliationAudits, reconciliationOutbox, timeoutEvents, expiredReservationState, expiredRunState, expiredFence, llmAvailable, llmAllocated, parallelToolAvailable, parallelToolAllocated)
	}

	// Governed child admission composes the authorized aggregate and reservation
	// atomically. The parent envelope lock makes competing structural checks see
	// a serial order even when consumable capacity would allow both children.
	structuralRoot, structuralResolution, structuralEvidence := makeAdmission()
	structuralRoot.Constraints = map[string]any{"max_llm_tokens": float64(10), "max_active_children": float64(1), "max_total_children": float64(2), "max_delegation_depth": float64(1)}
	structuralResolution.ResolvedConstraints = structuralRoot.Constraints
	if err := repository.AdmitRun(ctx, structuralRoot, structuralResolution, structuralEvidence); err != nil {
		t.Fatal(err)
	}
	makeGovernedChild := func(offset time.Duration) (domain.RunAdmission, domain.RunVersionResolution, ports.RunAdmissionEvidence, domain.ResourceReservation) {
		childAdmission, childResolution, childEvidence := makeAdmission()
		childAdmission.DeadlineAt = nil
		childAdmission.Constraints = map[string]any{"max_active_children": float64(1), "max_total_children": float64(2), "max_delegation_depth": float64(1)}
		childResolution.ResolvedConstraints = childAdmission.Constraints
		reservation := domain.ResourceReservation{ID: mustNewRepositoryID(t), TenantID: tenant, ParentEnvelopeID: structuralRoot.EnvelopeID, ChildEnvelopeID: childAdmission.EnvelopeID, ChildRunID: childAdmission.RunID, CreatedAt: now.Add(offset), Amounts: []domain.ResourceReservationAmount{{DimensionID: llmDimension, Coefficient: 1}}}
		return childAdmission, childResolution, childEvidence, reservation
	}
	expanded, expandedResolution, expandedEvidence, expandedReservation := makeGovernedChild(19 * time.Second)
	expanded.Constraints["max_active_children"] = float64(2)
	if _, err := repository.AdmitChildRun(ctx, expanded, expandedResolution, expandedEvidence, expandedReservation); domain.ErrorCodeOf(err) != domain.CodeConflict {
		t.Fatalf("expanded child authority error=%v", err)
	}
	var rolledBackExpanded int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM runs WHERE tenant_id=$1 AND id=$2`, tenant.String(), expanded.RunID.String()).Scan(&rolledBackExpanded); err != nil || rolledBackExpanded != 0 {
		t.Fatalf("expanded child remained durable count=%d error=%v", rolledBackExpanded, err)
	}
	governed := make([]struct {
		admission   domain.RunAdmission
		resolution  domain.RunVersionResolution
		evidence    ports.RunAdmissionEvidence
		reservation domain.ResourceReservation
	}, 2)
	for index := range governed {
		governed[index].admission, governed[index].resolution, governed[index].evidence, governed[index].reservation = makeGovernedChild(time.Duration(20+index) * time.Second)
	}
	governedErrors := make(chan error, 2)
	for index := range governed {
		go func(candidate struct {
			admission   domain.RunAdmission
			resolution  domain.RunVersionResolution
			evidence    ports.RunAdmissionEvidence
			reservation domain.ResourceReservation
		}) {
			_, err := repository.AdmitChildRun(ctx, candidate.admission, candidate.resolution, candidate.evidence, candidate.reservation)
			governedErrors <- err
		}(governed[index])
	}
	var admittedChildren, rejectedChildren int
	for range governed {
		if err := <-governedErrors; err == nil {
			admittedChildren++
		} else if domain.ErrorCodeOf(err) == domain.CodeConflict {
			rejectedChildren++
		} else {
			t.Fatalf("governed child admission error=%v", err)
		}
	}
	var durableChildren, structuralReservations int
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM resource_envelopes WHERE tenant_id=$1 AND parent_envelope_id=$2),(SELECT count(*) FROM resource_reservations WHERE tenant_id=$1 AND parent_envelope_id=$2)`, tenant.String(), structuralRoot.EnvelopeID.String()).Scan(&durableChildren, &structuralReservations); err != nil {
		t.Fatal(err)
	}
	if admittedChildren != 1 || rejectedChildren != 1 || durableChildren != 1 || structuralReservations != 1 {
		t.Fatalf("governed admissions=%d/%d durable=%d reservations=%d", admittedChildren, rejectedChildren, durableChildren, structuralReservations)
	}

	// Closing the first reservation simulates the RES-009 settlement state for
	// the structural counters: active capacity reopens, lifetime capacity does
	// not. A second child is admitted; a third conflicts on total children.
	if _, err := pool.Exec(ctx, `UPDATE resource_reservations SET state='SETTLED',settled_at=$3 WHERE tenant_id=$1 AND parent_envelope_id=$2 AND state='OPEN'`, tenant.String(), structuralRoot.EnvelopeID.String(), now.Add(30*time.Second)); err != nil {
		t.Fatal(err)
	}
	second, secondResolution, secondEvidence, secondReservation := makeGovernedChild(31 * time.Second)
	if _, err := repository.AdmitChildRun(ctx, second, secondResolution, secondEvidence, secondReservation); err != nil {
		t.Fatalf("second lifetime child admission error=%v", err)
	}
	third, thirdResolution, thirdEvidence, thirdReservation := makeGovernedChild(32 * time.Second)
	if _, err := repository.AdmitChildRun(ctx, third, thirdResolution, thirdEvidence, thirdReservation); domain.ErrorCodeOf(err) != domain.CodeConflict {
		t.Fatalf("total-child ceiling error=%v", err)
	}
	var rolledBackThird int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM runs WHERE tenant_id=$1 AND id=$2`, tenant.String(), third.RunID.String()).Scan(&rolledBackThird); err != nil || rolledBackThird != 0 {
		t.Fatalf("rejected child remained durable count=%d error=%v", rolledBackThird, err)
	}

	// Terminal settlement uses the child's authoritative balance, closes its
	// open reservation, and returns unused capacity to the parent exactly once.
	settledAt := now.Add(34 * time.Second)
	if _, err := pool.Exec(ctx, `UPDATE runs SET state='COMPLETED',state_version=2,updated_at=$3,terminal_at=$3 WHERE tenant_id=$1 AND id=$2`, tenant.String(), second.RunID.String(), settledAt); err != nil {
		t.Fatal(err)
	}
	settlement := domain.ResourceSettlement{ID: mustNewRepositoryID(t), TenantID: tenant, ReservationID: secondReservation.ID, ActorPrincipalID: principal, PolicyDecisionID: mustNewRepositoryID(t), IdempotencyKey: "settle-integration-1", TerminalRunState: "COMPLETED", SettledAt: settledAt}
	settlementEvidence := ports.ResourceSettlementEvidence{AuditID: mustNewRepositoryID(t), OutboxID: mustNewRepositoryID(t), RequestID: mustNewRepositoryID(t), ReasonCodes: []string{"workload.operation.allowed"}}
	settled, err := repository.SettleReservation(ctx, settlement, settlementEvidence)
	if err != nil || settled.Duplicate || len(settled.Returned) != 1 || settled.Returned[0].Value != 1 || settled.Consumed[0].Value != 0 {
		t.Fatalf("settlement=%+v error=%v", settled, err)
	}
	replayedSettlement, err := repository.SettleReservation(ctx, settlement, settlementEvidence)
	if err != nil || !replayedSettlement.Duplicate || replayedSettlement.ID != settled.ID {
		t.Fatalf("settlement replay=%+v error=%v", replayedSettlement, err)
	}
	mismatchedSettlement := settlement
	mismatchedSettlement.TerminalRunState = "FAILED"
	if _, err := repository.SettleReservation(ctx, mismatchedSettlement, settlementEvidence); domain.ErrorCodeOf(err) != domain.CodeConflict {
		t.Fatalf("mismatched settlement replay error=%v", err)
	}
	var settlementRows, settlementItems, settlementAudits, settlementOutbox int
	var settledState string
	var settledAvailable, settledAllocated int64
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM resource_settlements WHERE tenant_id=$1 AND reservation_id=$2),(SELECT count(*) FROM resource_settlement_items WHERE tenant_id=$1 AND settlement_id=$3),(SELECT count(*) FROM audit_events WHERE tenant_id=$1 AND action='resources.settle' AND resource_id=$2::text),(SELECT count(*) FROM outbox_messages WHERE tenant_id=$1 AND event_type='resource.reservation.settled' AND aggregate_id=$2::text),(SELECT state FROM resource_reservations WHERE tenant_id=$1 AND id=$2),(SELECT available_value FROM resource_balances WHERE tenant_id=$1 AND envelope_id=$4 AND dimension_id=$5),(SELECT allocated_open_value FROM resource_balances WHERE tenant_id=$1 AND envelope_id=$4 AND dimension_id=$5)`, tenant.String(), secondReservation.ID.String(), settlement.ID.String(), structuralRoot.EnvelopeID.String(), llmDimension.String()).Scan(&settlementRows, &settlementItems, &settlementAudits, &settlementOutbox, &settledState, &settledAvailable, &settledAllocated); err != nil {
		t.Fatal(err)
	}
	if settlementRows != 1 || settlementItems != 1 || settlementAudits != 1 || settlementOutbox != 1 || settledState != "SETTLED" || settledAvailable != 9 || settledAllocated != 1 {
		t.Fatalf("settlement rows=%d items=%d evidence=%d/%d state=%s parent=%d/%d", settlementRows, settlementItems, settlementAudits, settlementOutbox, settledState, settledAvailable, settledAllocated)
	}

	// A grandchild would be depth two while the inherited absolute ceiling is
	// one, so its entire admission is rolled back.
	grandchild, grandchildResolution, grandchildEvidence, grandchildReservation := makeGovernedChild(33 * time.Second)
	grandchildReservation.ParentEnvelopeID = second.EnvelopeID
	if _, err := repository.AdmitChildRun(ctx, grandchild, grandchildResolution, grandchildEvidence, grandchildReservation); domain.ErrorCodeOf(err) != domain.CodeConflict {
		t.Fatalf("delegation-depth ceiling error=%v", err)
	}
	var rolledBackGrandchild int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM runs WHERE tenant_id=$1 AND id=$2`, tenant.String(), grandchild.RunID.String()).Scan(&rolledBackGrandchild); err != nil || rolledBackGrandchild != 0 {
		t.Fatalf("rejected grandchild remained durable count=%d error=%v", rolledBackGrandchild, err)
	}
	outOfBoundsAdmission, outOfBoundsResolution, outOfBoundsEvidence := makeAdmission()
	outOfBoundsAdmission.Constraints["max_llm_tokens"] = float64(1001)
	outOfBoundsResolution.ResolvedConstraints["max_llm_tokens"] = float64(1001)
	if err := repository.AdmitRun(ctx, outOfBoundsAdmission, outOfBoundsResolution, outOfBoundsEvidence); domain.ErrorCodeOf(err) != domain.CodeUnavailable {
		t.Fatalf("out-of-bounds grant error=%v", err)
	}
	var rolledBackRuns, rolledBackEnvelopes int
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM runs WHERE tenant_id=$1 AND id=$2),(SELECT count(*) FROM resource_envelopes WHERE tenant_id=$1 AND id=$3)`, tenant.String(), outOfBoundsAdmission.RunID.String(), outOfBoundsAdmission.EnvelopeID.String()).Scan(&rolledBackRuns, &rolledBackEnvelopes); err != nil {
		t.Fatal(err)
	}
	if rolledBackRuns != 0 || rolledBackEnvelopes != 0 {
		t.Fatalf("invalid grant left runs=%d envelopes=%d", rolledBackRuns, rolledBackEnvelopes)
	}

	expectedVersion := int64(1)
	signal := domain.RunSignal{ID: mustNewRepositoryID(t), TenantID: tenant, RunID: admission.RunID, ActorPrincipalID: principal, Type: domain.RunSignalCustom, Payload: []byte(`{"name":"runtime.refresh","data":{"scope":"tools"}}`), IdempotencyKey: "integration-signal-0001", ExpectedStateVersion: &expectedVersion, CreatedAt: now.Add(time.Second)}
	signalEvidence := ports.RunSignalEvidence{EventID: mustNewRepositoryID(t), AuditID: mustNewRepositoryID(t), OutboxID: mustNewRepositoryID(t), RequestID: mustNewRepositoryID(t), PolicyDecisionID: mustNewRepositoryID(t), ReasonCodes: []string{"run.access.allowed"}}
	accepted, err := repository.AppendRunSignal(ctx, signal, signalEvidence)
	if err != nil || accepted.Sequence != 2 || accepted.Type != "run.signal.accepted" {
		t.Fatalf("accepted signal=%+v error=%v", accepted, err)
	}
	replayed, err := repository.AppendRunSignal(ctx, signal, signalEvidence)
	if err != nil || replayed.ID != accepted.ID || replayed.Sequence != accepted.Sequence {
		t.Fatalf("replayed signal=%+v error=%v", replayed, err)
	}
	if _, err := otherRepository.AppendRunSignal(ctx, signal, signalEvidence); err == nil {
		t.Fatal("cross-tenant signal unexpectedly succeeded")
	}
	eventWindow, err := repository.ListRunEvents(ctx, admission.RunID, 0, 128)
	if err != nil || eventWindow.OldestRetained != 1 || eventWindow.Latest != 2 || len(eventWindow.Events) != 2 || eventWindow.Events[0].Sequence != 1 || eventWindow.Events[1].Sequence != 2 {
		t.Fatalf("ordered event window=%+v error=%v", eventWindow, err)
	}
	resumedWindow, err := repository.ListRunEvents(ctx, admission.RunID, 1, 128)
	if err != nil || len(resumedWindow.Events) != 1 || resumedWindow.Events[0].ID != accepted.ID || resumedWindow.Events[0].Sequence != 2 {
		t.Fatalf("resumed event window=%+v error=%v", resumedWindow, err)
	}
	if _, err := otherRepository.ListRunEvents(ctx, admission.RunID, 0, 128); domain.ErrorCodeOf(err) != domain.CodeNotFound {
		t.Fatalf("cross-tenant event query error=%v", err)
	}
	stale := signal
	stale.ID, stale.IdempotencyKey = mustNewRepositoryID(t), "integration-signal-0002"
	wrongVersion := int64(2)
	stale.ExpectedStateVersion = &wrongVersion
	if _, err := repository.AppendRunSignal(ctx, stale, ports.RunSignalEvidence{EventID: mustNewRepositoryID(t), AuditID: mustNewRepositoryID(t), OutboxID: mustNewRepositoryID(t), RequestID: mustNewRepositoryID(t), PolicyDecisionID: mustNewRepositoryID(t), ReasonCodes: []string{"run.access.allowed"}}); domain.ErrorCodeOf(err) != domain.CodeConflict {
		t.Fatalf("stale signal error=%v", err)
	}
	const parallelSignals = 8
	var group sync.WaitGroup
	errorsBySignal := make(chan error, parallelSignals)
	for index := range parallelSignals {
		parallel := signal
		parallel.ID = mustNewRepositoryID(t)
		parallel.IdempotencyKey = fmt.Sprintf("integration-signal-parallel-%02d", index)
		evidence := ports.RunSignalEvidence{EventID: mustNewRepositoryID(t), AuditID: mustNewRepositoryID(t), OutboxID: mustNewRepositoryID(t), RequestID: mustNewRepositoryID(t), PolicyDecisionID: mustNewRepositoryID(t), ReasonCodes: []string{"run.access.allowed"}}
		group.Add(1)
		go func() {
			defer group.Done()
			_, appendErr := repository.AppendRunSignal(ctx, parallel, evidence)
			errorsBySignal <- appendErr
		}()
	}
	group.Wait()
	close(errorsBySignal)
	for appendErr := range errorsBySignal {
		if appendErr != nil {
			t.Fatalf("parallel signal: %v", appendErr)
		}
	}
	var signalCount, signalEvents, signalAudits, signalOutbox int
	for query, target := range map[string]*int{
		`SELECT count(*) FROM run_signals WHERE tenant_id=$1 AND run_id=$2`:                                                &signalCount,
		`SELECT count(*) FROM run_events WHERE tenant_id=$1 AND run_id=$2 AND event_type='run.signal.accepted'`:            &signalEvents,
		`SELECT count(*) FROM audit_events WHERE tenant_id=$1 AND resource_id=$2 AND action='runs.signal'`:                 &signalAudits,
		`SELECT count(*) FROM outbox_messages WHERE tenant_id=$1 AND aggregate_id=$2 AND event_type='run.signal.accepted'`: &signalOutbox,
	} {
		if err := pool.QueryRow(ctx, query, tenant.String(), admission.RunID.String()).Scan(target); err != nil {
			t.Fatal(err)
		}
	}
	if signalCount != 1+parallelSignals || signalEvents != 1+parallelSignals || signalAudits != 1+parallelSignals || signalOutbox != 1+parallelSignals {
		t.Fatalf("signals=%d events=%d audits=%d outbox=%d", signalCount, signalEvents, signalAudits, signalOutbox)
	}
	var distinctSequences, minimumSequence, maximumSequence int
	if err := pool.QueryRow(ctx, `SELECT count(DISTINCT sequence),min(sequence),max(sequence) FROM run_events WHERE tenant_id=$1 AND run_id=$2 AND event_type='run.signal.accepted'`, tenant.String(), admission.RunID.String()).Scan(&distinctSequences, &minimumSequence, &maximumSequence); err != nil {
		t.Fatal(err)
	}
	if distinctSequences != 1+parallelSignals || minimumSequence != 2 || maximumSequence != 2+parallelSignals {
		t.Fatalf("ordered signal sequences distinct=%d range=%d..%d", distinctSequences, minimumSequence, maximumSequence)
	}

	cancelExpected := int64(1)
	cancelAdmission, cancelResolution, cancelAdmissionEvidence := makeAdmission()
	if err := repository.AdmitRun(ctx, cancelAdmission, cancelResolution, cancelAdmissionEvidence); err != nil {
		t.Fatal(err)
	}
	cancellation := domain.RunCancellation{TenantID: tenant, RunID: cancelAdmission.RunID, ActorPrincipalID: principal, IdempotencyKey: "cancel:integration-cancel-0001", ReasonCode: "caller.request", ExpectedStateVersion: &cancelExpected, CreatedAt: now.Add(2 * time.Second)}
	cancellationEvidence := ports.RunCancellationEvidence{SignalID: mustNewRepositoryID(t), EventID: mustNewRepositoryID(t), AuditID: mustNewRepositoryID(t), OutboxID: mustNewRepositoryID(t), RequestID: mustNewRepositoryID(t), PolicyDecisionID: mustNewRepositoryID(t), ReasonCodes: []string{"run.access.allowed"}}
	cancelled, err := repository.CancelRun(ctx, cancellation, cancellationEvidence)
	if err != nil || cancelled.State != domain.RunCancelled || cancelled.StateVersion != 2 {
		t.Fatalf("cancelled run=%+v error=%v", cancelled, err)
	}
	replayedCancellation, err := repository.CancelRun(ctx, cancellation, cancellationEvidence)
	if err != nil || replayedCancellation.State != domain.RunCancelled || replayedCancellation.StateVersion != 2 {
		t.Fatalf("replayed cancellation=%+v error=%v", replayedCancellation, err)
	}
	var cancellationSignals, cancellationEvents, cancellationAudits, cancellationOutbox int
	for query, target := range map[string]*int{
		`SELECT count(*) FROM run_signals WHERE tenant_id=$1 AND run_id=$2 AND signal_type='CANCEL'`:                 &cancellationSignals,
		`SELECT count(*) FROM run_events WHERE tenant_id=$1 AND run_id=$2 AND event_type='run.cancelled'`:            &cancellationEvents,
		`SELECT count(*) FROM audit_events WHERE tenant_id=$1 AND resource_id=$2 AND action='runs.cancel'`:           &cancellationAudits,
		`SELECT count(*) FROM outbox_messages WHERE tenant_id=$1 AND aggregate_id=$2 AND event_type='run.cancelled'`: &cancellationOutbox,
	} {
		if err := pool.QueryRow(ctx, query, tenant.String(), cancelAdmission.RunID.String()).Scan(target); err != nil {
			t.Fatal(err)
		}
	}
	if cancellationSignals != 1 || cancellationEvents != 1 || cancellationAudits != 1 || cancellationOutbox != 1 {
		t.Fatalf("cancellation evidence signals=%d events=%d audits=%d outbox=%d", cancellationSignals, cancellationEvents, cancellationAudits, cancellationOutbox)
	}
	if _, err := otherRepository.CancelRun(ctx, cancellation, cancellationEvidence); err == nil {
		t.Fatal("cross-tenant cancellation unexpectedly succeeded")
	}

	staleAdmission, staleResolution, staleAdmissionEvidence := makeAdmission()
	if err := repository.AdmitRun(ctx, staleAdmission, staleResolution, staleAdmissionEvidence); err != nil {
		t.Fatal(err)
	}
	staleExpected := int64(2)
	staleCancellation := cancellation
	staleCancellation.RunID, staleCancellation.IdempotencyKey, staleCancellation.ExpectedStateVersion = staleAdmission.RunID, "cancel:integration-cancel-stale", &staleExpected
	if _, err := repository.CancelRun(ctx, staleCancellation, ports.RunCancellationEvidence{SignalID: mustNewRepositoryID(t), EventID: mustNewRepositoryID(t), AuditID: mustNewRepositoryID(t), OutboxID: mustNewRepositoryID(t), RequestID: mustNewRepositoryID(t), PolicyDecisionID: mustNewRepositoryID(t), ReasonCodes: []string{"run.access.allowed"}}); domain.ErrorCodeOf(err) != domain.CodeConflict {
		t.Fatalf("stale cancellation error=%v", err)
	}

	// Race cancellation against a worker terminal transition. Both contenders
	// lock the same row; exactly one terminal state wins and cancellation never
	// overwrites an established terminal state.
	raceAdmission, raceResolution, raceAdmissionEvidence := makeAdmission()
	if err := repository.AdmitRun(ctx, raceAdmission, raceResolution, raceAdmissionEvidence); err != nil {
		t.Fatal(err)
	}
	raceCancellation := cancellation
	raceCancellation.RunID, raceCancellation.IdempotencyKey = raceAdmission.RunID, "cancel:integration-cancel-race"
	raceEvidence := ports.RunCancellationEvidence{SignalID: mustNewRepositoryID(t), EventID: mustNewRepositoryID(t), AuditID: mustNewRepositoryID(t), OutboxID: mustNewRepositoryID(t), RequestID: mustNewRepositoryID(t), PolicyDecisionID: mustNewRepositoryID(t), ReasonCodes: []string{"run.access.allowed"}}
	startRace := make(chan struct{})
	raceErrors := make(chan error, 2)
	go func() {
		<-startRace
		_, cancelErr := repository.CancelRun(ctx, raceCancellation, raceEvidence)
		raceErrors <- cancelErr
	}()
	go func() {
		<-startRace
		_, workerErr := pool.Exec(ctx, `UPDATE runs SET state='COMPLETED',state_version=state_version+1,updated_at=$3,terminal_at=$3 WHERE tenant_id=$1 AND id=$2 AND state='ADMITTED'`, tenant.String(), raceAdmission.RunID.String(), now.Add(3*time.Second))
		raceErrors <- workerErr
	}()
	close(startRace)
	for range 2 {
		if raceErr := <-raceErrors; raceErr != nil {
			t.Fatalf("terminal race: %v", raceErr)
		}
	}
	raceProjection, err := repository.GetRun(ctx, raceAdmission.RunID)
	if err != nil || (raceProjection.Run.State != domain.RunCancelled && raceProjection.Run.State != domain.RunCompleted) || raceProjection.Run.StateVersion != 2 {
		t.Fatalf("terminal race projection=%+v error=%v", raceProjection, err)
	}
	var raceCancellationEvents int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM run_events WHERE tenant_id=$1 AND run_id=$2 AND event_type='run.cancelled'`, tenant.String(), raceAdmission.RunID.String()).Scan(&raceCancellationEvents); err != nil {
		t.Fatal(err)
	}
	if raceProjection.Run.State == domain.RunCancelled && raceCancellationEvents != 1 || raceProjection.Run.State == domain.RunCompleted && raceCancellationEvents != 0 {
		t.Fatalf("terminal race state=%s cancellation_events=%d", raceProjection.Run.State, raceCancellationEvents)
	}

	failedAdmission, failedResolution, failedEvidence := makeAdmission()
	failedEvidence.OutboxID = evidence.OutboxID
	if err := repository.AdmitRun(ctx, failedAdmission, failedResolution, failedEvidence); domain.ErrorCodeOf(err) != domain.CodeConflict {
		t.Fatalf("duplicate evidence error=%v", err)
	}
	var remaining int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM runs WHERE tenant_id=$1 AND id=$2`, tenant.String(), failedAdmission.RunID.String()).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 0 {
		t.Fatalf("failed admission left %d run rows", remaining)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM resource_dimensions WHERE tenant_id=$1 AND id=$2`, tenant.String(), llmDimension.String()); err == nil {
		t.Fatal("dimension with grants unexpectedly deleted")
	}

	// Drain earlier claimable fixtures so lease recovery can be asserted against
	// one deterministic run in this tenant.
	workerID := mustNewRepositoryID(t)
	claimAt := now.Add(10 * time.Second)
	for {
		lease, claimErr := repositories.ClaimRun(ctx, tenant, workerID, mustNewRepositoryID(t), claimAt, claimAt.Add(time.Minute))
		if domain.ErrorCodeOf(claimErr) == domain.CodeNotFound {
			break
		}
		if claimErr != nil {
			t.Fatal(claimErr)
		}
		if _, err := repositories.MutateWorkerRun(ctx, lease, domain.WorkerRunStart, mustNewRepositoryID(t), claimAt.Add(time.Second)); err != nil {
			t.Fatal(err)
		}
		if _, err := repositories.MutateWorkerRun(ctx, lease, domain.WorkerRunComplete, mustNewRepositoryID(t), claimAt.Add(2*time.Second)); err != nil {
			t.Fatal(err)
		}
	}

	workerAdmission, workerResolution, workerEvidence := makeAdmission()
	if err := repository.AdmitRun(ctx, workerAdmission, workerResolution, workerEvidence); err != nil {
		t.Fatal(err)
	}
	firstLease, err := repositories.ClaimRun(ctx, tenant, workerID, mustNewRepositoryID(t), claimAt, claimAt.Add(time.Minute))
	if err != nil || firstLease.RunID != workerAdmission.RunID || firstLease.FencingToken != 1 {
		t.Fatalf("first lease=%+v error=%v", firstLease, err)
	}
	firstLease, err = repositories.HeartbeatRun(ctx, firstLease, claimAt.Add(30*time.Second), claimAt.Add(90*time.Second))
	if err != nil || firstLease.ExpiresAt != claimAt.Add(90*time.Second) {
		t.Fatalf("heartbeat=%+v error=%v", firstLease, err)
	}
	secondWorker := mustNewRepositoryID(t)
	secondLease, err := repositories.ClaimRun(ctx, tenant, secondWorker, mustNewRepositoryID(t), claimAt.Add(91*time.Second), claimAt.Add(3*time.Minute))
	if err != nil || secondLease.RunID != firstLease.RunID || secondLease.FencingToken != firstLease.FencingToken+1 {
		t.Fatalf("reclaimed lease=%+v error=%v", secondLease, err)
	}
	if _, err := repositories.HeartbeatRun(ctx, firstLease, claimAt.Add(92*time.Second), claimAt.Add(4*time.Minute)); domain.ErrorCodeOf(err) != domain.CodeConflict {
		t.Fatalf("stale heartbeat error=%v", err)
	}
	if _, err := repositories.MutateWorkerRun(ctx, firstLease, domain.WorkerRunStart, mustNewRepositoryID(t), claimAt.Add(92*time.Second)); domain.ErrorCodeOf(err) != domain.CodeConflict {
		t.Fatalf("stale start error=%v", err)
	}
	running, err := repositories.MutateWorkerRun(ctx, secondLease, domain.WorkerRunStart, mustNewRepositoryID(t), claimAt.Add(92*time.Second))
	if err != nil || running.State != domain.RunRunning || running.StateVersion != 2 {
		t.Fatalf("running=%+v error=%v", running, err)
	}
	completed, err := repositories.MutateWorkerRun(ctx, secondLease, domain.WorkerRunComplete, mustNewRepositoryID(t), claimAt.Add(93*time.Second))
	if err != nil || completed.State != domain.RunCompleted || completed.StateVersion != 3 {
		t.Fatalf("completed=%+v error=%v", completed, err)
	}
	if _, err := repositories.MutateWorkerRun(ctx, secondLease, domain.WorkerRunFail, mustNewRepositoryID(t), claimAt.Add(94*time.Second)); domain.ErrorCodeOf(err) != domain.CodeConflict {
		t.Fatalf("post-terminal stale mutation error=%v", err)
	}
	var workerEvents, workerDistinctSequences int
	if err := pool.QueryRow(ctx, `SELECT count(*),count(DISTINCT sequence) FROM run_events WHERE tenant_id=$1 AND run_id=$2`, tenant.String(), workerAdmission.RunID.String()).Scan(&workerEvents, &workerDistinctSequences); err != nil {
		t.Fatal(err)
	}
	if workerEvents != 6 || workerDistinctSequences != workerEvents {
		t.Fatalf("worker events=%d distinct sequences=%d", workerEvents, workerDistinctSequences)
	}
	var workerOutbox, matchingWorkerOutbox int
	if err := pool.QueryRow(ctx, `SELECT count(*),count(o.id)
	FROM run_events e LEFT JOIN outbox_messages o ON o.id=e.id AND o.tenant_id=e.tenant_id AND o.aggregate_type='run' AND o.aggregate_id=e.run_id::text AND o.event_type=e.event_type AND o.payload=e.payload
	WHERE e.tenant_id=$1 AND e.run_id=$2 AND e.event_type IN ('run.claimed','run.heartbeat','run.started','run.completed')`, tenant.String(), workerAdmission.RunID.String()).Scan(&workerOutbox, &matchingWorkerOutbox); err != nil {
		t.Fatal(err)
	}
	if workerOutbox != 5 || matchingWorkerOutbox != workerOutbox {
		t.Fatalf("worker outbox=%d matching events=%d", workerOutbox, matchingWorkerOutbox)
	}

	// Simulate a process crash after the sink accepted a worker event but before
	// its outbox row was acknowledged. Lease expiry must redeliver the same ID.
	crashSink := &recordingSink{}
	crashPublisher, err := NewOutboxPublisher(pool, crashSink, OutboxConfig{WorkerID: "run009-crash-worker", BatchSize: 1000, MaxAttempts: 3, Lease: time.Minute, BaseRetry: time.Second, MaxRetry: time.Minute, Jitter: func(time.Duration) time.Duration { return 0 }})
	if err != nil {
		t.Fatal(err)
	}
	crashAt := claimAt.Add(10 * time.Minute)
	claimed, err := crashPublisher.claim(ctx, crashAt)
	if err != nil {
		t.Fatal(err)
	}
	var crashedMessage *ClaimedMessage
	for index := range claimed {
		if claimed[index].AggregateID == workerAdmission.RunID.String() && claimed[index].EventType == "run.completed" {
			crashedMessage = &claimed[index]
			break
		}
	}
	if crashedMessage == nil {
		t.Fatal("completed worker outbox message was not claimed")
	}
	if err := crashSink.Send(ctx, crashedMessage.OutboxMessage); err != nil {
		t.Fatal(err)
	}
	if _, err := crashPublisher.PublishBatch(ctx, crashAt.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if crashSink.count(crashedMessage.ID) != 2 {
		t.Fatalf("crash replay deliveries=%d want 2", crashSink.count(crashedMessage.ID))
	}
	var replayPublished *time.Time
	if err := pool.QueryRow(ctx, `SELECT published_at FROM outbox_messages WHERE id=$1`, crashedMessage.ID.String()).Scan(&replayPublished); err != nil || replayPublished == nil {
		t.Fatalf("replayed publication=%v error=%v", replayPublished, err)
	}

	timeoutAdmission, timeoutResolution, timeoutEvidence := makeAdmission()
	if err := repository.AdmitRun(ctx, timeoutAdmission, timeoutResolution, timeoutEvidence); err != nil {
		t.Fatal(err)
	}
	type claimResult struct {
		lease domain.RunLease
		err   error
	}
	claimResults := make(chan claimResult, 2)
	claimStart := make(chan struct{})
	claimWorkers := []domain.ID{mustNewRepositoryID(t), mustNewRepositoryID(t)}
	claimLeases := []domain.ID{mustNewRepositoryID(t), mustNewRepositoryID(t)}
	for index := range 2 {
		go func(workerID, leaseID domain.ID) {
			<-claimStart
			lease, claimErr := repositories.ClaimRun(ctx, tenant, workerID, leaseID, claimAt.Add(4*time.Minute), claimAt.Add(5*time.Minute))
			claimResults <- claimResult{lease: lease, err: claimErr}
		}(claimWorkers[index], claimLeases[index])
	}
	close(claimStart)
	var timeoutLease domain.RunLease
	var successfulClaims, unavailableClaims int
	for range 2 {
		result := <-claimResults
		if result.err == nil {
			successfulClaims++
			timeoutLease = result.lease
		} else if domain.ErrorCodeOf(result.err) == domain.CodeNotFound {
			unavailableClaims++
		} else {
			t.Fatalf("concurrent claim error=%v", result.err)
		}
	}
	if successfulClaims != 1 || unavailableClaims != 1 || timeoutLease.RunID != timeoutAdmission.RunID {
		t.Fatalf("concurrent claims successful=%d unavailable=%d lease=%+v", successfulClaims, unavailableClaims, timeoutLease)
	}
	timedOut, err := repositories.MutateWorkerRun(ctx, timeoutLease, domain.WorkerRunTimeout, mustNewRepositoryID(t), claimAt.Add(4*time.Minute+time.Second))
	if err != nil || timedOut.State != domain.RunTimedOut || timedOut.StateVersion != 2 {
		t.Fatalf("timed out=%+v error=%v", timedOut, err)
	}
}
