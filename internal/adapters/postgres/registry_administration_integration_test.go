//go:build integration

package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRegistryAdministrationHTTPReplayAndIsolation(t *testing.T) {
	f := newAdministrationFixture(t)
	sponsor := mustNewRepositoryID(t)
	if _, e := f.pool.Exec(context.Background(), `INSERT INTO principals(id,tenant_id,external_issuer,external_subject,principal_type,created_at)VALUES($1,$2,'https://admin.test',$3,'HUMAN',$4)`, sponsor.String(), f.tenant.String(), sponsor.String(), time.Now().UTC()); e != nil {
		t.Fatal(e)
	}
	handler := administrationHTTP(t, f)
	call := func(path, key, token string, b any) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(b)
		r := httptest.NewRequest("POST", path, bytes.NewReader(raw))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("Idempotency-Key", key)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	require := func(w *httptest.ResponseRecorder, n int) {
		t.Helper()
		if w.Code != n {
			t.Fatalf("status=%d want=%d %s", w.Code, n, w.Body)
		}
	}
	id := mustNewRepositoryID(t)
	b := map[string]any{"id": id.String(), "name": "operator-created", "owner": f.actor.String(), "sponsor": sponsor.String(), "risk_class": "low"}
	require(call("/v1/admin/agents", "agent-create-denied-key", "invoker", b), 403)
	a := call("/v1/admin/agents", "agent-create-test-key", "admin", b)
	require(a, 201)
	again := call("/v1/admin/agents", "agent-create-test-key", "admin", b)
	require(again, 201)
	if a.Body.String() != again.Body.String() {
		t.Fatal("agent replay differs")
	}
	manifest, e := domain.NewAgentManifest("registry.example/test@sha256:"+strings.Repeat("a", 64), nil, nil, nil, nil, domain.AgentLimits{})
	if e != nil {
		t.Fatal(e)
	}
	digest, _ := manifest.ContentDigest()
	version := map[string]any{"digest": digest, "image": manifest.Image, "models": []string{}, "tools": []string{}, "skills": []string{}, "subagents": []string{}, "limits": map[string]any{}}
	path := "/v1/admin/agents/" + id.String() + "/versions"
	v := call(path, "version-register-key", "admin", version)
	require(v, 201)
	vr := call(path, "version-register-key", "admin", version)
	require(vr, 201)
	if v.Body.String() != vr.Body.String() {
		t.Fatal("version replay differs")
	}
	foreign := mustNewRepositoryID(t)
	require(call("/v1/admin/agents/"+foreign.String()+"/versions", "version-foreign-key", "admin", version), 404)

}
