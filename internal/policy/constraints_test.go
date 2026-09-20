package policy

import (
	"encoding/json"
	"math"
	"reflect"
	"testing"
	"time"
)

func TestResolveConstraintsInheritsAndNarrowsWithoutMutation(t *testing.T) {
	authority := map[string]any{"max_llm_tokens": json.Number("9007199254740993"), "max_execution_time_seconds": 300, "nested": map[string]any{"tools": []any{"read", "write"}, "count": 10}}
	requested := map[string]any{"max_execution_time_seconds": 1000, "nested": map[string]any{"tools": []any{"read", "other"}}}
	got, err := ResolveConstraints(authority, requested, map[string]any{"max_execution_time_seconds": 120})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"max_llm_tokens": json.Number("9007199254740993"), "max_execution_time_seconds": 120, "nested": map[string]any{"tools": []any{"read"}, "count": 10}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("resolved=%v want=%v", got, want)
	}
	got["nested"].(map[string]any)["count"] = 1
	if authority["nested"].(map[string]any)["count"] != 10 || len(authority["nested"].(map[string]any)["tools"].([]any)) != 2 {
		t.Fatal("resolution mutated authority")
	}
}

func TestResolveConstraintsRejectsInvalidOrExpandedPolicy(t *testing.T) {
	for _, tc := range []struct {
		name                       string
		authority, request, output map[string]any
	}{
		{"unknown request", map[string]any{"tokens": 100}, map[string]any{"tools": 1}, map[string]any{}},
		{"unknown output", map[string]any{"tokens": 100}, nil, map[string]any{"tools": 1}},
		{"expanded authority", map[string]any{"tokens": 100}, nil, map[string]any{"tokens": 101}},
		{"expanded request", map[string]any{"tokens": 100}, map[string]any{"tokens": 10}, map[string]any{"tokens": 11}},
		{"negative", map[string]any{"tokens": 100}, nil, map[string]any{"tokens": -1}},
		{"wrong type", map[string]any{"tokens": 100}, nil, map[string]any{"tokens": "10"}},
		{"nonfinite", map[string]any{"tokens": math.Inf(1)}, nil, map[string]any{}},
		{"null output", map[string]any{"tokens": 100}, nil, nil},
		{"null authority", map[string]any{"tokens": nil}, nil, map[string]any{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ResolveConstraints(tc.authority, tc.request, tc.output); err == nil {
				t.Fatal("accepted invalid constraints")
			}
		})
	}
}

func TestDecisionAllowsInheritedLimitsWithoutCallerFields(t *testing.T) {
	in := validInput()
	in.AuthorityConstraints = map[string]any{"max_llm_tokens": 100, "max_tool_calls": 10}
	in.RequestedConstraints = map[string]any{"max_tool_calls": 3}
	d := allowDecision()
	d.DecisionID = in.DecisionID
	d.ResolvedConstraints = map[string]any{"max_llm_tokens": 100, "max_tool_calls": 3}
	if err := ValidateDecision(d, in, 30*time.Second); err != nil {
		t.Fatal(err)
	}
	// Partial policy output is legal but cannot erase caller or authority bounds.
	d.ResolvedConstraints = map[string]any{}
	got, err := ResolveConstraints(in.AuthorityConstraints, in.RequestedConstraints, d.ResolvedConstraints)
	if err != nil || got["max_tool_calls"] != 3 || got["max_llm_tokens"] != 100 {
		t.Fatalf("resolved=%v error=%v", got, err)
	}
}
