package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/bdobrica/ThinkPixelAG/internal/adapters/postgres"
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
	"strings"
)

type harnessState struct {
	runtime *runtimeRoutes
	repo    *postgres.TenantRepository
}

func (s *harnessState) HarnessSnapshot(ctx context.Context) (ports.HarnessSnapshot, error) {
	r := s.runtime
	_, p, e := s.repo.CurrentPolicy(ctx, r.runtime.PolicyChannel)
	if e != nil {
		return ports.HarnessSnapshot{}, e
	}
	snap := ports.HarnessSnapshot{PolicyDigest: p.Digest, PolicyVersion: p.Version, RunList: r.localKey != nil}
	var integration any = r.settings.OPA.URL
	if r.runtime.IntegrationsMode == "api" {
		m, e := s.repo.OPAIntegration(ctx)
		if e != nil {
			return snap, e
		}
		integration = m
	}
	if r.runtime.RoleMappingsMode == "api" {
		m, e := s.repo.RoleMappings(ctx, strings.TrimSuffix(r.settings.OIDC.IssuerURL, "/"))
		if e != nil {
			return snap, e
		}
		snap.MappingRevision = m.Revision
	}
	raw, e := json.Marshal(struct {
		Runtime     runtimeSettings
		Integration any
	}{r.runtime, integration})
	if e != nil {
		return snap, e
	}
	m := hmac.New(sha256.New, []byte(r.settings.CursorKey.Value()))
	m.Write(raw)
	snap.ConfigurationRevision = hex.EncodeToString(m.Sum(nil))
	return snap, nil
}
