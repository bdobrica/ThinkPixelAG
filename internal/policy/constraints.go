package policy

import "errors"

// IntersectConstraints retains every authoritative dimension. Restrictions may
// narrow existing dimensions but cannot create authority for a new dimension.
// It returns a fresh map and never mutates caller or cached policy state.
func IntersectConstraints(authority, restrictions map[string]any) (map[string]any, error) {
	result := make(map[string]any, len(authority))
	for key, ceiling := range authority {
		value, err := intersectConstraint(ceiling, ceiling)
		if err != nil {
			return nil, err
		}
		result[key] = value
	}
	for key, restriction := range restrictions {
		ceiling, ok := authority[key]
		if !ok {
			return nil, errors.New("constraint has no authoritative ceiling")
		}
		value, err := intersectConstraint(ceiling, restriction)
		if err != nil {
			return nil, err
		}
		result[key] = value
	}
	return result, nil
}

// ResolveConstraints fills omissions from trusted ceilings and optional caller
// restrictions. An explicit policy expansion is rejected, not silently clamped.
func ResolveConstraints(authority, requested, resolved map[string]any) (map[string]any, error) {
	effective, err := IntersectConstraints(authority, requested)
	if err != nil {
		return nil, err
	}
	if resolved == nil || !constraintsNarrow(resolved, effective) {
		return nil, errors.New("policy decision expands effective constraints")
	}
	return IntersectConstraints(effective, resolved)
}

func intersectConstraint(ceiling, restriction any) (any, error) {
	if limit, ok := constraintNumber(ceiling); ok {
		value, valid := constraintNumber(restriction)
		if !valid || limit.Sign() < 0 || value.Sign() < 0 {
			return nil, errors.New("invalid numeric constraint")
		}
		if value.Cmp(limit) < 0 {
			return restriction, nil
		}
		return ceiling, nil
	}
	switch limit := ceiling.(type) {
	case string:
		if value, ok := restriction.(string); ok && value == limit {
			return limit, nil
		}
	case []any:
		values, ok := restriction.([]any)
		if !ok {
			break
		}
		allowed := make(map[string]bool, len(limit))
		for _, entry := range limit {
			value, ok := entry.(string)
			if !ok {
				return nil, errors.New("invalid constraint allowlist")
			}
			allowed[value] = true
		}
		intersection := []any{}
		for _, entry := range values {
			value, ok := entry.(string)
			if !ok {
				return nil, errors.New("invalid constraint allowlist")
			}
			if allowed[value] {
				intersection = append(intersection, value)
			}
		}
		return intersection, nil
	case map[string]any:
		if values, ok := restriction.(map[string]any); ok {
			return IntersectConstraints(limit, values)
		}
	}
	return nil, errors.New("incompatible constraint shape")
}
