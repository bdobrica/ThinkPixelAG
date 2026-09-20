package httpserver

import (
	"github.com/bdobrica/ThinkPixelAG/internal/adapters/oidc"
	"github.com/bdobrica/ThinkPixelAG/internal/application"
	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"net/http"
	"strings"
)

func IntegrationsHandler(v oidc.Verifier, s *application.IntegrationAdministration) http.Handler {
	return AuthenticateBearer(v, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, e := administrationCaller(r)
		if e != nil {
			WriteError(w, r, e)
			return
		}
		var out any
		status := http.StatusOK
		if r.Method == http.MethodPut {
			if len(r.Header.Values("Idempotency-Key")) != 1 {
				WriteError(w, r, domain.NewError(domain.CodeInvalidArgument, "one Idempotency-Key required"))
				return
			}
			var b application.UpdateIntegration
			if e = DecodeJSON(r, &b); e == nil {
				out, e = s.Update(r.Context(), c, b)
			}
			status = http.StatusCreated
		} else if strings.HasSuffix(r.URL.Path, "/status") {
			out, e = s.Status(r.Context(), c)
		} else {
			out, e = s.Read(r.Context(), c)
		}
		if e != nil {
			WriteError(w, r, e)
			return
		}
		writeJSON(w, status, out)
	}))
}
