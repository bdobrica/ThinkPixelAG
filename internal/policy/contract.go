// Package policy defines the fail-closed authorization contract shared by the
// application, OPA adapter, and policy lifecycle.
package policy

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

const ContractVersion = "thinkpixelag.authorization/v1alpha1"

type Subject struct {
	PrincipalID           string   `json:"principal_id"`
	TenantID              string   `json:"tenant_id"`
	PrincipalType         string   `json:"principal_type"`
	Roles                 []string `json:"roles"`
	AuthenticationMethods []string `json:"authentication_methods"`
	Issuer                string   `json:"issuer"`
}
type Resource struct {
	Type       string         `json:"type"`
	ID         string         `json:"id"`
	TenantID   string         `json:"tenant_id"`
	Attributes map[string]any `json:"attributes"`
}
type Agent struct {
	ID            string `json:"id"`
	VersionDigest string `json:"version_digest"`
	RiskClass     string `json:"risk_class"`
	Owner         string `json:"owner"`
	Approved      bool   `json:"approved"`
	Revoked       bool   `json:"revoked,omitempty"`
}
type SecurityState struct {
	GlobalEpoch           int64 `json:"global_epoch"`
	TenantPolicyEpoch     int64 `json:"tenant_policy_epoch"`
	TenantRevocationEpoch int64 `json:"tenant_revocation_epoch"`
	AgentRevocationEpoch  int64 `json:"agent_revocation_epoch"`
	AgeSeconds            int64 `json:"age_seconds"`
	HasGap                bool  `json:"has_gap"`
	Authoritative         bool  `json:"authoritative"`
}
type RequestContext struct {
	RequestID      string `json:"request_id"`
	SourceWorkload string `json:"source_workload,omitempty"`
}

type Input struct {
	ContractVersion      string         `json:"contract_version"`
	DecisionID           string         `json:"decision_id"`
	RequestTime          time.Time      `json:"request_time"`
	Subject              Subject        `json:"subject"`
	Action               string         `json:"action"`
	Resource             Resource       `json:"resource"`
	Agent                *Agent         `json:"agent,omitempty"`
	RequestedConstraints map[string]any `json:"requested_constraints"`
	AuthorityConstraints map[string]any `json:"authority_constraints"`
	SecurityState        SecurityState  `json:"security_state"`
	Context              RequestContext `json:"context"`
}
type Obligation struct {
	Type      string `json:"type"`
	Mandatory bool   `json:"mandatory"`
}
type Decision struct {
	ContractVersion     string         `json:"contract_version"`
	DecisionID          string         `json:"decision_id"`
	Allow               bool           `json:"allow"`
	ReasonCodes         []string       `json:"reason_codes"`
	ResolvedConstraints map[string]any `json:"resolved_constraints"`
	Obligations         []Obligation   `json:"obligations"`
	DecisionTTLSeconds  int64          `json:"decision_ttl_seconds"`
}
type Metadata struct {
	PolicyDigest  string
	PolicyVersion int64
	InputDigest   string
	Duration      time.Duration
	CacheStatus   string
}
type Result struct {
	Decision Decision
	Metadata Metadata
}

var reasonCodes = map[string]bool{
	"agent.discover.allowed": true, "agent.invoke.allowed": true, "run.access.allowed": true, "workload.operation.allowed": true, "governance.operation.allowed": true,
	"identity.invalid": true, "tenant.mismatch": true, "resource.not_visible": true, "agent.not_approved": true, "agent.revoked": true, "principal.revoked": true, "policy.revoked": true,
	"security_state.stale": true, "security_state.gap": true, "constraint.expands_authority": true, "approval.required": true, "action.not_permitted": true, "contract.invalid": true,
}

