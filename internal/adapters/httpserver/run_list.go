package httpserver

import (
	"crypto/sha256"
	"fmt"
	"github.com/bdobrica/ThinkPixelAG/internal/adapters/oidc"
	"github.com/bdobrica/ThinkPixelAG/internal/application"
	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/policy"
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
	"net/http"
	"strconv"
)

func RunListHandler(v oidc.Verifier, admin *application.PolicyAdministration, store ports.RunListRepository, query RunQueryService, codec *domain.CursorCodec) http.Handler {
	return AuthenticateBearer(v, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, e := administrationCaller(r)
		if e != nil {
			WriteError(w, r, e)
			return
		}
		c.Key = "read-run-list-key"
		if _, e = admin.Authorize(r.Context(), c, "runs.read", "runs", nil); e != nil {
			WriteError(w, r, e)
			return
		}
		limit := 50
		if raw := r.URL.Query().Get("limit"); raw != "" {
			limit, e = strconv.Atoi(raw)
			if e != nil || limit < 1 || limit > 100 {
				WriteError(w, r, domain.NewError(domain.CodeInvalidArgument, "invalid page limit"))
				return
			}
		}
		scope := fmt.Sprintf("runs:%x", sha256.Sum256([]byte(c.TenantID.String()+c.PrincipalID.String())))
		var after domain.ID
		if raw := r.URL.Query().Get("cursor"); raw != "" {
			cursor, err := codec.Decode(raw)
			if err != nil || cursor.SortKey != scope {
				WriteError(w, r, domain.NewError(domain.CodeInvalidArgument, "invalid page cursor"))
				return
			}
			after = cursor.ID
		}
		ids, e := store.ListRunIDs(r.Context(), after, limit+1)
		if e != nil {
			WriteError(w, r, e)
			return
		}
		more := len(ids) > limit
		if more {
			ids = ids[:limit]
		}
		items := []map[string]any{}
		for _, id := range ids {
			run, err := query.Get(r.Context(), application.GetRun{TenantID: c.TenantID, PrincipalID: c.PrincipalID, RequestID: c.RequestID, RunID: id, Roles: c.Roles, Issuer: c.Issuer, SecurityState: policy.SecurityState{Authoritative: true}})
			if domain.ErrorCodeOf(err) == domain.CodeNotFound {
				continue
			}
			if err != nil {
				WriteError(w, r, err)
				return
			}
			items = append(items, publicRun(run))
		}
		next := ""
		if more {
			next, e = codec.Encode(domain.PageCursor{SortKey: scope, ID: ids[len(ids)-1]})
			if e != nil {
				WriteError(w, r, e)
				return
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items, "next_cursor": next})
	}))
}
