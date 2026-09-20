//go:build integration

package postgres

import (
	"context"
	"github.com/bdobrica/ThinkPixelAG/internal/adapters/localapprovals"
	"github.com/bdobrica/ThinkPixelAG/internal/application"
	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/identity"
	"testing"
	"time"
)

func TestOperatorRecoveryPreservesIndependentExpansionApproval(t *testing.T) {
	f := newAdministrationFixture(t)
	ctx := context.Background()
	issuer := "https://admin.test"
	second := mustNewRepositoryID(t)
	_, e := f.pool.Exec(ctx, `INSERT INTO principals(id,tenant_id,external_issuer,external_subject,principal_type,created_at)VALUES($1,$2,$3,$4,'HUMAN',$5)`, second.String(), f.tenant.String(), issuer, second.String(), time.Now().UTC())
	if e != nil {
		t.Fatal(e)
	}
	_, e = f.pool.Exec(ctx, `INSERT INTO role_mapping_revisions(tenant_id,issuer,revision,mappings,created_by,created_at)VALUES($1,$2,1,'{"lost-idp-group":"policy-admin"}',$3,$4)`, f.tenant.String(), issuer, f.actor.String(), time.Now().UTC())
	if e != nil {
		t.Fatal(e)
	}
	s := application.OperatorRecovery{Store: f.repo, Issuer: issuer, Channel: "stable", Clock: domain.SystemClock{}, Provider: &localapprovals.Provider{Store: f.repo}}
	// No mapped administrator role remains for either verified identity.
	p := identity.Principal{TenantID: f.tenant.String(), ID: f.actor.String(), Issuer: issuer}
	b := application.UpdateRoleMappings{ExpectedRevision: 1, Mappings: map[string]string{"recovered-operators": "policy-admin"}, ApprovalReference: "not-required"}
	if _, e = s.Apply(ctx, p, "recovery-no-approval", b); domain.ErrorCodeOf(e) != domain.CodeForbidden {
		t.Fatal("recovery bypassed approval", e)
	}
	a, e := s.Request(ctx, p, "recovery-request-key", b)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Approve(ctx, p, "recovery-self-key", a.ID); e == nil {
		t.Fatal("self recovery approval")
	}
	q := p
	q.ID = second.String()
	if _, e = s.Approve(ctx, q, "recovery-second-key", a.ID); e != nil {
		t.Fatal(e)
	}
	b.ApprovalReference = a.ID.String()
	m, e := s.Apply(ctx, p, "recovery-apply-key", b)
	if e != nil || m.Revision != 2 {
		t.Fatal(m, e)
	}
	replay, e := s.Apply(ctx, p, "recovery-apply-key", b)
	if e != nil || replay.Revision != 2 {
		t.Fatal("replay", e)
	}
	foreign := p
	foreign.TenantID = mustNewRepositoryID(t).String()
	if _, e = s.Request(ctx, foreign, "recovery-foreign-key", b); e == nil {
		t.Fatal("foreign recovery")
	}
	var count int
	if e = f.pool.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE tenant_id=$1 AND action='operator.role_mappings.recovery.apply' AND policy_decision_id IS NULL AND metadata->>'authorization_source'='deployment-operator'`, f.tenant.String()).Scan(&count); e != nil || count != 1 {
		t.Fatal("recovery evidence", count, e)
	}
}
