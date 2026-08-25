package domain

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
)

// RootResourceGrant is the policy-resolved coefficient for one root-envelope
// dimension. The repository binds it to the tenant's authoritative dimension
// definition and copies that definition's unit and scale into the grant.
type RootResourceGrant struct {
	DimensionName string
	Coefficient   int64
}

var rootResourceConstraintDimensions = map[string]string{
	"max_budget_usd_microunits": "budget_usd_microunits",
	"max_llm_tokens":            "llm_tokens",
	"max_tool_calls":            "tool_calls",
	"max_tool_calls_per_minute": "tool_calls_per_minute",
	"max_active_children":       "active_children",
	"max_total_children":        "total_children",
	"max_delegation_depth":      "delegation_depth",
}

// RootResourceGrants converts the closed admission constraint vocabulary into
// a stable, deterministically ordered grant vector. Unknown non-resource
// constraints remain part of the policy snapshot but cannot silently become
// accounting authority.
func RootResourceGrants(constraints map[string]any) ([]RootResourceGrant, error) {
	grants := make([]RootResourceGrant, 0, len(rootResourceConstraintDimensions))
	for key, name := range rootResourceConstraintDimensions {
		value, exists := constraints[key]
		if !exists {
			continue
		}
		coefficient, err := exactNonnegativeInt64(value)
		if err != nil {
			return nil, fmt.Errorf("%w: %s must be an exact nonnegative integer", ErrInvalidResourceDimension, key)
		}
		grants = append(grants, RootResourceGrant{DimensionName: name, Coefficient: coefficient})
	}
	sort.Slice(grants, func(i, j int) bool { return grants[i].DimensionName < grants[j].DimensionName })
	return grants, nil
}

func exactNonnegativeInt64(value any) (int64, error) {
	var result int64
	switch typed := value.(type) {
	case int:
		result = int64(typed)
	case int64:
		result = typed
	case json.Number:
		parsed, err := typed.Int64()
		if err != nil {
			return 0, err
		}
		result = parsed
	case float64:
		if typed != math.Trunc(typed) || typed > math.MaxInt64 {
			return 0, ErrInvalidResourceDimension
		}
		result = int64(typed)
	default:
		return 0, ErrInvalidResourceDimension
	}
	if result < 0 {
		return 0, ErrInvalidResourceDimension
	}
	return result, nil
}
