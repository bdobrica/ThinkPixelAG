package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/bdobrica/ThinkPixelAG/internal/artifact"
	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/policy"
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
	"net/url"
	"regexp"
	"time"
)

// OperatorBootstrap is composed only by the protected local operator command.
// It is not a remotely callable authorization or recovery interface.
type OperatorBootstrap struct {
	Store     ports.BootstrapStore
	Signer    ports.Signer
	Verifier  ports.Verifier
	KeyID     string
	Modules   ports.PolicyModuleRuntime
	Evaluator func(ports.PolicyAdministrationStore) policy.Evaluator
	Clock     domain.Clock
}

func (s *OperatorBootstrap) Provision(ctx context.Context, b ports.BootstrapSpec, source []byte) (ports.BootstrapResult, error) {
	bad := func() (ports.BootstrapResult, error) {
		return ports.BootstrapResult{}, domain.NewError(domain.CodeInvalidArgument, "invalid reviewed bootstrap specification")
	}
	u, e := url.Parse(b.Issuer)
	if e != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || b.TenantID.IsZero() || b.AgentID.IsZero() || !regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`).MatchString(b.Slug) || len(b.Principals) < 2 || len(b.Principals) > 32 || !adminIdentifier.MatchString(b.Channel) {
		return bad()
	}
	seen := map[domain.ID]bool{}
	for _, id := range b.Principals {
		if id.IsZero() || seen[id] {
			return bad()
		}
		seen[id] = true
	}
	if e = domain.ValidateRoleMappings(b.Mappings); e != nil {
		return ports.BootstrapResult{}, e
	}
	roles := map[string]bool{}
	for _, role := range b.Mappings {
		roles[role] = true
	}
	if !roles["registry-admin"] || !roles["agent-invoker"] {
		return bad()
	}
	if e = b.Manifest.Validate(); e != nil {
		return bad()
	}
	if len(source) == 0 || len(source) > 1<<20 {
		return bad()
	}
	signing, digest, e := artifact.SigningDigest(artifact.PolicyBundle, policy.ContractVersion, 1, source)
	if e != nil {
		return bad()
	}
	sig, e := s.Signer.Sign(ctx, s.KeyID, signing)
	if e != nil {
		return ports.BootstrapResult{}, e
	}
	if e = policy.VerifyBundle(ctx, 1, policy.ContractVersion, digest, source, sig, s.Verifier, s.Modules); e != nil {
		return ports.BootstrapResult{}, domain.NewError(domain.CodeInvalidArgument, "initial policy signature or compilation invalid")
	}
	id, e := domain.NewID()
	if e != nil {
		return ports.BootstrapResult{}, e
	}
	now := s.Clock.Now().UTC().Truncate(time.Microsecond)
	a := ports.PolicyArtifact{ID: id, Digest: digest, ContractVersion: policy.ContractVersion, Revision: 1, Source: source, Signature: sig, Channel: b.Channel, State: "VALIDATED", CreatedAt: now}
	raw, e := json.Marshal(struct {
		Spec                      ports.BootstrapSpec
		Policy, KeyID, KeyVersion string
	}{b, digest, sig.KeyID, sig.KeyVersion})
	if e != nil {
		return bad()
	}
	sum := sha256.Sum256(raw)
	return s.Store.Provision(ctx, b, a, "sha256:"+hex.EncodeToString(sum[:]), now, func(repo ports.BootstrapRepository) error {
		registry, _ := NewAgentRegistry(repo, s.Clock)
		if _, e := registry.Create(ctx, CreateAgent{ID: b.AgentID, TenantID: b.TenantID, OwnerPrincipalID: b.Principals[0], SponsorPrincipalID: b.Principals[1], Name: b.AgentName, Description: "Operator-provisioned local evaluation agent", RiskClass: domain.AgentRiskLow}); e != nil {
			return e
		}
		versions, _ := NewAgentVersionRegistry(repo, s.Clock)
		versionID, e := domain.NewID()
		if e != nil {
			return e
		}
		vd, e := b.Manifest.ContentDigest()
		if e != nil {
			return e
		}
		if _, e = versions.Register(ctx, RegisterAgentVersion{ID: versionID, TenantID: b.TenantID, AgentID: b.AgentID, CreatedBy: b.Principals[0], ContentDigest: vd, Image: b.Manifest.Image, Models: b.Manifest.Models, Tools: b.Manifest.Tools, Skills: b.Manifest.Skills, Subagents: b.Manifest.Subagents, Limits: b.Manifest.Limits}); e != nil {
			return e
		}
		authorizer, e := NewPolicyAgentApprovalAuthorizer(s.Evaluator(repo), s.Clock.Now)
		if e != nil {
			return e
		}
		approvals, _ := NewAgentApprovalRegistry(repo, authorizer, s.Clock)
		approvalID, e := domain.NewID()
		if e != nil {
			return e
		}
		request, e := domain.NewID()
		if e != nil {
			return e
		}
		// Bootstrap authority is deployment-controlled; initial agent approval still
		// passes the actual signed policy and normal registry application service.
		_, e = approvals.Decide(ctx, DecideAgentVersion{ID: approvalID, TenantID: b.TenantID, AgentID: b.AgentID, ActorPrincipalID: b.Principals[0], RequestID: request, VersionDigest: vd, Decision: domain.DecisionApprove, ReasonCode: "registry.version.approved", ApprovalReference: "operator-bootstrap", Roles: []string{"registry-admin"}})
		return e
	})
}
