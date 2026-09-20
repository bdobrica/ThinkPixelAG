package httpserver

import (
	"github.com/bdobrica/ThinkPixelAG/internal/adapters/oidc"
	"github.com/bdobrica/ThinkPixelAG/internal/application"
	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"net/http"
	"strings"
)

func RegistryAdministrationHandler(v oidc.Verifier, s *application.RegistryAdministration) http.Handler {
	return AuthenticateBearer(v, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, e := administrationCaller(r)
		if e != nil {
			WriteError(w, r, e)
			return
		}
		if len(r.Header.Values("Idempotency-Key")) != 1 {
			WriteError(w, r, domain.NewError(domain.CodeInvalidArgument, "one Idempotency-Key required"))
			return
		}
		var out any
		if agent := r.PathValue("agent_id"); agent != "" {
			var b struct {
				Digest    string              `json:"digest"`
				Image     string              `json:"image"`
				Models    []string            `json:"models"`
				Tools     []string            `json:"tools"`
				Skills    []string            `json:"skills"`
				Subagents []string            `json:"subagents"`
				Limits    *domain.AgentLimits `json:"limits"`
			}
			id, err := domain.ParseID(agent)
			if err != nil {
				WriteError(w, r, domain.NewError(domain.CodeInvalidArgument, "invalid agent identifier"))
				return
			}
			if e = DecodeJSON(r, &b); e == nil {
				if b.Models == nil || b.Tools == nil || b.Skills == nil || b.Subagents == nil || b.Limits == nil {
					WriteError(w, r, domain.NewError(domain.CodeInvalidArgument, "complete manifest required"))
					return
				}
				var version domain.AgentVersion
				version, e = s.Register(r.Context(), c, application.RegisterAgentVersion{AgentID: id, ContentDigest: b.Digest, Image: b.Image, Models: b.Models, Tools: b.Tools, Skills: b.Skills, Subagents: b.Subagents, Limits: *b.Limits})
				if e == nil {
					out = map[string]any{"agent_id": version.AgentID, "digest": version.ContentDigest, "image": version.Manifest.Image, "state": "REGISTERED", "models": version.Manifest.Models, "tools": version.Manifest.Tools, "skills": version.Manifest.Skills, "subagents": version.Manifest.Subagents, "limits": version.Manifest.Limits, "created_at": version.CreatedAt}
				}
			}
		} else {
			var b struct {
				ID          domain.ID `json:"id"`
				Name        string    `json:"name"`
				Owner       domain.ID `json:"owner"`
				Sponsor     domain.ID `json:"sponsor"`
				Risk        string    `json:"risk_class"`
				Description string    `json:"description"`
			}
			if e = DecodeJSON(r, &b); e == nil {
				var agent domain.Agent
				agent, e = s.Create(r.Context(), c, application.CreateAgent{ID: b.ID, Name: b.Name, OwnerPrincipalID: b.Owner, SponsorPrincipalID: b.Sponsor, RiskClass: domain.AgentRiskClass(strings.ToUpper(b.Risk)), Description: b.Description})
				if e == nil {
					out = publicAgent(agent)
				}
			}
		}
		if e != nil {
			WriteError(w, r, e)
			return
		}
		writeJSON(w, http.StatusCreated, out)
	}))
}
