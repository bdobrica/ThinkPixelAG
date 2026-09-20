//go:build integration

package postgres

import (
	"context"
	"github.com/bdobrica/ThinkPixelAG/internal/application"
	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"testing"
	"time"
)

func TestManagedMappingsApprovalRevisionAndIsolation(t *testing.T) {
	f := newAdministrationFixture(t)
	ctx := context.Background()
	issuer := "https://admin.test"
	_, e := f.pool.Exec(ctx, `INSERT INTO role_mapping_revisions(tenant_id,issuer,revision,mappings,created_by,created_at) VALUES($1,$2,1,'{"operators":"policy-admin","old":"registry-admin"}',$3,$4)`, f.tenant.String(), issuer, f.actor.String(), time.Now().UTC())
	if e != nil {
		t.Fatal(e)
	}
	s := &application.RoleMappingAdministration{Policy: f.service, Store: f.repo, Mode: "api", Issuer: issuer}
	c := application.AdminCaller{TenantID: f.tenant, PrincipalID: f.actor, RequestID: mustNewRepositoryID(t), Issuer: issuer, MappingRevision: 1, Roles: []string{"policy-admin"}, Key: "mapping-update-test-key"}
	proposal := application.UpdateRoleMappings{ExpectedRevision: 1, Mappings: map[string]string{"operators": "policy-admin", "new": "registry-admin"}, ApprovalReference: "not-required"}
	if _, e = s.Update(ctx, c, proposal); domain.ErrorCodeOf(e) != domain.CodeForbidden {
		t.Fatal("expansion without approval", e)
	}
	c.Key = "mapping-approval-test-key"
	a, e := s.RequestApproval(ctx, c, proposal)
	if e != nil {
		t.Fatal(e)
	}
	c.Key = "mapping-self-approve-key"
	if _, e = f.service.DecideApproval(ctx, c, a.ID, true); e == nil {
		t.Fatal("self approval")
	}
	second := mustNewRepositoryID(t)
	_, e = f.pool.Exec(ctx, `INSERT INTO principals(id,tenant_id,external_issuer,external_subject,principal_type,created_at)VALUES($1,$2,$3,$5,'HUMAN',$4)`, second.String(), f.tenant.String(), issuer, time.Now().UTC(), second.String())
	if e != nil {
		t.Fatal(e)
	}
	approver := c
	approver.PrincipalID = second
	approver.Key = "mapping-second-approve-key"
	if _, e = f.service.DecideApproval(ctx, approver, a.ID, true); e != nil {
		t.Fatal(e)
	}
	proposal.ApprovalReference = a.ID.String()
	c.Key = "mapping-approved-update-key"
	m, e := s.Update(ctx, c, proposal)
	if e != nil || m.Revision != 2 {
		t.Fatal(m, e)
	}
	// A request authorized from the old mapping cannot mutate after removal.
	c.Key = "mapping-stale-policy-key"
	if _, e = f.service.SaveDraft(ctx, c, domain.ID{}, application.SavePolicyDraft{Source: administrationPolicySource}); domain.ErrorCodeOf(e) != domain.CodeConflict {
		t.Fatal("stale privileged write", e)
	}
	c.MappingRevision = 2
	c.Key = "mapping-current-update-key"
	proposal.ExpectedRevision = 2
	proposal.Mappings = map[string]string{"users": "agent-invoker"}
	if _, e = s.Update(ctx, c, proposal); domain.ErrorCodeOf(e) != domain.CodeConflict {
		t.Fatal("last admin", e)
	}
	proposal.Mappings = map[string]string{"operators": "policy-admin", "workload": "tool-gateway"}
	if _, e = s.Update(ctx, c, proposal); domain.ErrorCodeOf(e) != domain.CodeInvalidArgument {
		t.Fatal("service assignment", e)
	}
	s.Mode = "file"
	if _, e = s.Update(ctx, c, proposal); domain.ErrorCodeOf(e) != domain.CodeForbidden {
		t.Fatal("file write", e)
	}
	foreign := &TenantRepository{tenantID: mustNewRepositoryID(t), db: f.pool}
	if _, e = foreign.RoleMappings(ctx, issuer); domain.ErrorCodeOf(e) != domain.CodeUnavailable {
		t.Fatal("tenant isolation", e)
	}
}
