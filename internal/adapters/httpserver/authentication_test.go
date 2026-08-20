package httpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bdobrica/ThinkPixelAG/internal/adapters/oidc"
	"github.com/bdobrica/ThinkPixelAG/internal/domain"
)

type fakeVerifier struct {
	principal oidc.Principal
	err       error
	token     string
}

func (v *fakeVerifier) Verify(_ context.Context, token string) (oidc.Principal, error) {
	v.token = token
	return v.principal, v.err
}

func TestAuthenticateBearerMapsPrincipalAndRejectsHints(t *testing.T) {
	verifier := &fakeVerifier{principal: oidc.Principal{ID: "principal", TenantID: "tenant", Roles: []string{"role"}}}
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, ok := PrincipalFromContext(r.Context())
		if !ok || p.TenantID != "tenant" {
			t.Fatalf("principal = %#v, %v", p, ok)
		}
		w.WriteHeader(http.StatusNoContent)
	})
	handler := requestID(AuthenticateBearer(verifier, next), func() (string, error) { return fixedRequestID, nil })
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer token")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	if response.Code != http.StatusNoContent || verifier.token != "token" {
		t.Fatalf("status=%d token=%q", response.Code, verifier.token)
	}
	for _, header := range []string{"X-Tenant-ID", "X-Principal-ID", "X-Roles", "X-Forwarded-Tenant", "X-Forwarded-User", "X-Forwarded-Roles"} {
		req = httptest.NewRequest(http.MethodGet, "/protected", nil)
		req.Header.Set("Authorization", "Bearer token")
		req.Header.Set(header, "forged")
		response = httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		if response.Code != http.StatusUnauthorized {
			t.Errorf("%s status=%d", header, response.Code)
		}
	}
}

func TestAuthenticateBearerFailuresAndTenantBodyHint(t *testing.T) {
	tests := []struct {
		name, header string
		verifier     *fakeVerifier
		status       int
	}{
		{name: "missing", verifier: &fakeVerifier{}, status: http.StatusUnauthorized},
		{name: "basic", header: "Basic abc", verifier: &fakeVerifier{}, status: http.StatusUnauthorized},
		{name: "invalid", header: "Bearer bad", verifier: &fakeVerifier{err: domain.NewError(domain.CodeUnauthenticated, "invalid token")}, status: http.StatusUnauthorized},
		{name: "outage", header: "Bearer token", verifier: &fakeVerifier{err: domain.NewError(domain.CodeUnavailable, "identity provider keys are unavailable").WithRetryable()}, status: http.StatusServiceUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := requestID(AuthenticateBearer(tt.verifier, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("called") })), func() (string, error) { return fixedRequestID, nil })
			req := httptest.NewRequest(http.MethodGet, "/protected", nil)
			if tt.header != "" {
				req.Header.Set("Authorization", tt.header)
			}
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			if w.Code != tt.status {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			var problem Problem
			if err := json.Unmarshal(w.Body.Bytes(), &problem); err != nil || problem.RequestID != fixedRequestID {
				t.Fatalf("problem=%#v err=%v", problem, err)
			}
		})
	}
	ctx := context.WithValue(context.Background(), principalKey{}, oidc.Principal{TenantID: "tenant"})
	if err := RejectTenantHint(ctx, "other"); domain.ErrorCodeOf(err) != domain.CodeInvalidArgument {
		t.Fatalf("conflict=%v", err)
	}
	if err := RejectTenantHint(ctx, "tenant"); domain.ErrorCodeOf(err) != domain.CodeInvalidArgument {
		t.Fatalf("matching hint=%v", err)
	}
	if err := RejectTenantHint(ctx, ""); err != nil {
		t.Fatal(err)
	}
}
