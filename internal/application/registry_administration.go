package application

import (
	"context"
	"encoding/json"
	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
)

type RegistryAdministration struct {
	Policy *PolicyAdministration
	Store  ports.RegistryAdministrationStore
}

func (s *RegistryAdministration) Create(ctx context.Context, c AdminCaller, b CreateAgent) (domain.Agent, error) {
	b.TenantID = c.TenantID
	op, e := s.Policy.Authorize(ctx, c, "agents.manage", b.ID.String(), b)
	if e != nil {
		return domain.Agent{}, e
	}
	raw, e := s.Store.RegistryTransaction(ctx, op, func(repo ports.RegistryRepository) (any, error) {
		svc, _ := NewAgentRegistry(repo, s.Policy.Clock)
		return svc.Create(ctx, b)
	})
	var a domain.Agent
	if e == nil {
		e = json.Unmarshal(raw, &a)
	}
	return a, e
}
func (s *RegistryAdministration) Register(ctx context.Context, c AdminCaller, b RegisterAgentVersion) (domain.AgentVersion, error) {
	b.TenantID = c.TenantID
	b.CreatedBy = c.PrincipalID
	// Generated storage identity is excluded from the caller's replay digest.
	b.ID = domain.ID{}
	payload := struct {
		AgentID  string
		Digest   string
		Manifest domain.AgentManifest
	}{b.AgentID.String(), b.ContentDigest, domain.AgentManifest{SchemaVersion: 1, Image: b.Image, Models: b.Models, Tools: b.Tools, Skills: b.Skills, Subagents: b.Subagents, Limits: b.Limits}}
	op, e := s.Policy.Authorize(ctx, c, "agents.manage", b.AgentID.String()+":"+b.ContentDigest, payload)
	if e != nil {
		return domain.AgentVersion{}, e
	}
	raw, e := s.Store.RegistryTransaction(ctx, op, func(repo ports.RegistryRepository) (any, error) {
		id, e := domain.NewID()
		if e != nil {
			return nil, e
		}
		b.ID = id
		svc, _ := NewAgentVersionRegistry(repo, s.Policy.Clock)
		return svc.Register(ctx, b)
	})
	var v domain.AgentVersion
	if e == nil {
		e = json.Unmarshal(raw, &v)
	}
	return v, e
}
