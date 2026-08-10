package repository_test

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestCIExposesRequiredGates(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../.github/workflows/ci.yaml")
	if err != nil {
		t.Fatal(err)
	}
	workflow := string(data)
	for _, job := range []string{"quality", "unit", "race", "policy", "integration", "security", "build", "image"} {
		if !regexp.MustCompile(`(?m)^  ` + regexp.QuoteMeta(job) + `:$`).MatchString(workflow) {
			t.Errorf("CI job %q is missing", job)
		}
	}
	for _, target := range []string{"fmt-check", "generate-check", "lint", "test", "test-race", "test-policy", "compose-check", "test-integration", "dependency-check", "vulnerability-check", "license-check", "build", "image"} {
		if !strings.Contains(workflow, "make "+target) {
			t.Errorf("CI does not invoke make target %q", target)
		}
	}
}

func TestCIPinsActionsAndLimitsAuthority(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../.github/workflows/ci.yaml")
	if err != nil {
		t.Fatal(err)
	}
	workflow := string(data)
	if !strings.Contains(workflow, "permissions:\n  contents: read") {
		t.Error("CI must declare read-only repository permissions")
	}
	if !strings.Contains(workflow, "persist-credentials: false") {
		t.Error("CI checkout must not persist GitHub credentials")
	}
	actionPin := regexp.MustCompile(`(?m)^\s+uses:\s+[^\s@]+@([0-9a-f]{40})(?:\s+#.*)?$`)
	usesLine := regexp.MustCompile(`(?m)^\s+uses:\s+.+$`)
	if got, want := len(actionPin.FindAllStringSubmatch(workflow, -1)), len(usesLine.FindAllString(workflow, -1)); got != want {
		t.Errorf("every action must use an immutable 40-character commit SHA: pinned %d of %d", got, want)
	}
}

func TestCIImageGateTransitionsAtDockerfile(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../.github/workflows/ci.yaml")
	if err != nil {
		t.Fatal(err)
	}
	workflow := string(data)
	for _, contract := range []string{"hashFiles('Dockerfile') == ''", "hashFiles('Dockerfile') != ''", "make image"} {
		if !strings.Contains(workflow, contract) {
			t.Errorf("image transition contract %q is missing", contract)
		}
	}
}
