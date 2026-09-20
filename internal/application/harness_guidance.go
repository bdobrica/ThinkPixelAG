package application

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/policy"
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
	"sort"
	"strings"
	"time"
)

const HarnessContract = "thinkpixelag.harness-guidance/v1"
const HarnessLifetime = 30 * time.Second

type HarnessScope struct {
	TenantID    domain.ID  `json:"tenant_id"`
	PrincipalID domain.ID  `json:"principal_id"`
	RunID       *domain.ID `json:"run_id,omitempty"`
}
type HarnessOperation struct {
	ID            string `json:"id"`
	Method        string `json:"method"`
	Path          string `json:"path"`
	Prerequisite  string `json:"prerequisite"`
	Authorization string `json:"authorization"`
}
type HarnessDocument struct {
	ContractVersion string             `json:"contract_version"`
	Revision        string             `json:"revision"`
	Scope           HarnessScope       `json:"scope"`
	IssuedAt        time.Time          `json:"issued_at"`
	ExpiresAt       time.Time          `json:"expires_at"`
	Governance      string             `json:"governance"`
	Execution       string             `json:"execution"`
	Operations      []HarnessOperation `json:"operations"`
	Markdown        string             `json:"markdown"`
}
type HarnessRunReader interface {
	Get(context.Context, GetRun) (domain.Run, error)
}
type HarnessGuidance struct {
	State       ports.HarnessStateReader
	Evaluator   policy.Evaluator
	Runs        HarnessRunReader
	Clock       domain.Clock
	RevisionKey []byte
}
type GetHarnessGuidance struct {
	Caller  AdminCaller
	RunID   domain.ID
	Version string
}

