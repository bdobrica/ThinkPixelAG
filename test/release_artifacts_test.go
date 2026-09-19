package repository_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestReleaseArtifactsBindImmutableImageAndCleanSource(t *testing.T) {
	root := t.TempDir()
	script, err := filepath.Abs("../scripts/release-artifacts.sh")
	if err != nil {
		t.Fatal(err)
	}
	write := func(name, value string, mode os.FileMode) {
		t.Helper()
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(value), mode); err != nil {
			t.Fatal(err)
		}
	}
	write(".gitignore", "out/\nbin/\n", 0600)
	write("api/openapi/thinkpixelag.yaml", "openapi: 3.1.0\n", 0600)
	write("api/schemas/example.json", "{}\n", 0600)
	write("deploy/kubernetes/example.yaml", "kind: Deployment\n", 0600)
	// The scanner stub records threshold arguments, writes fixtures and can fail.
	// This tests artifact assembly, not real vulnerability detection.
	write("bin/trivy", "#!/bin/sh\n[ -z \"${FAIL_SCAN:-}\" ] || exit 23\nprintf '%s\\n' \"$*\" >> out/scans.txt\nwhile [ $# -gt 0 ]; do\n if [ \"$1\" = --output ]; then shift; printf '{}\\n' > \"$1\"; fi\n shift\ndone\n", 0700)
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git: %v: %s", err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "--quiet")
	git("add", ".")
	git("-c", "user.name=Release Test", "-c", "user.email=release-test@example.invalid", "-c", "commit.gpgsign=false", "commit", "--quiet", "-m", "fixture")
	revision, created := git("rev-parse", "HEAD"), git("show", "-s", "--format=%ct", "HEAD")
	digest := strings.Repeat("a", 64)
	run := func(image, rev, stamp string, extra ...string) ([]byte, error) {
		cmd := exec.Command("bash", script)
		cmd.Dir = root
		// Do not inherit caller signing credentials or tool overrides.
		cmd.Env = []string{"PATH=" + filepath.Join(root, "bin") + ":" + os.Getenv("PATH"), "HOME=" + root,
			"VERSION=0.1.0-rc.1", "REVISION=" + rev, "SOURCE_DATE_EPOCH=" + stamp,
			"IMAGE=" + image, "OUTPUT_DIR=out"}
		cmd.Env = append(cmd.Env, extra...)
		return cmd.CombinedOutput()
	}
	image := "example.invalid/ag@sha256:" + digest
	for _, tc := range []struct{ image, rev, stamp string }{
		{"example.invalid/ag:mutable", revision, created},
		{image, strings.Repeat("b", 40), created},
		{image, revision, "1"},
	} {
		if out, err := run(tc.image, tc.rev, tc.stamp); err == nil {
			t.Fatalf("accepted unbound release: %s", out)
		}
	}
	if out, err := run(image, revision, created); err != nil {
		t.Fatalf("release: %v: %s", err, out)
	}
	data, err := os.ReadFile(filepath.Join(root, "out/provenance.json"))
	if err != nil {
		t.Fatal(err)
	}
	var metadata struct {
		Authentication string                             `json:"authentication"`
		Image          struct{ Digest map[string]string } `json:"image"`
		Source         struct {
			GitCommit string `json:"git_commit"`
		} `json:"source"`
	}
	if err := json.Unmarshal(data, &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata.Image.Digest["sha256"] != digest || metadata.Source.GitCommit != revision || metadata.Authentication != "unsigned-local-metadata" {
		t.Fatalf("incorrect release identity: %s", data)
	}
	cmd := exec.Command("sha256sum", "--check", "SHA256SUMS")
	cmd.Dir = filepath.Join(root, "out")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("checksums: %v: %s", err, out)
	}
	scans, err := os.ReadFile(filepath.Join(root, "out/scans.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(scans), "--severity CRITICAL,HIGH") || strings.Contains(string(scans), "--ignore-unfixed") {
		t.Fatal("critical/high threshold did not include unfixed findings")
	}
	if _, err := run(image, revision, created, "FAIL_SCAN=1"); err == nil {
		t.Fatal("scanner failure accepted")
	}
	write("api/schemas/example.json", "{\"dirty\":true}\n", 0600)
	if _, err := run(image, revision, created); err == nil {
		t.Fatal("dirty tracked source accepted")
	}
	git("checkout", "--", "api/schemas/example.json")
	write("untracked.go", "package fixture\n", 0600)
	if _, err := run(image, revision, created); err == nil {
		t.Fatal("untracked source accepted")
	}
}