func (in Input) Validate() error {
	if in.ContractVersion != ContractVersion || in.DecisionID == "" || in.RequestTime.IsZero() || in.Subject.PrincipalID == "" || in.Subject.TenantID == "" || in.Action == "" || in.Resource.Type == "" || in.Resource.TenantID == "" || in.Context.RequestID == "" {
		return errors.New("invalid policy input contract")
	}
	if in.RequestedConstraints == nil || in.AuthorityConstraints == nil || in.Resource.Attributes == nil {
		return errors.New("policy input maps must be present")
	}
	if in.SecurityState.GlobalEpoch < 0 || in.SecurityState.TenantPolicyEpoch < 0 || in.SecurityState.TenantRevocationEpoch < 0 || in.SecurityState.AgentRevocationEpoch < 0 || in.SecurityState.AgeSeconds < 0 {
		return errors.New("policy security state cannot be negative")
	}
	if len(in.Subject.Roles) > 32 || len(in.Action) > 128 || len(in.Resource.ID) > 512 {
		return errors.New("policy input exceeds bounds")
	}
	return boundedJSON(in, 64<<10)
}

func ValidateDecision(d Decision, in Input, maxTTL time.Duration) error {
	if d.ContractVersion != ContractVersion || d.DecisionID != in.DecisionID || len(d.ReasonCodes) == 0 || len(d.ReasonCodes) > 16 || d.ResolvedConstraints == nil || d.Obligations == nil || d.DecisionTTLSeconds < 0 {
		return errors.New("invalid policy decision contract")
	}
	for _, c := range d.ReasonCodes {
		if !reasonCodes[c] {
			return fmt.Errorf("unregistered policy reason code")
		}
	}
	for _, o := range d.Obligations {
		if o.Type == "" || o.Mandatory {
			return errors.New("unknown mandatory policy obligation")
		}
	}
	if time.Duration(d.DecisionTTLSeconds)*time.Second > maxTTL {
		return errors.New("policy decision TTL exceeds configured maximum")
	}
	if !constraintsNarrow(d.ResolvedConstraints, in.AuthorityConstraints) {
		return errors.New("policy decision expands authority constraints")
	}
	if !constraintsNarrow(d.ResolvedConstraints, in.RequestedConstraints) {
		return errors.New("policy decision expands requested constraints")
	}
	return boundedJSON(d, 64<<10)
}

// constraintsNarrow supports the contract's JSON primitives: numeric maxima,
// string allowlists, and nested objects. Unknown shapes fail closed.
func constraintsNarrow(got, ceiling map[string]any) bool {
	for key, value := range got {
		limit, ok := ceiling[key]
		if !ok {
			return false
		}
		switch v := value.(type) {
		case float64:
			l, ok := limit.(float64)
			if !ok || v > l {
				return false
			}
		case string:
			if v != limit {
				return false
			}
		case []any:
			allowed, ok := limit.([]any)
			if !ok || !subset(v, allowed) {
				return false
			}
		case map[string]any:
			l, ok := limit.(map[string]any)
			if !ok || !constraintsNarrow(v, l) {
				return false
			}
		default:
			if fmt.Sprint(v) != fmt.Sprint(limit) {
				return false
			}
		}
	}
	return true
}
func subset(values, allowed []any) bool {
	set := map[string]bool{}
	for _, v := range allowed {
		set[fmt.Sprint(v)] = true
	}
	for _, v := range values {
		if !set[fmt.Sprint(v)] {
			return false
		}
	}
	return true
}
func boundedJSON(v any, limit int) error {
	b, e := json.Marshal(v)
	if e != nil {
		return e
	}
	if len(b) > limit {
		return errors.New("policy contract exceeds size bound")
	}
	return nil
}
func NormalizeStrings(v []string) []string {
	out := append([]string(nil), v...)
	sort.Strings(out)
	out = compact(out)
	return out
}

// AuthorizationDigest excludes evidence-only request fields while retaining
// every identity, action, resource, constraint, and security-state field that
// may affect authorization. Equivalent role/authentication sets hash equally.
func AuthorizationDigest(in Input) (string, error) {
	if err := in.Validate(); err != nil {
		return "", err
	}
	in.DecisionID = ""
	in.RequestTime = time.Time{}
	in.Context.RequestID = ""
	in.Subject.Roles = NormalizeStrings(in.Subject.Roles)
	in.Subject.AuthenticationMethods = NormalizeStrings(in.Subject.AuthenticationMethods)
	b, err := json.Marshal(in)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(b)
	return fmt.Sprintf("sha256:%x", digest), nil
}
func compact(v []string) []string {
	n := 0
	for _, x := range v {
		if strings.TrimSpace(x) == "" || n > 0 && v[n-1] == x {
			continue
		}
		v[n] = x
		n++
	}
	return v[:n]
}