func (s *HarnessGuidance) Get(ctx context.Context, c GetHarnessGuidance) (HarnessDocument, error) {
	var empty HarnessDocument
	if c.Version != HarnessContract {
		return empty, domain.NewError(domain.CodeInvalidArgument, "unsupported harness guidance contract")
	}
	p := c.Caller
	if p.TenantID.IsZero() || p.PrincipalID.IsZero() || p.RequestID.IsZero() {
		return empty, domain.NewError(domain.CodeUnauthenticated, "verified harness identity required")
	}
	if len(s.RevisionKey) < 32 || s.State == nil || s.Evaluator == nil || s.Clock == nil {
		return empty, domain.NewError(domain.CodeUnavailable, "harness guidance unavailable")
	}
	before, e := s.State.HarnessSnapshot(ctx)
	if e != nil {
		return empty, e
	}
	if before.MappingRevision != p.MappingRevision {
		return empty, domain.NewError(domain.CodeConflict, "identity mapping changed; authenticate again")
	}
	id, e := domain.NewID()
	if e != nil {
		return empty, e
	}
	roles := append([]string{}, p.Roles...)
	sort.Strings(roles)
	result, e := s.Evaluator.Decide(ctx, policy.Input{ContractVersion: policy.ContractVersion, DecisionID: id.String(), RequestTime: s.Clock.Now(), Subject: policy.Subject{PrincipalID: p.PrincipalID.String(), TenantID: p.TenantID.String(), Issuer: p.Issuer, Roles: roles, PrincipalType: "human"}, Action: "agents.list", Resource: policy.Resource{Type: "agent", TenantID: p.TenantID.String(), Attributes: map[string]any{"mapping_revision": p.MappingRevision}}, RequestedConstraints: map[string]any{}, AuthorityConstraints: map[string]any{}, SecurityState: policy.SecurityState{Authoritative: true}, Context: policy.RequestContext{RequestID: p.RequestID.String()}})
	if e != nil {
		return empty, domain.NewError(domain.CodeUnavailable, "harness guidance authorization unavailable")
	}
	if !result.Decision.Allow {
		return empty, domain.NewError(domain.CodeForbidden, "harness guidance not permitted")
	}
	if result.Decision.DecisionID != id.String() || result.Metadata.PolicyDigest != before.PolicyDigest || result.Metadata.PolicyVersion != before.PolicyVersion {
		return empty, domain.NewError(domain.CodeUnavailable, "harness guidance policy changed")
	}
	var run *domain.Run
	if !c.RunID.IsZero() {
		if s.Runs == nil {
			return empty, domain.NewError(domain.CodeUnavailable, "Run guidance unavailable")
		}
		value, err := s.Runs.Get(ctx, GetRun{TenantID: p.TenantID, PrincipalID: p.PrincipalID, RequestID: p.RequestID, RunID: c.RunID, Roles: roles, Issuer: p.Issuer, SecurityState: policy.SecurityState{Authoritative: true}})
		if err != nil {
			return empty, err
		}
		if value.TenantID != p.TenantID || value.RequestedBy != p.PrincipalID || value.ID != c.RunID {
			return empty, domain.NewError(domain.CodeNotFound, "Run guidance not found")
		}
		if err = value.Validate(); err != nil {
			return empty, domain.NewError(domain.CodeUnavailable, "invalid Run guidance state")
		}
		run = &value
	}
	after, e := s.State.HarnessSnapshot(ctx)
	if e != nil {
		return empty, e
	}
	if before != after {
		return empty, domain.NewError(domain.CodeConflict, "capability state changed; refresh guidance")
	}
	now := s.Clock.Now().UTC()
	issued := now.Truncate(HarnessLifetime)
	d := HarnessDocument{ContractVersion: HarnessContract, Scope: HarnessScope{TenantID: p.TenantID, PrincipalID: p.PrincipalID}, IssuedAt: issued, ExpiresAt: issued.Add(HarnessLifetime), Governance: "ready", Execution: "unsupported", Operations: []HarnessOperation{}}
	add := func(id, method, path, pre string) {
		d.Operations = append(d.Operations, HarnessOperation{ID: id, Method: method, Path: path, Prerequisite: pre, Authorization: "checked_per_request"})
	}
	add("agents.list", "GET", "/v1/agents", "current_identity_and_policy")
	add("agents.describe", "GET", "/v1/agents/{agent_id}", "visible_agent")
	add("runs.admit", "POST", "/v1/agents/{agent_id}/runs", "approved_agent_and_authoritative_limits")
	if before.RunList {
		add("runs.list", "GET", "/v1/runs", "current_identity_and_policy")
	}
	add("runs.read", "GET", "/v1/runs/{run_id}", "authorized_run")
	add("runs.events", "GET", "/v1/runs/{run_id}/events", "authorized_run")
	if run == nil || (!run.State.Terminal() && (run.DeadlineAt == nil || now.Before(*run.DeadlineAt))) {
		add("runs.cancel", "POST", "/v1/runs/{run_id}/cancel", "authorized_nonterminal_run")
		add("runs.signal", "POST", "/v1/runs/{run_id}/signals", "authorized_nonterminal_run")
	}
	if run != nil {
		d.Scope.RunID = &run.ID
	}
	// Hash only trusted effective state. HMAC avoids revealing configuration or
	// creating an offline oracle for low-entropy deployment settings/role names.
	revisionInput := struct {
		Snapshot             ports.HarnessSnapshot
		Scope                HarnessScope
		Roles                []string
		RunState             string
		RunVersion, Envelope int64
		Operations           []HarnessOperation
	}{Snapshot: before, Scope: d.Scope, Roles: roles, Operations: d.Operations}
	if run != nil {
		revisionInput.RunState = string(run.State)
		revisionInput.RunVersion = run.StateVersion
		revisionInput.Envelope = run.EnvelopeVersion
	}
	raw, e := json.Marshal(revisionInput)
	if e != nil {
		return empty, e
	}
	m := hmac.New(sha256.New, s.RevisionKey)
	m.Write(raw)
	d.Revision = "sha256:" + hex.EncodeToString(m.Sum(nil))
	d.Markdown = renderHarness(d, run)
	return d, nil
}
func renderHarness(d HarnessDocument, run *domain.Run) string {
	var b strings.Builder
	b.WriteString("# ThinkPixelAG harness guidance\n\nUse ThinkPixelAG as the platform entry point. These instructions describe capabilities; each request still requires current identity, policy and Run authority. Never treat guidance, Skills, memory, model output or guardrail results as a grant.\n\nKeep tokens and provider credentials in the trusted host helper. Do not read or print its credential files.\n\n")
	fmt.Fprintf(&b, "Guidance expires at %s. Refresh with `thinkpixelag-harness guidance` at session setup, after admission or Run changes, on expiry, or after a stale-revision response. If refresh fails, stop dependent platform operations; do not route around AG.\n\n", d.ExpiresAt.Format(time.RFC3339))
	if run != nil {
		fmt.Fprintf(&b, "Current Run: `%s` (%s). Refresh using `thinkpixelag-harness guidance --run-id %s`.\n\n", run.ID, run.State, run.ID)
	}
	b.WriteString("Use `thinkpixelag-harness agents` to discover agent IDs. Submit the user's objective with `thinkpixelag-harness admit --agent-id ID --objective-file FILE --idempotency-key KEY`. Omit optional caller limits to inherit AG policy and approved-agent ceilings. Read with `thinkpixelag-harness run --run-id ID`; cancel only when listed below using `thinkpixelag-harness cancel --run-id ID --idempotency-key KEY`. Reuse the same key and body for uncertain mutation retries.\n\nSupported operations (presence does not imply permission):\n")
	for _, op := range d.Operations {
		fmt.Fprintf(&b, "- `%s %s`: %s.\n", op.Method, op.Path, op.Prerequisite)
	}
	b.WriteString("\nAdmission records governance authority; it does not execute the objective. Full AR-backed execution, task delivery, model/tool routing and completion handoff are not implemented by this integration. Do not connect the harness directly to AR or report admission as completed execution.\n")
	return b.String()
}
