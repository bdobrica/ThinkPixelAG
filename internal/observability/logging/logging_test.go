package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

func decodeLines(t *testing.T, buffer *bytes.Buffer) []map[string]any {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(buffer.String()), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return nil
	}
	result := make([]map[string]any, 0, len(lines))
	for _, line := range lines {
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("decode log line %q: %v", line, err)
		}
		result = append(result, entry)
	}
	return result
}

func TestNewAndLevelFiltering(t *testing.T) {
	t.Parallel()
	var buffer bytes.Buffer
	logger, err := New(&buffer, "warn")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	logger.Info("filtered")
	logger.Warn("visible")

	lines := decodeLines(t, &buffer)
	if len(lines) != 1 || lines[0]["msg"] != "visible" || lines[0]["level"] != "WARN" {
		t.Fatalf("unexpected filtered output: %#v", lines)
	}
	if _, err := New(&buffer, "verbose"); err == nil {
		t.Fatal("New() error = nil, want unsupported level error")
	}
}

func TestCorrelationComesOnlyFromContext(t *testing.T) {
	t.Parallel()
	var buffer bytes.Buffer
	logger, err := New(&buffer, "info")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	logger = logger.With("request_id", "spoofed-prebound").WithGroup("operation")
	ctx := WithCorrelation(context.Background(), Correlation{RequestID: "req-123", TraceID: "0123456789abcdef"})
	logger.InfoContext(ctx, "handled", "trace-id", "spoofed-record", "action", "runs.create")

	entry := decodeLines(t, &buffer)[0]
	if entry["request_id"] != "req-123" || entry["trace_id"] != "0123456789abcdef" {
		t.Fatalf("trusted correlation missing: %#v", entry)
	}
	operation, ok := entry["operation"].(map[string]any)
	if !ok || operation["action"] != "runs.create" {
		t.Fatalf("grouped attributes missing: %#v", entry)
	}
	serialized := buffer.String()
	if strings.Contains(serialized, "spoofed") {
		t.Fatalf("caller supplied reserved correlation field: %s", serialized)
	}
}

func TestInvalidCorrelationIsOmitted(t *testing.T) {
	t.Parallel()
	ctx := WithCorrelation(context.Background(), Correlation{RequestID: "bad id", TraceID: strings.Repeat("a", maxIDLength+1)})
	if got := CorrelationFromContext(ctx); got.RequestID != "" || got.TraceID != "" {
		t.Fatalf("invalid correlation retained: %#v", got)
	}
}

type sensitiveLogValue struct{}

func (sensitiveLogValue) LogValue() slog.Value {
	return slog.GroupValue(slog.String("access_token", "logvaluer-secret"), slog.String("result", "allowed"))
}

type sensitiveStruct struct {
	Name       string `json:"name"`
	Password   string `json:"password"`
	Credential string
}

func TestSensitiveFieldsAreRedactedRecursively(t *testing.T) {
	t.Parallel()
	var buffer bytes.Buffer
	logger, err := New(&buffer, "debug")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	deep := map[string]any{"level": map[string]any{"level": map[string]any{"level": map[string]any{"level": map[string]any{"level": map[string]any{"level": map[string]any{"level": map[string]any{"authorization": "deep-secret"}}}}}}}}

	logger.Debug("redaction test",
		slog.String("Authorization", "bearer-secret"),
		slog.String("client-secret", "client-secret-value"),
		slog.Group("request",
			slog.String("objective", "sensitive-objective"),
			slog.Any("headers", map[string][]string{
				"X-API-Key":    {"api-key-secret"},
				"Content-Type": {"application/json"},
			}),
		),
		slog.Any("decision", sensitiveLogValue{}),
		slog.Any("nested", []any{map[string]any{"refresh_token": "refresh-secret", "status": "ok"}}),
		slog.Any("structured", sensitiveStruct{Name: "safe-name", Password: "struct-password", Credential: "struct-credential"}),
		slog.Any("too_deep", deep),
		slog.String("action", "runs.create"),
	)

	serialized := buffer.String()
	for _, secret := range []string{"bearer-secret", "client-secret-value", "sensitive-objective", "api-key-secret", "logvaluer-secret", "refresh-secret", "deep-secret", "struct-password", "struct-credential"} {
		if strings.Contains(serialized, secret) {
			t.Errorf("log output leaked %q: %s", secret, serialized)
		}
	}
	if !strings.Contains(serialized, RedactedValue) || !strings.Contains(serialized, "runs.create") || !strings.Contains(serialized, "application/json") || !strings.Contains(serialized, "safe-name") {
		t.Errorf("redaction removed safe fields or marker: %s", serialized)
	}
}

func TestHandlerWithAttrsAndGroupsIsImmutable(t *testing.T) {
	t.Parallel()
	var buffer bytes.Buffer
	logger, err := New(&buffer, "info")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	component := logger.With("component", "api")
	grouped := component.WithGroup("http")
	component.Info("component")
	grouped.Info("grouped", "route", "/v1/agents")

	lines := decodeLines(t, &buffer)
	if len(lines) != 2 {
		t.Fatalf("log line count = %d, want 2", len(lines))
	}
	if _, exists := lines[0]["http"]; exists {
		t.Fatalf("WithGroup mutated parent handler: %#v", lines[0])
	}
	httpGroup, ok := lines[1]["http"].(map[string]any)
	if !ok || httpGroup["route"] != "/v1/agents" {
		t.Fatalf("grouped handler output = %#v", lines[1])
	}
}
