// Command dependencycheck enforces ThinkPixelAG's Go module source policy.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type policy struct {
	SchemaVersion         int               `json:"schema_version"`
	AllowedModulePrefixes []string          `json:"allowed_module_prefixes"`
	ModuleExceptions      []moduleException `json:"module_exceptions"`
}

type moduleException struct {
	Path      string `json:"path"`
	Version   string `json:"version"`
	Owner     string `json:"owner"`
	Reason    string `json:"reason"`
	Approval  string `json:"approval"`
	ExpiresOn string `json:"expires_on"`
}

type module struct {
	Path      string
	Version   string
	Main      bool
	Replace   *module
	Retracted []string
	Error     *struct{ Err string }
}

func main() {
	moduleDir := flag.String("module-dir", ".", "directory containing the application go.mod")
	policyPath := flag.String("policy", "dependency-policy.json", "dependency policy JSON file")
	flag.Parse()

	if err := run(*moduleDir, *policyPath, time.Now().UTC()); err != nil {
		fmt.Fprintln(os.Stderr, "dependencycheck:", err)
		os.Exit(1)
	}
}

func run(moduleDir, policyPath string, now time.Time) error {
	p, err := loadPolicy(policyPath)
	if err != nil {
		return err
	}
	if err := validatePolicy(p, now); err != nil {
		return err
	}

	cmd := exec.Command("go", "list", "-mod=readonly", "-m", "-retracted", "-json", "all")
	cmd.Dir = moduleDir
	cmd.Stderr = os.Stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("capture go list output: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start go list: %w", err)
	}

	modules, decodeErr := decodeModules(stdout)
	waitErr := cmd.Wait()
	if decodeErr != nil {
		return decodeErr
	}
	if waitErr != nil {
		return fmt.Errorf("go list failed: %w", waitErr)
	}

	violations := auditModules(modules, p)
	if len(violations) != 0 {
		return fmt.Errorf("policy violations:\n  - %s", strings.Join(violations, "\n  - "))
	}

	fmt.Printf("dependencycheck: %d modules comply with source policy\n", len(modules))
	return nil
}

func loadPolicy(path string) (policy, error) {
	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		return policy{}, fmt.Errorf("open policy: %w", err)
	}
	defer f.Close()

	decoder := json.NewDecoder(f)
	decoder.DisallowUnknownFields()
	var p policy
	if err := decoder.Decode(&p); err != nil {
		return policy{}, fmt.Errorf("decode policy: %w", err)
	}
	if err := ensureEOF(decoder); err != nil {
		return policy{}, err
	}
	return p, nil
}

func ensureEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("policy contains multiple JSON values")
		}
		return fmt.Errorf("decode trailing policy data: %w", err)
	}
	return nil
}

func validatePolicy(p policy, now time.Time) error {
	if p.SchemaVersion != 1 {
		return fmt.Errorf("unsupported policy schema_version %d", p.SchemaVersion)
	}
	if len(p.AllowedModulePrefixes) == 0 {
		return errors.New("allowed_module_prefixes must not be empty")
	}
	for _, prefix := range p.AllowedModulePrefixes {
		if prefix == "" || !strings.HasSuffix(prefix, "/") || strings.ContainsAny(prefix, "*? ") {
			return fmt.Errorf("invalid allowed module prefix %q", prefix)
		}
	}
	seen := make(map[string]struct{}, len(p.ModuleExceptions))
	for _, exception := range p.ModuleExceptions {
		key := exception.Path + "@" + exception.Version
		if exception.Path == "" || exception.Version == "" || exception.Owner == "" ||
			exception.Reason == "" || exception.Approval == "" {
			return fmt.Errorf("exception %q has an empty required field", key)
		}
		if strings.ContainsAny(exception.Path+exception.Version, "*?") {
			return fmt.Errorf("exception %q contains a wildcard", key)
		}
		expires, err := time.Parse("2006-01-02", exception.ExpiresOn)
		if err != nil {
			return fmt.Errorf("exception %q has invalid expires_on: %w", key, err)
		}
		if expires.Before(now.Truncate(24 * time.Hour)) {
			return fmt.Errorf("exception %q expired on %s", key, exception.ExpiresOn)
		}
		if _, exists := seen[key]; exists {
			return fmt.Errorf("duplicate exception %q", key)
		}
		seen[key] = struct{}{}
	}
	return nil
}

func decodeModules(r io.Reader) ([]module, error) {
	decoder := json.NewDecoder(r)
	var modules []module
	for {
		var m module
		if err := decoder.Decode(&m); errors.Is(err, io.EOF) {
			return modules, nil
		} else if err != nil {
			return nil, fmt.Errorf("decode go list output: %w", err)
		}
		modules = append(modules, m)
	}
}

func auditModules(modules []module, p policy) []string {
	exceptions := make(map[string]struct{}, len(p.ModuleExceptions))
	for _, exception := range p.ModuleExceptions {
		exceptions[exception.Path+"@"+exception.Version] = struct{}{}
	}

	var violations []string
	for _, m := range modules {
		if m.Main {
			continue
		}
		key := m.Path + "@" + m.Version
		if m.Error != nil {
			violations = append(violations, key+": module error: "+m.Error.Err)
		}
		if m.Version == "" {
			violations = append(violations, m.Path+": dependency has no exact version")
		}
		if m.Replace != nil {
			violations = append(violations, key+": replace directives are not allowed")
		}
		if len(m.Retracted) != 0 {
			violations = append(violations, key+": selected version is retracted")
		}
		if _, excepted := exceptions[key]; excepted {
			continue
		}
		if isPseudoVersion(m.Version) {
			violations = append(violations, key+": pseudo-version requires an exception")
		}
		if !hasAllowedPrefix(m.Path, p.AllowedModulePrefixes) {
			violations = append(violations, key+": module source prefix is not allowed")
		}
	}
	sort.Strings(violations)
	return violations
}

func hasAllowedPrefix(path string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

func isPseudoVersion(version string) bool {
	parts := strings.Split(version, "-")
	if len(parts) < 3 {
		return false
	}
	timestamp := parts[len(parts)-2]
	return len(timestamp) == 14 && strings.IndexFunc(timestamp, func(r rune) bool {
		return r < '0' || r > '9'
	}) == -1
}
