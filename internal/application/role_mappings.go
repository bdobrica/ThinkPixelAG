package application

import (
	"context"
	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
	"time"
)

type RoleMappingAdministration struct {
	Policy       *PolicyAdministration
	Store        ports.RoleMappingStore
	Mode, Issuer string
	File         map[string]string
}
type UpdateRoleMappings struct {
	ExpectedRevision  int64             `json:"expected_revision"`
	Mappings          map[string]string `json:"mappings"`
	ApprovalReference string            `json:"approval_reference"`
}

func (s *RoleMappingAdministration) authorize(ctx context.Context, c AdminCaller, action string, b any) (ports.AdministrationOperation, error) {
	// The closed RC administrator gate supplements, never replaces, policy.
	allowed := false
	for _, r := range c.Roles {
		allowed = allowed || r == "policy-admin"
	}
	if !allowed {
		return ports.AdministrationOperation{}, domain.NewError(domain.CodeForbidden, "policy-administrator required")
	}
	return s.Policy.Authorize(ctx, c, action, "role-mappings", b)
}
func (s *RoleMappingAdministration) Read(ctx context.Context, c AdminCaller) (ports.RoleMappings, error) {
	c.Key = "read-role-mappings"
	if _, e := s.authorize(ctx, c, "role_mappings.read", nil); e != nil {
		return ports.RoleMappings{}, e
	}
	if s.Mode == "file" {
		return ports.RoleMappings{Mode: "file", Issuer: s.Issuer, Mappings: s.File}, nil
	}
	return s.Store.RoleMappings(ctx, s.Issuer)
}
func (s *RoleMappingAdministration) prepare(ctx context.Context, c AdminCaller, b UpdateRoleMappings) (ports.AdministrationOperation, ports.RoleMappings, error) {
	op, e := s.authorize(ctx, c, "role_mappings.manage", b)
	if e != nil {
		return op, ports.RoleMappings{}, e
	}
	if s.Mode != "api" {
		return op, ports.RoleMappings{}, domain.NewError(domain.CodeForbidden, "role mappings are file-managed")
	}
	if b.ExpectedRevision < 1 || b.ExpectedRevision == 1<<63-1 {
		return op, ports.RoleMappings{}, domain.NewError(domain.CodeInvalidArgument, "invalid expected revision")
	}
	if e = domain.ValidateRoleMappings(b.Mappings); e != nil {
		return op, ports.RoleMappings{}, e
	}
	old, e := s.Store.RoleMappings(ctx, s.Issuer)

	return op, old, e
}
func (s *RoleMappingAdministration) Update(ctx context.Context, c AdminCaller, b UpdateRoleMappings) (ports.RoleMappings, error) {
	op, _, e := s.prepare(ctx, c, b)
	if e != nil {
		return ports.RoleMappings{}, e
	}
	return s.Store.SaveRoleMappings(ctx, op, ports.RoleMappings{Issuer: s.Issuer, Revision: b.ExpectedRevision, Mappings: b.Mappings}, b.ApprovalReference)
}
func (s *RoleMappingAdministration) RequestApproval(ctx context.Context, c AdminCaller, b UpdateRoleMappings) (ports.ApprovalView, error) {
	op, old, e := s.prepare(ctx, c, b)
	if e != nil {
		return ports.ApprovalView{}, e
	}
	if old.Revision != b.ExpectedRevision {
		return ports.ApprovalView{}, domain.NewError(domain.CodeConflict, "role mappings changed")
	}
	if !domain.RoleMappingExpansion(old.Mappings, b.Mappings) {
		return ports.ApprovalView{}, domain.NewError(domain.CodeInvalidArgument, "mapping edit does not expand administrative authority")
	}
	op.Action = "role_mappings.manage.approval"
	return s.Policy.RequestLocalApproval(ctx, op, domain.ApprovalEmergencyExpansion, "role_mappings", "role-mappings", domain.RoleMappingDigest(c.TenantID, s.Issuer, b.ExpectedRevision, b.Mappings), "role-mappings.expand", 10*time.Minute)
}
