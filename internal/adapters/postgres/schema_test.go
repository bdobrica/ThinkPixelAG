package postgres

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestProjectMigrationSourcesAreValid(t *testing.T) {
	t.Parallel()

	dir := projectMigrationsDir(t)
	if err := validateMigrationSources(os.DirFS(dir)); err != nil {
		t.Fatalf("validate project migrations: %v", err)
	}
}

func TestReleasedMigrationChecksumsArePresent(t *testing.T) {
	t.Parallel()
	contents, err := os.ReadFile(filepath.Join(projectMigrationsDir(t), migrationChecksumManifest))
	if err != nil {
		t.Fatal(err)
	}
	for _, migration := range []string{
		"001_create_registry_and_policy.sql", "002_create_runs.sql", "003_create_resources.sql",
		"004_create_revocations.sql", "005_create_delivery_primitives.sql", "006_enforce_agent_version_immutability.sql",
		"007_enforce_agent_approval_append_only.sql",
		"008_enforce_run_resolution_immutability.sql",
		"009_validate_resource_dimensions.sql", "010_enforce_resource_grant_immutability.sql",
		"011_enforce_resource_reservation_item_immutability.sql",
		"012_create_resource_rate_windows.sql",
		"013_create_resource_extensions.sql",
		"014_harden_resource_settlements.sql",
		"015_create_governance_approvals.sql",
	} {
		if !strings.Contains(string(contents), migration) {
			t.Errorf("checksum manifest does not cover %s", migration)
		}
	}
}

func TestPhaseTwoSchemaContracts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		file     string
		required []string
	}{
		{
			file: "001_create_registry_and_policy.sql",
			required: []string{
				"CREATE TABLE tenants", "CREATE TABLE principals", "CREATE TABLE agents",
				"CREATE TABLE agent_versions", "CREATE TABLE agent_capabilities",
				"CREATE TABLE agent_version_approvals", "CREATE TABLE policy_bundles",
				"CREATE TABLE policy_activations", "UNIQUE (tenant_id, agent_id, content_digest)",
				"policy_activations_one_active_channel_idx",
			},
		},
		{
			file: "002_create_runs.sql",
			required: []string{
				"CREATE TABLE runs", "state_version bigint", "CREATE TABLE run_version_resolutions",
				"CREATE TABLE run_signals", "CREATE TABLE run_events",
				"UNIQUE (tenant_id, run_id, sequence)", "fencing_token bigint",
			},
		},
		{
			file: "003_create_resources.sql",
			required: []string{
				"CREATE TABLE resource_dimensions", "CREATE TABLE resource_envelopes",
				"CREATE TABLE resource_envelope_grants", "CREATE TABLE resource_balances",
				"CREATE TABLE resource_reservations", "CREATE TABLE trusted_usage_entries",
				"CREATE TABLE resource_settlements", "UNIQUE (tenant_id, producer_id, source_event_id)",
				"returned_value = reserved_value - consumed_value",
			},
		},
		{
			file: "004_create_revocations.sql",
			required: []string{
				"CREATE TABLE security_epochs", "CREATE TABLE tenant_security_epochs",
				"CREATE TABLE agent_security_epochs", "CREATE TABLE revocations",
				"CREATE TABLE revocation_changes", "CREATE TABLE revocation_log",
				"GENERATED ALWAYS AS IDENTITY", "CREATE TABLE gateway_checkpoints",
			},
		},
		{
			file: "005_create_delivery_primitives.sql",
			required: []string{
				"CREATE TABLE idempotency_records", "request_hash text", "CREATE TABLE audit_events",
				"CREATE TABLE outbox_messages", "claimed_until timestamptz", "attempt_count integer",
				"dead_lettered_at timestamptz", "outbox_messages_ready_idx",
			},
		},
		{
			file: "006_enforce_agent_version_immutability.sql",
			required: []string{
				"agent_versions_immutable", "agent_capabilities_immutable",
				"reject_agent_artifact_mutation", "BETWEEN 1 AND 1024",
			},
		},
		{
			file:     "007_enforce_agent_approval_append_only.sql",
			required: []string{"agent_version_approvals_append_only", "reject_agent_artifact_mutation"},
		},
		{
			file:     "008_enforce_run_resolution_immutability.sql",
			required: []string{"run_version_resolutions_immutable", "reject_agent_artifact_mutation"},
		},
		{
			file: "009_validate_resource_dimensions.sql",
			required: []string{
				"resource_dimensions_unit_canonical", "resource_dimensions_class_aggregation",
				"resource_dimensions_deadline_representation", "unix_microseconds_utc",
			},
		},
		{file: "010_enforce_resource_grant_immutability.sql", required: []string{"resource_envelope_grants_immutable", "reject_agent_artifact_mutation"}},
		{file: "011_enforce_resource_reservation_item_immutability.sql", required: []string{"resource_reservation_items_immutable", "reject_agent_artifact_mutation"}},
		{file: "012_create_resource_rate_windows.sql", required: []string{"CREATE TABLE resource_rate_windows", "used_value bigint", "date_trunc('minute', window_start)"}},
		{file: "013_create_resource_extensions.sql", required: []string{"CREATE TABLE resource_extensions", "CREATE TABLE resource_extension_items", "content_digest text", "resource_extensions_immutable"}},
		{file: "014_harden_resource_settlements.sql", required: []string{"policy_decision_id", "idempotency_key", "resource_settlements_immutable", "resource_settlement_items_immutable"}},
		{file: "015_create_governance_approvals.sql", required: []string{"CREATE TABLE governance_approval_requests", "CREATE TABLE governance_approval_decisions", "CREATE TABLE governance_approval_consumptions", "requester_principal_id <> approver_principal_id", "governance_approval_requests_append_only"}},
	}

	dir := projectMigrationsDir(t)
	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			contents, err := os.ReadFile(filepath.Join(dir, tt.file))
			if err != nil {
				t.Fatal(err)
			}
			for _, required := range tt.required {
				if !strings.Contains(string(contents), required) {
					t.Errorf("migration does not contain required schema contract %q", required)
				}
			}
		})
	}
}

func projectMigrationsDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve schema test source path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "..", "migrations"))
}
