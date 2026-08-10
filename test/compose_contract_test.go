package repository_test

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestComposePinsAndIsolatesLocalDependencies(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../compose.yaml")
	if err != nil {
		t.Fatal(err)
	}
	compose := string(data)
	for _, image := range []string{"postgres:18.4-alpine3.23", "openpolicyagent/opa:1.19.0-debug", "valkey/valkey:9.1.1-alpine3.24"} {
		pattern := regexp.MustCompile(regexp.QuoteMeta("image: "+image+"@sha256:") + `[a-f0-9]{64}`)
		if !pattern.MatchString(compose) {
			t.Errorf("image %q is not pinned by a full digest", image)
		}
	}
	if count := strings.Count(compose, "healthcheck:"); count != 3 {
		t.Errorf("health check count = %d, want 3", count)
	}
	if count := strings.Count(compose, "host_ip: 127.0.0.1"); count != 3 {
		t.Errorf("loopback binding count = %d, want 3", count)
	}
	if !strings.Contains(compose, "profiles: [valkey]") {
		t.Error("Valkey is not optional through its profile")
	}
	if !strings.Contains(compose, "postgres-data:/var/lib/postgresql") {
		t.Error("PostgreSQL state is not isolated in its named volume")
	}
	for _, forbidden := range []string{"latest", "host_ip: 0.0.0.0", "POSTGRES_HOST_AUTH_METHOD: trust"} {
		if strings.Contains(compose, forbidden) {
			t.Errorf("compose contains forbidden value %q", forbidden)
		}
	}
}

func TestMakefileExposesLocalDependencyTargets(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../Makefile")
	if err != nil {
		t.Fatal(err)
	}
	makefile := string(data)
	for _, target := range []string{"compose-check", "dev-up", "dev-up-valkey", "dev-status", "dev-smoke", "dev-down", "dev-reset"} {
		if !regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(target) + `:`).MatchString(makefile) {
			t.Errorf("Makefile target %q is missing", target)
		}
	}
}
