//go:build integration

package postgres

import (
	"context"
	"encoding/json"
	"github.com/bdobrica/ThinkPixelAG/internal/adapters/integrationconfig"
	"github.com/bdobrica/ThinkPixelAG/internal/application"
	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestIntegrationValidateBeforeCommitAndRetainLastValid(t *testing.T) {
	f := newAdministrationFixture(t)
	ctx := context.Background()
	connection := ports.OPAConnection{Endpoint: f.modules.Base}
	raw, _ := json.Marshal(connection)
	_, e := f.pool.Exec(ctx, `INSERT INTO integration_revisions(tenant_id,integration,revision,connection,created_by,created_at) VALUES($1,'opa',1,$2,$3,$4)`, f.tenant.String(), raw, f.actor.String(), time.Now().UTC())
	if e != nil {
		t.Fatal(e)
	}
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }))
	defer bad.Close()
	checker := &integrationconfig.OPA{AllowedOrigins: []string{f.modules.Base, bad.URL}, Client: http.DefaultClient, Timeout: time.Second, MaxTTL: time.Minute, Store: f.repo, Verifier: f.key, Channel: "stable", Tenant: f.tenant}
	s := &application.IntegrationAdministration{Policy: f.service, Store: f.repo, Mode: "api", Checker: checker}
	c := application.AdminCaller{TenantID: f.tenant, PrincipalID: f.actor, RequestID: mustNewRepositoryID(t), Roles: []string{"policy-admin"}, Key: "integration-update-key"}
	if _, e = s.Update(ctx, c, application.UpdateIntegration{ExpectedRevision: 1, Connection: ports.OPAConnection{Endpoint: bad.URL}}); e == nil {
		t.Fatal("unavailable integration committed")
	}
	old, e := f.repo.OPAIntegration(ctx)
	if e != nil || old.Revision != 1 || old.Connection != connection {
		t.Fatal(old, e)
	}
	c.Key = "integration-valid-key"
	m, e := s.Update(ctx, c, application.UpdateIntegration{ExpectedRevision: 1, Connection: connection})
	if e != nil || m.Revision != 2 {
		t.Fatal(m, e)
	}
	again, e := s.Update(ctx, c, application.UpdateIntegration{ExpectedRevision: 1, Connection: connection})
	if e != nil || again.Revision != 2 {
		t.Fatal("replay", again, e)
	}
	c.Key = "integration-stale-key"
	if _, e = s.Update(ctx, c, application.UpdateIntegration{ExpectedRevision: 1, Connection: connection}); domain.ErrorCodeOf(e) != domain.CodeConflict {
		t.Fatal("stale", e)
	}
	status, e := s.Status(ctx, c)
	if e != nil || status.State != "ready" {
		t.Fatal(status, e)
	}
	s.Mode = "file"
	if _, e = s.Update(ctx, c, application.UpdateIntegration{ExpectedRevision: 2, Connection: connection}); domain.ErrorCodeOf(e) != domain.CodeForbidden {
		t.Fatal(e)
	}
	c.Roles = []string{"registry-admin"}
	if _, e = s.Read(ctx, c); domain.ErrorCodeOf(e) != domain.CodeForbidden {
		t.Fatal(e)
	}
}
