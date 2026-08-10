package repository_test

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestMakefileExposesRequiredTargets(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../Makefile")
	if err != nil {
		t.Fatal(err)
	}
	targetPattern := regexp.MustCompile(`(?m)^([a-z][a-z0-9-]*):`)
	available := make(map[string]bool)
	for _, match := range targetPattern.FindAllStringSubmatch(string(data), -1) {
		available[match[1]] = true
	}
	for _, target := range []string{"tools", "generate", "fmt", "lint", "test", "test-race", "test-policy", "test-integration", "test-e2e", "build", "image", "verify"} {
		if !available[target] {
			t.Errorf("Makefile target %q is missing", target)
		}
	}
}

func TestVerifyIncludesRequiredNonContainerGates(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../Makefile")
	if err != nil {
		t.Fatal(err)
	}
	linePattern := regexp.MustCompile(`(?m)^verify:([^#\n]+)`)
	match := linePattern.FindStringSubmatch(string(data))
	if len(match) != 2 {
		t.Fatal("verify target dependencies are missing")
	}
	dependencies := strings.Fields(match[1])
	for _, required := range []string{"generate-check", "lint", "test", "test-race", "test-policy", "test-integration", "test-e2e", "compose-check", "security", "build"} {
		found := false
		for _, dependency := range dependencies {
			if dependency == required {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("verify does not include %q", required)
		}
	}
}
