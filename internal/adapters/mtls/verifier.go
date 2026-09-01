// Package mtls maps verified client certificates to closed workload identities.
package mtls

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"sort"
	"strings"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/identity"
)

const Issuer = "mtls"

// Binding is deployment-owned authorization metadata for one exact URI SAN.
// Certificate attributes never carry tenant or role authority themselves.
type Binding struct {
	URISAN, PrincipalID, TenantID string
	Roles                         []string
}

// Verifier is an immutable, replaceable workload identity mapper.
type Verifier struct{ bindings map[string]identity.Principal }

func New(bindings []Binding) (*Verifier, error) {
	if len(bindings) == 0 {
		return nil, fmt.Errorf("at least one workload identity binding is required")
	}
	result := &Verifier{bindings: make(map[string]identity.Principal, len(bindings))}
	for _, binding := range bindings {
		if !validCanonical(binding.URISAN, 2048) || !strings.Contains(binding.URISAN, "://") || !validCanonical(binding.PrincipalID, 256) || !validCanonical(binding.TenantID, 256) || len(binding.Roles) == 0 {
			return nil, fmt.Errorf("workload identity binding is invalid")
		}
		if _, exists := result.bindings[binding.URISAN]; exists {
			return nil, fmt.Errorf("duplicate workload URI SAN")
		}
		roles := append([]string(nil), binding.Roles...)
		sort.Strings(roles)
		for index, role := range roles {
			if !allowedRole(role) || (index > 0 && role == roles[index-1]) {
				return nil, fmt.Errorf("workload role binding is invalid")
			}
		}
		result.bindings[binding.URISAN] = identity.Principal{ID: binding.PrincipalID, TenantID: binding.TenantID, Issuer: Issuer, Roles: roles}
	}
	return result, nil
}

func (v *Verifier) VerifyWorkload(_ context.Context, state *tls.ConnectionState) (identity.Principal, error) {
	if state == nil || len(state.VerifiedChains) == 0 || len(state.PeerCertificates) == 0 {
		return identity.Principal{}, domain.NewError(domain.CodeUnauthenticated, "a verified client certificate is required")
	}
	leaf := state.PeerCertificates[0]
	if !chainContainsLeaf(state.VerifiedChains, leaf) || len(leaf.URIs) != 1 {
		return identity.Principal{}, domain.NewError(domain.CodeUnauthenticated, "client certificate identity is invalid")
	}
	principal, found := v.bindings[leaf.URIs[0].String()]
	if !found {
		return identity.Principal{}, domain.NewError(domain.CodeUnauthenticated, "client certificate identity is not trusted")
	}
	return principal, nil
}

func chainContainsLeaf(chains [][]*x509.Certificate, leaf *x509.Certificate) bool {
	for _, chain := range chains {
		if len(chain) > 0 && chain[0].Equal(leaf) {
			return true
		}
	}
	return false
}

func allowedRole(role string) bool {
	switch role {
	case "trusted-workload", "trusted-meter", "trusted-settler", "trusted-gateway":
		return true
	default:
		return false
	}
}

func validCanonical(value string, maximum int) bool {
	return value != "" && len(value) <= maximum && strings.TrimSpace(value) == value && !strings.ContainsAny(value, "\r\n\x00")
}
