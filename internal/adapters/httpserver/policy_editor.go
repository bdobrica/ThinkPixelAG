package httpserver

import (
	"crypto/sha256"
	"encoding/hex"
	"github.com/bdobrica/ThinkPixelAG/internal/adapters/oidc"
	"github.com/bdobrica/ThinkPixelAG/internal/application"
	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
	"net/http"
	"strconv"
	"strings"
)

func PolicyEditorHandler(verifier oidc.Verifier, s *application.PolicyAdministration, codec *domain.CursorCodec) http.Handler {
	return AuthenticateBearer(verifier, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := administrationCaller(r)
		if err != nil {
			WriteError(w, r, err)
			return
		}
		store, ok := s.Store.(ports.PolicyEditorStore)
		if !ok {
			WriteError(w, r, domain.NewError(domain.CodeUnavailable, "policy editor unavailable"))
			return
		}
		var result any
		status := 200
		if r.Method != "GET" && len(r.Header.Values("Idempotency-Key")) != 1 {
			WriteError(w, r, domain.NewError(domain.CodeInvalidArgument, "one Idempotency-Key required"))
			return
		}
		draftID, approvalID := r.PathValue("draft_id"), r.PathValue("approval_id")
		switch {
		case approvalID != "":
			var id domain.ID
			id, err = domain.ParseID(approvalID)
			if err != nil {
				break
			}
			if r.Method == "GET" {
				result, err = s.Approval(r.Context(), c, id)
			} else {
				var b struct {
					Approved *bool `json:"approved"`
				}
				if err = DecodeJSON(r, &b); err == nil {
					if b.Approved == nil {
						err = domain.NewError(domain.CodeInvalidArgument, "approved decision required")
					} else {
						result, err = s.DecideApproval(r.Context(), c, id, *b.Approved)
						status = 201
					}
				}
			}
		case strings.HasSuffix(r.URL.Path, "/rollback-approvals"):
			var b application.RequestRollbackApproval
			if err = DecodeJSON(r, &b); err == nil {
				result, err = s.RequestRollback(r.Context(), c, r.PathValue("policy_digest"), b)
				status = 201
			}
		case draftID != "" || r.URL.Path == "/v1/admin/policy-drafts" && r.Method == "POST":
			var id domain.ID
			if draftID != "" {
				id, err = domain.ParseID(draftID)
				if err != nil {
					break
				}
			}
			switch {
			case r.Method == "GET":
				var revision int64
				if value := r.URL.Query().Get("revision"); value != "" {
					revision, err = strconv.ParseInt(value, 10, 64)
				}
				if err == nil {
					result, err = s.Draft(r.Context(), c, id, revision)
				}
			case strings.HasSuffix(r.URL.Path, "/validation"):
				var b struct {
					Revision int64 `json:"revision"`
				}
				if err = DecodeJSON(r, &b); err == nil {
					result, err = s.ValidateDraft(r.Context(), c, id, b.Revision)
				}
			case strings.HasSuffix(r.URL.Path, "/promotions"):
				var b application.PromotePolicyDraft
				if err = DecodeJSON(r, &b); err == nil {
					result, err = s.PromoteDraft(r.Context(), c, id, b)
					status = 201
				}
			default:
				var b application.SavePolicyDraft
				if err = DecodeJSON(r, &b); err == nil {
					result, err = s.SaveDraft(r.Context(), c, id, b)
					status = 201
				}
			}
		case r.PathValue("policy_digest") != "":
			if err = s.AuthorizeRead(r.Context(), c, r.PathValue("policy_digest")); err == nil {
				var b ports.PolicyArtifact
				b, err = s.Store.PolicyArtifact(r.Context(), s.Channel, r.PathValue("policy_digest"))
				if strings.HasSuffix(r.URL.Path, "/source") {
					result = map[string]any{"digest": b.Digest, "source": string(b.Source), "artifact_revision": b.Revision}
				} else {
					result = b
				}
			}
		case r.URL.Path == "/v1/admin/policy-activations/current":
			if err = s.AuthorizeRead(r.Context(), c, "current-policy"); err == nil {
				_, resultActivation, e := s.Store.CurrentPolicy(r.Context(), s.Channel)
				result, err = resultActivation, e
			}
		default:
			if err = s.AuthorizeRead(r.Context(), c, r.URL.Path); err != nil {
				break
			}
			limit := 50
			if v := r.URL.Query().Get("limit"); v != "" {
				limit, err = strconv.Atoi(v)
			}
			if err != nil || limit < 1 || limit > 100 {
				err = domain.NewError(domain.CodeInvalidArgument, "limit must be 1..100")
				break
			}
			sum := sha256.Sum256([]byte(c.TenantID.String() + c.PrincipalID.String() + r.URL.Path + s.Channel))
			scope := hex.EncodeToString(sum[:])
			var after domain.ID
			if token := r.URL.Query().Get("cursor"); token != "" {
				var cursor domain.PageCursor
				cursor, err = codec.Decode(token)
				if err != nil || cursor.SortKey != scope {
					err = domain.NewError(domain.CodeInvalidArgument, "invalid scoped cursor")
					break
				}
				after = cursor.ID
			}
			next := ""
			var items any
			switch r.URL.Path {
			case "/v1/admin/policies":
				var list []ports.PolicyArtifact
				list, err = store.ListPolicies(r.Context(), s.Channel, after, limit+1)
				if len(list) > limit {
					next, err = codec.Encode(domain.PageCursor{ID: list[limit-1].ID, SortKey: scope})
					list = list[:limit]
				}
				items = list
			case "/v1/admin/policy-drafts":
				var list []ports.PolicyDraft
				list, err = store.ListPolicyDrafts(r.Context(), after, limit+1)
				if len(list) > limit {
					next, err = codec.Encode(domain.PageCursor{ID: list[limit-1].ID, SortKey: scope})
					list = list[:limit]
				}
				items = list
			case "/v1/admin/policy-activations":
				var list []ports.PolicyActivation
				list, err = store.ListPolicyActivations(r.Context(), s.Channel, after, limit+1)
				if len(list) > limit {
					next, err = codec.Encode(domain.PageCursor{ID: list[limit-1].ID, SortKey: scope})
					list = list[:limit]
				}
				items = list
			default:
				err = domain.NewError(domain.CodeNotFound, "administration route not found")
			}
			result = map[string]any{"items": items, "next_cursor": next}
		}
		if err != nil {
			if _, ok := err.(*strconv.NumError); ok {
				err = domain.NewError(domain.CodeInvalidArgument, "invalid numeric parameter")
			}
			if err == domain.ErrInvalidID {
				err = domain.NewError(domain.CodeInvalidArgument, "invalid identifier")
			}
			WriteError(w, r, err)
			return
		}
		writeJSON(w, status, result)
	}))
}
