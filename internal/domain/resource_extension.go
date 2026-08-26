package domain

import (
	"errors"
	"regexp"
	"sort"
	"time"
)

var ErrInvalidResourceExtension = errors.New("invalid resource extension")

var extensionReferencePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,255}$`)
var extensionReasonPattern = regexp.MustCompile(`^[a-z][a-z0-9._-]{0,127}$`)

type ResourceExtensionAmount struct {
	Name     string
	Quantity Quantity
}

type ResourceExtension struct {
	ID, TenantID, RunID, ActorPrincipalID, PolicyDecisionID ID
	IdempotencyKey, ReasonCode, ApprovalReference           string
	Additions                                               []ResourceExtensionAmount
	DeadlineExtensionSeconds                                int64
	CreatedAt                                               time.Time
}

func ValidateResourceExtension(candidate ResourceExtension) (ResourceExtension, error) {
	if candidate.ID.IsZero() || candidate.TenantID.IsZero() || candidate.RunID.IsZero() || candidate.ActorPrincipalID.IsZero() || candidate.PolicyDecisionID.IsZero() ||
		!extensionReferencePattern.MatchString(candidate.IdempotencyKey) || !extensionReasonPattern.MatchString(candidate.ReasonCode) || !extensionReferencePattern.MatchString(candidate.ApprovalReference) ||
		(len(candidate.Additions) == 0 && candidate.DeadlineExtensionSeconds == 0) || candidate.DeadlineExtensionSeconds < 0 || candidate.DeadlineExtensionSeconds > 31536000 {
		return ResourceExtension{}, ErrInvalidResourceExtension
	}
	created, err := RequireUTC(candidate.CreatedAt)
	if err != nil || created.IsZero() || len(candidate.Additions) > 100 {
		return ResourceExtension{}, ErrInvalidResourceExtension
	}
	candidate.CreatedAt = created
	seen := make(map[string]struct{}, len(candidate.Additions))
	for _, addition := range candidate.Additions {
		if !resourceDimensionNamePattern.MatchString(addition.Name) || addition.Quantity.Amount().Coefficient() <= 0 {
			return ResourceExtension{}, ErrInvalidResourceExtension
		}
		if _, exists := seen[addition.Name]; exists {
			return ResourceExtension{}, ErrInvalidResourceExtension
		}
		seen[addition.Name] = struct{}{}
	}
	sort.Slice(candidate.Additions, func(i, j int) bool { return candidate.Additions[i].Name < candidate.Additions[j].Name })
	return candidate, nil
}

type ResourceExtensionResult struct {
	ID              ID
	EnvelopeVersion int64
	DeadlineAt      *time.Time
	Resumed         bool
}
