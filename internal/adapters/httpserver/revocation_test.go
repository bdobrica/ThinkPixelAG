package httpserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/adapters/oidc"
	"github.com/bdobrica/ThinkPixelAG/internal/application"
	"github.com/bdobrica/ThinkPixelAG/internal/domain"
)

type revocationAppStub struct {
	command application.ChangeRevocation
	lift    bool
}

func (a *revocationAppStub) Create(_ context.Context, c application.ChangeRevocation) (domain.RevocationResult, error) {
	a.command = c
	id, _ := domain.NewID()
	v := domain.Revocation{ID: id, Scope: c.Scope, Target: c.Target, ReasonCode: c.ReasonCode, EffectiveAt: c.EffectiveAt, CreatedAt: c.EffectiveAt}
	return domain.RevocationResult{RevocationID: id, Revocation: v, State: domain.RevocationCreated, Sequence: 1, Epochs: domain.EpochVector{Security: 1}}, nil
}
func (a *revocationAppStub) Lift(_ context.Context, c application.ChangeRevocation) (domain.RevocationResult, error) {
	a.command = c
	a.lift = true
	return domain.RevocationResult{RevocationID: c.RevocationID, State: domain.RevocationLifted, Epochs: domain.EpochVector{Security: 2}}, nil
}

func TestRevocationHandlerUsesAuthenticatedAuthority(t *testing.T) {
	tenant, _ := domain.NewID()
	actor, _ := domain.NewID()
	app := &revocationAppStub{}
	handler, err := RevocationHandler(&fakeVerifier{principal: oidc.Principal{ID: actor.String(), TenantID: tenant.String(), Roles: []string{"revocation-admin"}, Issuer: "https://issuer.test"}}, app, RevocationHTTPConfig{})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.Handle("POST /v1/admin/revocations", requestID(handler, func() (string, error) { return fixedRequestID, nil }))
	mux.Handle("POST /v1/admin/revocations/{revocation_id}/lift", requestID(handler, func() (string, error) { return fixedRequestID, nil }))
	now := time.Unix(200, 0).UTC().Format(time.RFC3339)
	req := httptest.NewRequest(http.MethodPost, "/v1/admin/revocations", strings.NewReader(`{"scope":"global","target":"all","reason_code":"security.emergency","effective_at":"`+now+`"}`))
	req.Header.Set("Authorization", "Bearer token")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusCreated || app.command.TenantID != tenant || app.command.PrincipalID != actor || app.command.Scope != domain.RevocationGlobal {
		t.Fatalf("status=%d command=%+v body=%s", w.Code, app.command, w.Body.String())
	}
	rev, _ := domain.NewID()
	req = httptest.NewRequest(http.MethodPost, "/v1/admin/revocations/"+rev.String()+"/lift", strings.NewReader(`{"reason_code":"security.restored","approval_reference":"approval:42"}`))
	req.Header.Set("Authorization", "Bearer token")
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK || !app.lift || app.command.RevocationID != rev {
		t.Fatalf("status=%d command=%+v body=%s", w.Code, app.command, w.Body.String())
	}
}
