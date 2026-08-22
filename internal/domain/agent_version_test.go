package domain

import (
	"strings"
	"testing"
	"time"
)

func TestAgentManifestCanonicalDigestAndCapabilities(t *testing.T) {
	t.Parallel()
	limit := int64(10)
	image := "registry.example/agent@sha256:" + strings.Repeat("a", 64)
	manifest, err := NewAgentManifest(image, []string{"model.b", "model.a"}, []string{"tool.a"}, []string{"sha256:" + strings.Repeat("b", 64)}, nil, AgentLimits{MaxToolCalls: &limit})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(manifest.Models, ","); got != "model.a,model.b" || manifest.Subagents == nil {
		t.Fatalf("normalized manifest = %+v", manifest)
	}
	digest, err := manifest.ContentDigest()
	if err != nil || len(digest) != 71 || !strings.HasPrefix(digest, "sha256:") {
		t.Fatalf("ContentDigest = %q, %v", digest, err)
	}
	canonical, _ := manifest.CanonicalJSON()
	parsed, err := ParseAgentManifest(canonical)
	if err != nil {
		t.Fatal(err)
	}
	parsedDigest, _ := parsed.ContentDigest()
	if parsedDigest != digest {
		t.Fatalf("parsed digest = %q, want %q", parsedDigest, digest)
	}
	version := AgentVersion{ID: mustID(t), TenantID: mustID(t), AgentID: mustID(t), CreatedBy: mustID(t), ContentDigest: digest, ImageDigest: "sha256:" + strings.Repeat("a", 64), Manifest: manifest, CreatedAt: time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)}
	if err := version.Validate(); err != nil {
		t.Fatal(err)
	}
	ids := []ID{mustID(t), mustID(t), mustID(t), mustID(t)}
	capabilities, err := version.Capabilities(ids)
	if err != nil || len(capabilities) != 4 || capabilities[0].Type != CapabilityModel || capabilities[3].Type != CapabilitySkill {
		t.Fatalf("Capabilities = %+v, %v", capabilities, err)
	}
}

func TestAgentManifestRejectsMalformedAndNonCanonicalInputs(t *testing.T) {
	t.Parallel()
	validImage := "registry.example/agent@sha256:" + strings.Repeat("a", 64)
	negative, zero, active, total := int64(-1), int64(0), int64(2), int64(1)
	tests := []struct {
		name string
		fn   func() error
	}{
		{"floating image tag", func() error {
			_, err := NewAgentManifest("registry.example/agent:latest", nil, nil, nil, nil, AgentLimits{})
			return err
		}},
		{"uppercase digest", func() error {
			_, err := NewAgentManifest("registry.example/agent@sha256:"+strings.Repeat("A", 64), nil, nil, nil, nil, AgentLimits{})
			return err
		}},
		{"duplicate declaration", func() error {
			return (AgentManifest{SchemaVersion: 1, Image: validImage, Models: []string{"same", "same"}, Tools: []string{}, Skills: []string{}, Subagents: []string{}}).Validate()
		}},
		{"negative limit", func() error {
			_, err := NewAgentManifest(validImage, nil, nil, nil, nil, AgentLimits{MaxToolCalls: &negative})
			return err
		}},
		{"zero execution time", func() error {
			_, err := NewAgentManifest(validImage, nil, nil, nil, nil, AgentLimits{MaxExecutionTimeSeconds: &zero})
			return err
		}},
		{"children inconsistent", func() error {
			_, err := NewAgentManifest(validImage, nil, nil, nil, nil, AgentLimits{MaxActiveChildren: &active, MaxTotalChildren: &total})
			return err
		}},
		{"unknown JSON", func() error {
			_, err := ParseAgentManifest([]byte(`{"schema_version":1,"image":"` + validImage + `","models":[],"tools":[],"skills":[],"subagents":[],"limits":{},"unknown":true}`))
			return err
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.fn(); err == nil {
				t.Fatal("invalid manifest accepted")
			}
		})
	}
}
