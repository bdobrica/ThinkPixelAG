package domain

import (
	"errors"
	"strings"
	"time"
)

type VersionResolutionMode string

const (
	ResolutionAutomatic VersionResolutionMode = "AUTOMATIC"
	ResolutionPinned    VersionResolutionMode = "PINNED"
	ResolutionRollback  VersionResolutionMode = "ROLLBACK"
)

type AgentVersionCandidate struct {
	Agent    Agent
	Version  AgentVersion
	Approval AgentVersionApproval
	State    AgentVersionState
}

type RunVersionResolution struct {
	RunID, TenantID, AgentID, AgentVersionID, ApprovalID ID
	AgentContentDigest, PolicyBundleDigest               string
	PolicyActivationVersion                              int64
	Mode                                                 VersionResolutionMode
	InvocationDecisionID, SelectionDecisionID            ID
	ResolvedConstraints                                  map[string]any
	ResolvedAt                                           time.Time
}

func (resolution RunVersionResolution) Validate() error {
	if resolution.RunID.IsZero() || resolution.TenantID.IsZero() || resolution.AgentID.IsZero() || resolution.AgentVersionID.IsZero() || resolution.ApprovalID.IsZero() || resolution.InvocationDecisionID.IsZero() {
		return errors.New("version resolution identifiers must be set")
	}
	if !ValidDigest(resolution.AgentContentDigest) || !ValidDigest(resolution.PolicyBundleDigest) || resolution.PolicyActivationVersion < 1 {
		return errors.New("version resolution policy or content metadata is invalid")
	}
	if resolution.Mode != ResolutionAutomatic && resolution.Mode != ResolutionPinned && resolution.Mode != ResolutionRollback {
		return errors.New("version resolution mode is invalid")
	}
	if resolution.Mode == ResolutionAutomatic && !resolution.SelectionDecisionID.IsZero() || resolution.Mode != ResolutionAutomatic && resolution.SelectionDecisionID.IsZero() {
		return errors.New("controlled version selection requires separate policy evidence")
	}
	if resolution.ResolvedConstraints == nil {
		return errors.New("version resolution constraints must be present")
	}
	if _, err := RequireUTC(resolution.ResolvedAt); err != nil || resolution.ResolvedAt.IsZero() {
		return errors.New("version resolution time must be a non-zero UTC timestamp")
	}
	return nil
}

func ValidDigest(value string) bool {
	if len(value) != 71 || !strings.HasPrefix(value, "sha256:") {
		return false
	}
	for _, character := range value[7:] {
		if character < '0' || character > '9' && character < 'a' || character > 'f' {
			return false
		}
	}
	return true
}
