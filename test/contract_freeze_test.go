package repository_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/bdobrica/ThinkPixelAG/internal/policy"
)

func TestReleaseContractsFrozen(t *testing.T) {
	raw, err := os.ReadFile("../api/contract-freeze.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Files          map[string]string `json:"files"`
		PolicyVersion  string            `json:"policy_version"`
		OpenAPIVersion string            `json:"openapi_version"`
	}
	if err = json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.PolicyVersion != policy.ContractVersion {
		t.Fatal("policy version differs from the reviewed freeze")
	}
	paths := []string{"api/openapi/thinkpixelag.yaml", "docs/contracts/policy-decision.md", "internal/policy/contract.go"}
	schemas, err := filepath.Glob("../api/schemas/*.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range schemas {
		paths = append(paths, filepath.ToSlash(path[3:]))
	}
	if len(manifest.Files) != len(paths) {
		t.Fatal("contract set differs from the reviewed freeze")
	}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join("..", path))
			if err != nil {
				t.Fatal(err)
			}
			raw = bytes.ReplaceAll(raw, []byte("\r\n"), []byte("\n"))
			digest := sha256.Sum256(raw)
			if hex.EncodeToString(digest[:]) != manifest.Files[path] {
				t.Fatal("frozen contract changed: review compatibility and versioning before regenerating api/contract-freeze.json")
			}
			if path == "api/openapi/thinkpixelag.yaml" && !bytes.Contains(raw, []byte("\n  version: "+manifest.OpenAPIVersion+"\n")) {
				t.Fatal("OpenAPI version differs from freeze metadata")
			}
		})
	}
}
