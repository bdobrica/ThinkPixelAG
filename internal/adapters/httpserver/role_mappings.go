package httpserver

import (
	"github.com/bdobrica/ThinkPixelAG/internal/adapters/oidc"
	"github.com/bdobrica/ThinkPixelAG/internal/application"
	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"net/http"
	"strings"
)

func RoleMappingsHandler(verifier oidc.Verifier, s *application.RoleMappingAdministration) http.Handler {
	return AuthenticateBearer(verifier, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := administrationCaller(r)
		if err != nil {
			WriteError(w, r, err)
			return
		}
		var result any
		status := http.StatusOK
		if r.Method == http.MethodGet {
			result, err = s.Read(r.Context(), c)
		} else {
			if len(r.Header.Values("Idempotency-Key")) != 1 {
				WriteError(w, r, domain.NewError(domain.CodeInvalidArgument, "one Idempotency-Key required"))
				return
			}
			var b application.UpdateRoleMappings
			if err = DecodeJSON(r, &b); err == nil {
				if strings.HasSuffix(r.URL.Path, "/approvals") {
					result, err = s.RequestApproval(r.Context(), c, b)
				} else {
					result, err = s.Update(r.Context(), c, b)
				}
			}
			status = http.StatusCreated
		}
		if err != nil {
			WriteError(w, r, err)
			return
		}
		writeJSON(w, status, result)
	}))
}
