package repository_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestKubernetesBaseIsRestrictedAndOperational(t *testing.T) {
	t.Parallel()
	read := func(name string) string {
		data, err := os.ReadFile(filepath.Join("..", "deploy", "kubernetes", "base", name))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	deployment := read("deployment.yaml")
	for _, required := range []string{"runAsNonRoot: true", "runAsUser: 65532", "readOnlyRootFilesystem: true", "allowPrivilegeEscalation: false", "drop: [ALL]", "type: RuntimeDefault", "startupProbe:", "path: /livez", "livenessProbe:", "readinessProbe:", "path: /readyz", "resources:", "topologySpreadConstraints:", "maxUnavailable: 0", "terminationGracePeriodSeconds: 30", "@sha256:"} {
		if !strings.Contains(deployment, required) {
			t.Errorf("Deployment is missing %q", required)
		}
	}
	for _, file := range []string{"serviceaccount.yaml", "configmap.yaml", "service.yaml", "migration-job.yaml", "networkpolicy.yaml", "poddisruptionbudget.yaml"} {
		_ = read(file)
	}
	if got := read("serviceaccount.yaml"); !strings.Contains(got, "automountServiceAccountToken: false") {
		t.Error("ServiceAccount token must not be mounted")
	}
	if got := read("migration-job.yaml"); !strings.Contains(got, "thinkpixelag-migration") || !strings.Contains(got, "/thinkpixelag-migrate") {
		t.Error("migration Job must use its dedicated secret and command")
	}
	if got := read("networkpolicy.yaml"); !strings.Contains(got, "thinkpixelag-default-deny") || !strings.Contains(got, "policyTypes: [Ingress, Egress]") {
		t.Error("default-deny NetworkPolicy is missing")
	}
}

func TestKubernetesOptionalOperationsAssets(t *testing.T) {
	t.Parallel()
	for _, file := range []string{"hpa.yaml", "servicemonitor.yaml", "prometheusrule.yaml"} {
		data, err := os.ReadFile(filepath.Join("..", "deploy", "kubernetes", "optional", file))
		if err != nil {
			t.Fatal(err)
		}
		if strings.TrimSpace(string(data)) == "" {
			t.Errorf("%s is empty", file)
		}
	}
	dashboard, err := os.ReadFile("../deploy/monitoring/dashboard.json")
	if err != nil {
		t.Fatal(err)
	}
	var parsed any
	if err := json.Unmarshal(dashboard, &parsed); err != nil {
		t.Fatalf("dashboard JSON: %v", err)
	}
	for _, signal := range []string{"API", "Policy", "Database", "Outbox", "Allocation", "Run", "Revocation", "cache", "Go runtime"} {
		if !strings.Contains(string(dashboard), signal) {
			t.Errorf("dashboard lacks %s signal", signal)
		}
	}
}

func TestReleaseAutomationHasSupplyChainOutputsAndThresholds(t *testing.T) {
	t.Parallel()
	workflow, err := os.ReadFile("../.github/workflows/release.yaml")
	if err != nil {
		t.Fatal(err)
	}
	script, err := os.ReadFile("../scripts/release-artifacts.sh")
	if err != nil {
		t.Fatal(err)
	}
	dockerfile, err := os.ReadFile("../Dockerfile")
	if err != nil {
		t.Fatal(err)
	}
	all := string(workflow) + string(script) + string(dockerfile)
	for _, required := range []string{"linux/amd64,linux/arm64", "--provenance=mode=max", "--sbom=true", "CRITICAL,HIGH", "SHA256SUMS", "provenance.json", "cosign sign", "cosign verify", "api/openapi", "draft", "@sha256:"} {
		if !strings.Contains(all, required) {
			t.Errorf("release automation is missing %q", required)
		}
	}
}

func TestReleaseOperationalRunbooksCoverRequiredRecovery(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../docs/operations/runbooks.md")
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ToLower(string(data))
	for _, required := range []string{"install and configure", "migration", "upgrade", "rollback", "backup", "restore", "policy rollback", "revocation gap", "database outage", "opa", "valkey", "outbox backlog", "key rotation", "break glass"} {
		if !strings.Contains(text, required) {
			t.Errorf("runbook lacks %q", required)
		}
	}
}
