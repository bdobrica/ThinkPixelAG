package postgres

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/evidence"
)

func TestBreakGlassApprovalExpiryAndEvidenceIntegration(t *testing.T) {
	databaseURL := os.Getenv("THINKPIXELAG_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("THINKPIXELAG_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	conn := newMigrationTestDatabase(t, databaseURL)
	migrateAndRequireVersion(t, ctx, conn, os.DirFS(projectMigrationsDir(t)), 18)
	repositories, err := NewRepositories(conn)
	if err != nil {
		t.Fatal(err)
	}
	tenant, requester, approver, approval, grantID := mustNewRepositoryID(t), mustNewRepositoryID(t), mustNewRepositoryID(t), mustNewRepositoryID(t), mustNewRepositoryID(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	expires := now.Add(10 * time.Minute)
	digest, err := domain.BreakGlassGrantDigest(tenant, requester, domain.BreakGlassPolicyRecovery, "policy_channel", "stable", "security.recovery", expires)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = conn.Exec(ctx, `INSERT INTO tenants(id,slug,display_name,created_at,updated_at) VALUES($1,$2,'break glass',$3,$3)`, tenant.String(), "break-glass-"+strings.ReplaceAll(tenant.String(), "-", ""), now); err != nil {
		t.Fatal(err)
	}
	for _, principal := range []domain.ID{requester, approver} {
		if _, err = conn.Exec(ctx, `INSERT INTO principals(id,tenant_id,external_issuer,external_subject,principal_type,created_at) VALUES($1::uuid,$2,'https://strong-auth.test',($1::uuid)::text,'HUMAN',$3)`, principal.String(), tenant.String(), now); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = conn.Exec(ctx, `INSERT INTO governance_approval_requests(id,tenant_id,requester_principal_id,action,resource_type,resource_id,request_digest,reason_code,provider,provider_reference,requested_at,expires_at) VALUES($1,$2,$3,'POLICY_BYPASS','break_glass',$4,$5,'security.recovery','test','request-1',$6,$7)`, approval.String(), tenant.String(), requester.String(), grantID.String(), digest, now.Add(-time.Minute), expires); err != nil {
		t.Fatal(err)
	}
	if _, err = conn.Exec(ctx, `INSERT INTO governance_approval_decisions(approval_id,tenant_id,requester_principal_id,approver_principal_id,approved,decision_reference,decided_at) VALUES($1,$2,$3,$4,true,'decision-1',$5)`, approval.String(), tenant.String(), requester.String(), approver.String(), now.Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	repo, err := repositories.ForTenant(tenant)
	if err != nil {
		t.Fatal(err)
	}
	grant := domain.BreakGlassGrant{ID: grantID, TenantID: tenant, PrincipalID: requester, ApprovalID: approval, Scope: domain.BreakGlassPolicyRecovery, ResourceType: "policy_channel", ResourceID: "stable", ReasonCode: "security.recovery", GrantDigest: digest, CredentialDigest: domain.DigestBreakGlassCredential("one-time-secret"), StrongAuthenticationReference: "webauthn:assertion-1", IssuedAt: now, ExpiresAt: expires}
	activated := testBreakGlassEvent(t, grant, "ACTIVATED", now)
	if err = repo.ActivateBreakGlass(ctx, grant, activated); err != nil {
		t.Fatal(err)
	}
	if err = repo.ActivateBreakGlass(ctx, grant, activated); err == nil {
		t.Fatal("approval replay accepted")
	}
	used := testBreakGlassEvent(t, grant, "USED", now.Add(time.Minute))
	if err = repo.UseBreakGlass(ctx, tenant, requester, grantID, domain.BreakGlassPolicyRecovery, "policy_channel", "stable", grant.CredentialDigest, now.Add(time.Minute), used); err != nil {
		t.Fatal(err)
	}
	wrong := testBreakGlassEvent(t, grant, "USED", now.Add(2*time.Minute))
	if err = repo.UseBreakGlass(ctx, tenant, requester, grantID, domain.BreakGlassPolicyRecovery, "policy_channel", "other", grant.CredentialDigest, now.Add(2*time.Minute), wrong); err == nil {
		t.Fatal("resource substitution accepted")
	}
	expired := testBreakGlassEvent(t, grant, "EXPIRED", expires)
	if err = repo.ExpireBreakGlass(ctx, tenant, grantID, expires, expired); err != nil {
		t.Fatal(err)
	}
	late := testBreakGlassEvent(t, grant, "USED", expires)
	if err = repo.UseBreakGlass(ctx, tenant, requester, grantID, domain.BreakGlassPolicyRecovery, "policy_channel", "stable", grant.CredentialDigest, expires, late); err == nil {
		t.Fatal("expired grant accepted")
	}
	var events, outbox int
	if err = conn.QueryRow(ctx, `SELECT count(*) FROM break_glass_events WHERE grant_id=$1`, grantID.String()).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if err = conn.QueryRow(ctx, `SELECT count(*) FROM outbox_messages WHERE aggregate_type='break_glass' AND aggregate_id=$1`, grantID.String()).Scan(&outbox); err != nil {
		t.Fatal(err)
	}
	if events != 3 || outbox != 3 {
		t.Fatalf("events=%d outbox=%d, want 3/3", events, outbox)
	}
}

func testBreakGlassEvent(t *testing.T, grant domain.BreakGlassGrant, change string, at time.Time) evidence.Event {
	t.Helper()
	id := mustNewRepositoryID(t)
	event, err := evidence.New(evidence.Event{ID: id, EventType: evidence.BreakGlass, TenantID: &grant.TenantID, Actor: evidence.Actor{Type: "PRINCIPAL", ID: grant.PrincipalID.String()}, Action: "break_glass." + strings.ToLower(change), Outcome: evidence.Succeeded, ReasonCodes: []string{"security.break_glass"}, OccurredAt: at}, evidence.BreakGlassData{SessionID: grant.ID.String(), Scope: string(grant.Scope), GrantDigest: grant.GrantDigest, ApprovalID: grant.ApprovalID.String(), ExpiresAt: grant.ExpiresAt, Change: change})
	if err != nil {
		t.Fatal(err)
	}
	return event
}
