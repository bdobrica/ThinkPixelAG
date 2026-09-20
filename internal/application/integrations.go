package application

import (
	"context"
	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
)

type IntegrationAdministration struct {
	Policy  *PolicyAdministration
	Store   ports.IntegrationStore
	Checker ports.IntegrationChecker
	Mode    string
	File    ports.OPAConnection
}
type UpdateIntegration struct {
	ExpectedRevision int64               `json:"expected_revision"`
	Connection       ports.OPAConnection `json:"connection"`
}
type IntegrationStatus struct {
	ID       string `json:"id"`
	Revision int64  `json:"revision"`
	State    string `json:"state"`
}

func (s *IntegrationAdministration) authorize(ctx context.Context, c AdminCaller, action string, b any) (ports.AdministrationOperation, error) {
	allowed := false
	for _, r := range c.Roles {
		allowed = allowed || r == "policy-admin"
	}
	if !allowed {
		return ports.AdministrationOperation{}, domain.NewError(domain.CodeForbidden, "policy-administrator required")
	}
	return s.Policy.Authorize(ctx, c, action, "opa", b)
}
func (s *IntegrationAdministration) Read(ctx context.Context, c AdminCaller) (ports.IntegrationSettings, error) {
	c.Key = "read-integrations"
	if _, e := s.authorize(ctx, c, "integrations.read", nil); e != nil {
		return ports.IntegrationSettings{}, e
	}
	if s.Mode == "file" {
		return ports.IntegrationSettings{ID: "opa", Mode: "file", Connection: s.File}, nil
	}
	return s.Store.OPAIntegration(ctx)
}
func (s *IntegrationAdministration) Update(ctx context.Context, c AdminCaller, b UpdateIntegration) (ports.IntegrationSettings, error) {
	op, e := s.authorize(ctx, c, "integrations.manage", b)
	if e != nil {
		return ports.IntegrationSettings{}, e
	}
	if s.Mode != "api" {
		return ports.IntegrationSettings{}, domain.NewError(domain.CodeForbidden, "OPA configuration is file-managed")
	}
	if b.ExpectedRevision < 1 || b.ExpectedRevision == 1<<63-1 {
		return ports.IntegrationSettings{}, domain.NewError(domain.CodeInvalidArgument, "invalid expected revision")
	}
	if e = s.Checker.Check(ctx, b.Connection); e != nil {
		return ports.IntegrationSettings{}, e
	}
	return s.Store.SaveOPAIntegration(ctx, op, b.ExpectedRevision, b.Connection)
}
func (s *IntegrationAdministration) Status(ctx context.Context, c AdminCaller) (IntegrationStatus, error) {
	m, e := s.Read(ctx, c)
	if e != nil {
		return IntegrationStatus{}, e
	}
	state := "ready"
	if s.Checker == nil || s.Checker.Check(ctx, m.Connection) != nil {
		state = "unavailable"
	}
	return IntegrationStatus{ID: "opa", Revision: m.Revision, State: state}, nil
}
