package mtls

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net/url"
	"testing"
)

func TestVerifierRequiresVerifiedExactBoundURISAN(t *testing.T) {
	uri, _ := url.Parse("spiffe://prod.example/ns/governance/sa/meter")
	leaf := &x509.Certificate{Raw: []byte("leaf"), URIs: []*url.URL{uri}}
	verifier, err := New([]Binding{{URISAN: uri.String(), PrincipalID: "018f0000-0000-7000-8000-000000000001", TenantID: "018f0000-0000-7000-8000-000000000002", Roles: []string{"trusted-meter"}}})
	if err != nil {
		t.Fatal(err)
	}
	principal, err := verifier.VerifyWorkload(context.Background(), &tls.ConnectionState{PeerCertificates: []*x509.Certificate{leaf}, VerifiedChains: [][]*x509.Certificate{{leaf}}})
	if err != nil || principal.Issuer != Issuer || principal.Roles[0] != "trusted-meter" {
		t.Fatalf("principal=%+v err=%v", principal, err)
	}
	for name, state := range map[string]*tls.ConnectionState{"nil": nil, "unverified": {PeerCertificates: []*x509.Certificate{leaf}}, "missing peer": {VerifiedChains: [][]*x509.Certificate{{leaf}}}} {
		t.Run(name, func(t *testing.T) {
			if _, err := verifier.VerifyWorkload(context.Background(), state); err == nil {
				t.Fatal("accepted invalid transport identity")
			}
		})
	}
}

func TestVerifierRejectsAmbiguousAndOverprivilegedBindings(t *testing.T) {
	if _, err := New([]Binding{{URISAN: "spiffe://example/workload", PrincipalID: "p", TenantID: "t", Roles: []string{"policy-admin"}}}); err == nil {
		t.Fatal("accepted administrative role")
	}
	if _, err := New([]Binding{{URISAN: "spiffe://example/workload", PrincipalID: "p", TenantID: "t", Roles: []string{"trusted-meter"}}, {URISAN: "spiffe://example/workload", PrincipalID: "p2", TenantID: "t", Roles: []string{"trusted-meter"}}}); err == nil {
		t.Fatal("accepted duplicate identity")
	}
}
