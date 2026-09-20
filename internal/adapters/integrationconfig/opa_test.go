package integrationconfig

import (
	"context"
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDestinationSecretAndRedirectBoundary(t *testing.T) {
	hit := 0
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hit++; w.WriteHeader(200) }))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 302) }))
	defer redirect.Close()
	s := OPA{AllowedOrigins: []string{redirect.URL}, Client: http.DefaultClient, Timeout: time.Second}
	for _, url := range []string{target.URL, redirect.URL + "/path", redirect.URL + "?q=x", "file:///etc/passwd", "http://user:pass@127.0.0.1"} {
		if _, e := s.Modules(ports.OPAConnection{Endpoint: url}); e == nil {
			t.Fatalf("accepted %s", url)
		}
	}
	if _, e := s.Modules(ports.OPAConnection{Endpoint: redirect.URL, TokenReference: "/etc/passwd"}); e == nil {
		t.Fatal("arbitrary path accepted")
	}
	path := filepath.Join(t.TempDir(), "token")
	if e := os.WriteFile(path, []byte("private-token"), 0600); e != nil {
		t.Fatal(e)
	}
	s.SecretFiles = map[string]string{"primary": path}
	m, e := s.Modules(ports.OPAConnection{Endpoint: redirect.URL, TokenReference: "primary"})
	if e != nil {
		t.Fatal(e)
	}
	if e = m.Validate(context.Background(), []byte("package thinkpixelag.authorization\nimport rego.v1\ndecision := {}\n")); e == nil {
		t.Fatal("redirect accepted")
	}
	if hit != 0 {
		t.Fatal("redirect leaked request")
	}
	if e = os.Chmod(path, 0644); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Modules(ports.OPAConnection{Endpoint: redirect.URL, TokenReference: "primary"}); e == nil {
		t.Fatal("public secret file accepted")
	}
}
