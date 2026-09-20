package opa

import (
	"context"
	"encoding/json"
	"github.com/bdobrica/ThinkPixelAG/internal/policy"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestModulesRejectMismatchAndRecoverMissingArtifact(t *testing.T) {
	source := []byte("package thinkpixelag.authorization\nimport rego.v1\ndecision := {}\n")
	digest, _ := policy.Digest(source)
	compiled, _ := moduleSource(digest, source)
	missing := true
	wrong := false
	puts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "PUT" {
			puts++
			missing = false
			w.Write([]byte(`{}`))
			return
		}
		if missing {
			w.WriteHeader(404)
			return
		}
		raw := string(compiled)
		if wrong {
			raw = "different"
		}
		json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"raw": raw}})
	}))
	defer server.Close()
	m := &Modules{Base: server.URL, Client: server.Client(), Timeout: time.Second}
	if err := m.Ensure(context.Background(), digest, source); err != nil {
		t.Fatal(err)
	}
	if err := m.Ensure(context.Background(), digest, source); err != nil || puts != 1 {
		t.Fatalf("immutable reuse failed %v %d", err, puts)
	}
	wrong = true
	if m.Ensure(context.Background(), digest, source) == nil || puts != 1 {
		t.Fatal("overwrote mismatched module")
	}
	if _, err := moduleSource(digest, []byte("package evil")); err == nil {
		t.Fatal("accepted digest/package substitution")
	}
}
