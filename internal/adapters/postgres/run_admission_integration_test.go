//go:build integration

package postgres

import (
	"context"
	"os"
	"strings"
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

	tenant, principal, sponsor, agentID, versionID, approvalID := mustNewRepositoryID(t), mustNewRepositoryID(t), mustNewRepositoryID(t), mustNewRepositoryID(t), mustNewRepositoryID(t), mustNewRepositoryID(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	_, err = pool.Exec(ctx, `INSERT INTO tenants(id,slug,display_name,created_at,updated_at)VALUES($1,$2,$2,$3,$3)`, tenant.String(), "run002-"+tenant.String(), now)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []domain.ID{principal, sponsor} {
		if _, err := pool.Exec(ctx, `INSERT INTO principals(id,tenant_id,external_issuer,external_subject,principal_type,created_at)VALUES($1,$2,'https://run002.test',$3,'HUMAN',$4)`, id.String(), tenant.String(), id.String(), now); err != nil {
			t.Fatal(err)
		}
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
		constraints := map[string]any{"max_execution_time_seconds": float64(60), "max_llm_tokens": float64(100)}
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
}
