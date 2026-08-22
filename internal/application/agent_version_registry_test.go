package application

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
)

type versionRepositoryStub struct {
	version      domain.AgentVersion
	capabilities []domain.AgentCapability
	registered   bool
}

func (stub *versionRepositoryStub) RegisterAgentVersion(_ context.Context, version domain.AgentVersion, capabilities []domain.AgentCapability) error {
	stub.version, stub.capabilities, stub.registered = version, capabilities, true
	return nil
}
func (stub *versionRepositoryStub) DescribeAgentVersion(context.Context, domain.ID, string) (domain.AgentVersion, []domain.AgentCapability, error) {
	return stub.version, stub.capabilities, nil
}

func TestAgentVersionRegistryValidatesDigestAndRegistersCanonicalManifest(t *testing.T) {
	t.Parallel()
	image := "registry.example/agent@sha256:" + strings.Repeat("a", 64)
	manifest, _ := domain.NewAgentManifest(image, []string{"model.b", "model.a"}, []string{"tool.a"}, nil, nil, domain.AgentLimits{})
	digest, _ := manifest.ContentDigest()
	stub := &versionRepositoryStub{}
	service, err := NewAgentVersionRegistry(stub, fixedClock{now: testTime()})
	if err != nil {
		t.Fatal(err)
	}
	command := RegisterAgentVersion{ID: applicationID(t), TenantID: applicationID(t), AgentID: applicationID(t), CreatedBy: applicationID(t), ContentDigest: digest, Image: image, Models: []string{"model.b", "model.a"}, Tools: []string{"tool.a"}}
	version, err := service.Register(context.Background(), command)
	if err != nil || !stub.registered || len(stub.capabilities) != 3 || version.Manifest.Models[0] != "model.a" {
		t.Fatalf("Register = %+v, %v; capabilities=%+v", version, err, stub.capabilities)
	}
	command.ContentDigest = "sha256:" + strings.Repeat("0", 64)
	if _, err := service.Register(context.Background(), command); domain.ErrorCodeOf(err) != domain.CodeInvalidArgument {
		t.Fatalf("mismatched digest error = %v", err)
	}
}

func testTime() (value time.Time) { return time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC) }
