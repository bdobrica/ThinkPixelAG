// Package integrationconfig resolves the deployment-owned OPA destination and
// secret-reference boundary. Requests never supply a credential or file path.
package integrationconfig

import (
	"context"
	"errors"
	"github.com/bdobrica/ThinkPixelAG/internal/adapters/opa"
	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/policy"
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

type OPA struct {
	DefaultToken    string
	AllowedOrigins  []string
	SecretFiles     map[string]string
	Client          *http.Client
	Timeout, MaxTTL time.Duration
	Store           ports.PolicyAdministrationStore
	Verifier        ports.Verifier
	Channel         string
	Tenant          domain.ID
}

func origin(endpoint string) bool {
	u, e := url.Parse(endpoint)
	return e == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Hostname() != "" && u.User == nil && u.Path == "" && u.RawPath == "" && u.RawQuery == "" && !u.ForceQuery && u.Fragment == "" && u.Opaque == "" && !strings.ContainsAny(endpoint, "\r\n\t ")
}
func (s *OPA) Modules(c ports.OPAConnection) (*opa.Modules, error) {
	allowed := false
	for _, o := range s.AllowedOrigins {
		allowed = allowed || o == c.Endpoint
	}
	if !origin(c.Endpoint) || !allowed {
		return nil, domain.NewError(domain.CodeInvalidArgument, "OPA destination is not deployment-allowlisted")
	}
	token := s.DefaultToken
	if c.TokenReference != "" {
		path, ok := s.SecretFiles[c.TokenReference]
		if !ok {
			return nil, domain.NewError(domain.CodeInvalidArgument, "unknown protected token reference")
		}
		info, e := os.Lstat(path)
		if e != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
			return nil, domain.NewError(domain.CodeUnavailable, "protected token unavailable")
		}
		f, e := os.Open(path)
		if e != nil {
			return nil, domain.NewError(domain.CodeUnavailable, "protected token unavailable")
		}
		b, e := io.ReadAll(io.LimitReader(f, 8193))
		f.Close()
		if e != nil || len(b) == 0 || len(b) > 8192 {
			return nil, domain.NewError(domain.CodeUnavailable, "protected token unavailable")
		}
		token = strings.TrimSpace(string(b))
		if token == "" || strings.ContainsAny(token, "\r\n") {
			return nil, domain.NewError(domain.CodeUnavailable, "protected token unavailable")
		}
	}
	if s.Client == nil || s.Timeout <= 0 || s.Timeout > 5*time.Second {
		return nil, errors.New("bounded integration transport required")
	}
	// Copy client so injected/default redirect behavior cannot leak the token.
	client := *s.Client
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	client.Timeout = s.Timeout
	return &opa.Modules{Base: c.Endpoint, Token: token, Client: &client, Timeout: s.Timeout}, nil
}
func (s *OPA) Check(ctx context.Context, c ports.OPAConnection) error {
	m, e := s.Modules(c)
	if e != nil {
		return e
	}
	ctx, cancel := context.WithTimeout(ctx, s.Timeout)
	defer cancel()
	evaluator := &opa.ArtifactEvaluator{Store: s.Store, Modules: m, Verifier: s.Verifier, Channel: s.Channel, MaxTTL: s.MaxTTL}
	id, e := domain.NewID()
	if e != nil {
		return e
	}
	_, e = evaluator.Decide(ctx, policy.Input{ContractVersion: policy.ContractVersion, DecisionID: id.String(), RequestTime: time.Now().UTC(), Subject: policy.Subject{PrincipalID: id.String(), TenantID: s.Tenant.String(), PrincipalType: "human", Roles: []string{}}, Action: "integrations.read", Resource: policy.Resource{Type: "integration", ID: "opa", TenantID: s.Tenant.String(), Attributes: map[string]any{}}, RequestedConstraints: map[string]any{}, AuthorityConstraints: map[string]any{}, SecurityState: policy.SecurityState{Authoritative: true}, Context: policy.RequestContext{RequestID: id.String()}})
	if e != nil {
		return domain.NewError(domain.CodeUnavailable, "OPA active-artifact check failed")
	}
	return nil
}
