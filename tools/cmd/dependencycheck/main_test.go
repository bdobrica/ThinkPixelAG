package main

import (
	"strings"
	"testing"
	"time"
)

func TestValidatePolicy(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name    string
		policy  policy
		wantErr string
	}{
		{name: "valid", policy: policy{SchemaVersion: 1, AllowedModulePrefixes: []string{"example.com/"}}},
		{name: "unknown schema", policy: policy{SchemaVersion: 2}, wantErr: "unsupported"},
		{name: "empty allowlist", policy: policy{SchemaVersion: 1}, wantErr: "must not be empty"},
		{name: "wildcard prefix", policy: policy{SchemaVersion: 1, AllowedModulePrefixes: []string{"*.example/"}}, wantErr: "invalid allowed"},
		{
			name: "expired exception",
			policy: policy{SchemaVersion: 1, AllowedModulePrefixes: []string{"example.com/"}, ModuleExceptions: []moduleException{{
				Path: "other.test/mod", Version: "v1.0.0", Owner: "security", Reason: "migration",
				Approval: "SEC-1", ExpiresOn: "2026-08-09",
			}}},
			wantErr: "expired",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := validatePolicy(tt.policy, now)
			if tt.wantErr == "" && err != nil {
				t.Fatalf("validatePolicy() error = %v", err)
			}
			if tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Fatalf("validatePolicy() error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestAuditModules(t *testing.T) {
	t.Parallel()
	p := policy{
		AllowedModulePrefixes: []string{"allowed.example/"},
		ModuleExceptions:      []moduleException{{Path: "legacy.example/module", Version: "v0.0.0-20260101120000-abcdef123456"}},
	}
	modules := []module{
		{Path: "github.com/bdobrica/ThinkPixelAG", Main: true},
		{Path: "allowed.example/good", Version: "v1.2.3"},
		{Path: "legacy.example/module", Version: "v0.0.0-20260101120000-abcdef123456"},
		{Path: "bad.example/unapproved", Version: "v1.0.0"},
		{Path: "allowed.example/pseudo", Version: "v0.0.0-20260101120000-abcdef123456"},
		{Path: "allowed.example/replaced", Version: "v1.0.0", Replace: &module{Path: "fork.example/replaced"}},
		{Path: "allowed.example/retracted", Version: "v1.0.0", Retracted: []string{"broken"}},
	}

	violations := strings.Join(auditModules(modules, p), "\n")
	for _, want := range []string{"unapproved", "pseudo-version", "replace directives", "selected version is retracted"} {
		if !strings.Contains(violations, want) {
			t.Errorf("violations missing %q:\n%s", want, violations)
		}
	}
	if strings.Contains(violations, "legacy.example") {
		t.Errorf("excepted module rejected:\n%s", violations)
	}
}

func TestIsPseudoVersion(t *testing.T) {
	t.Parallel()
	if !isPseudoVersion("v0.0.0-20260810123456-abcdef123456") {
		t.Fatal("expected pseudo-version")
	}
	if isPseudoVersion("v1.2.3-beta.1") {
		t.Fatal("tagged prerelease is not a pseudo-version")
	}
}
