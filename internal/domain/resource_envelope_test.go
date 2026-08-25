package domain

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestRootResourceGrantsAreExactAndDeterministic(t *testing.T) {
	t.Parallel()
	grants, err := RootResourceGrants(map[string]any{
		"max_tool_calls": json.Number("7"), "max_llm_tokens": float64(100),
		"max_execution_time_seconds": float64(60),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(grants) != 2 || grants[0] != (RootResourceGrant{DimensionName: "llm_tokens", Coefficient: 100}) || grants[1] != (RootResourceGrant{DimensionName: "tool_calls", Coefficient: 7}) {
		t.Fatalf("grants=%+v", grants)
	}
}

func TestRootResourceGrantsRejectInexactOrNegativeAuthority(t *testing.T) {
	t.Parallel()
	for _, value := range []any{float64(1.5), int64(-1), json.Number("1.1"), "10"} {
		if _, err := RootResourceGrants(map[string]any{"max_llm_tokens": value}); !errors.Is(err, ErrInvalidResourceDimension) {
			t.Fatalf("value=%v error=%v", value, err)
		}
	}
}
