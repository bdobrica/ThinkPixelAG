package application

import (
	"context"
	"errors"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
)

type AgentVersionRegistry struct {
	repository ports.AgentVersionRegistry
	clock      domain.Clock
}

func NewAgentVersionRegistry(repository ports.AgentVersionRegistry, clock domain.Clock) (*AgentVersionRegistry, error) {
	if repository == nil || clock == nil {
		return nil, errors.New("agent version registry requires a repository and clock")
	}
	return &AgentVersionRegistry{repository: repository, clock: clock}, nil
}

type RegisterAgentVersion struct {
	ID, TenantID, AgentID, CreatedBy domain.ID
	ContentDigest                    string
	Image                            string
	Models, Tools, Skills, Subagents []string
	Limits                           domain.AgentLimits
}

func (service *AgentVersionRegistry) Register(ctx context.Context, command RegisterAgentVersion) (domain.AgentVersion, error) {
	manifest, err := domain.NewAgentManifest(command.Image, command.Models, command.Tools, command.Skills, command.Subagents, command.Limits)
	if err != nil {
		return domain.AgentVersion{}, domain.WrapError(domain.CodeInvalidArgument, "agent version manifest is invalid", err)
	}
	contentDigest, err := manifest.ContentDigest()
	if err != nil || contentDigest != command.ContentDigest {
		return domain.AgentVersion{}, domain.WrapError(domain.CodeInvalidArgument, "agent version digest does not match its canonical manifest", err)
	}
	imageDigest, err := domain.ImageDigest(manifest.Image)
	if err != nil {
		return domain.AgentVersion{}, domain.WrapError(domain.CodeInvalidArgument, "agent image is invalid", err)
	}
	now, err := domain.RequireUTC(service.clock.Now())
	if err != nil {
		return domain.AgentVersion{}, domain.WrapError(domain.CodeInternal, "agent version registry clock is invalid", err)
	}
	version := domain.AgentVersion{ID: command.ID, TenantID: command.TenantID, AgentID: command.AgentID, CreatedBy: command.CreatedBy, ContentDigest: contentDigest, ImageDigest: imageDigest, Manifest: manifest, CreatedAt: now}
	if err := version.Validate(); err != nil {
		return domain.AgentVersion{}, domain.WrapError(domain.CodeInvalidArgument, "agent version is invalid", err)
	}
	capabilityIDs := make([]domain.ID, len(manifest.Models)+len(manifest.Tools)+len(manifest.Skills)+len(manifest.Subagents))
	for index := range capabilityIDs {
		capabilityIDs[index], err = domain.NewID()
		if err != nil {
			return domain.AgentVersion{}, domain.WrapError(domain.CodeInternal, "could not generate capability identifier", err)
		}
	}
	capabilities, err := version.Capabilities(capabilityIDs)
	if err != nil {
		return domain.AgentVersion{}, domain.WrapError(domain.CodeInternal, "could not normalize agent capabilities", err)
	}
	if err := service.repository.RegisterAgentVersion(ctx, version, capabilities); err != nil {
		return domain.AgentVersion{}, err
	}
	return version, nil
}

func (service *AgentVersionRegistry) Describe(ctx context.Context, tenantID, agentID domain.ID, digest string) (domain.AgentVersion, []domain.AgentCapability, error) {
	if tenantID.IsZero() || agentID.IsZero() || len(digest) != 71 {
		return domain.AgentVersion{}, nil, domain.NewError(domain.CodeInvalidArgument, "tenant, agent, and version digest are required")
	}
	version, capabilities, err := service.repository.DescribeAgentVersion(ctx, agentID, digest)
	if err != nil {
		return domain.AgentVersion{}, nil, err
	}
	if version.TenantID != tenantID || version.AgentID != agentID {
		return domain.AgentVersion{}, nil, domain.NewError(domain.CodeNotFound, "agent version not found")
	}
	return version, capabilities, nil
}
