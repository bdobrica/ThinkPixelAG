package httpserver

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/bdobrica/ThinkPixelAG/internal/adapters/oidc"
	"github.com/bdobrica/ThinkPixelAG/internal/application"
	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"net/http"
	"strings"
)

func HarnessGuidanceHandler(v oidc.Verifier, s *application.HarnessGuidance) http.Handler {
	return AuthenticateBearer(v, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "private, no-cache")
		w.Header().Set("Vary", "Authorization, Accept")
		c, e := administrationCaller(r)
		if e != nil {
			WriteError(w, r, e)
			return
		}
		version := r.URL.Query().Get("contract_version")
		if version == "" {
			version = application.HarnessContract
		}
		var run domain.ID
		if raw := r.URL.Query().Get("run_id"); raw != "" {
			run, e = domain.ParseID(raw)
			if e != nil {
				WriteError(w, r, domain.NewError(domain.CodeInvalidArgument, "invalid Run context"))
				return
			}
		}
		for key, values := range r.URL.Query() {
			if len(values) != 1 || (key != "run_id" && key != "contract_version") {
				WriteError(w, r, domain.NewError(domain.CodeInvalidArgument, "unsupported guidance query"))
				return
			}
		}
		d, e := s.Get(r.Context(), application.GetHarnessGuidance{Caller: c, RunID: run, Version: version})
		if e != nil {
			WriteError(w, r, e)
			return
		}
		raw, e := json.Marshal(d)
		if e != nil || len(raw) > 64<<10 {
			WriteError(w, r, domain.NewError(domain.CodeUnavailable, "guidance exceeds response bounds"))
			return
		}
		markdown := strings.HasSuffix(r.URL.Path, "/instructions")
		body := raw
		if markdown {
			body = []byte(d.Markdown)
		}
		etag := fmt.Sprintf(`"%x"`, sha256.Sum256(body))
		w.Header().Set("ETag", etag)
		w.Header().Set("X-AG-Capability-Revision", d.Revision)
		w.Header().Set("X-AG-Guidance-Expires", d.ExpiresAt.Format("2006-01-02T15:04:05Z"))
		// No early cache hit: identity, effective state, policy and Run checks above
		// also apply to every conditional request.
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		if markdown {
			w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		} else {
			w.Header().Set("Content-Type", "application/json")
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}))
}
