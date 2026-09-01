// Package logging provides the service's structured, redacting slog pipeline.
package logging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"reflect"
	"strings"
	"unicode"
)

const (
	RedactedValue = "[REDACTED]"
	maxDepth      = 8
	maxIDLength   = 128
)

type correlationKey struct{}

// Correlation contains bounded identifiers derived by trusted middleware.
type Correlation struct {
	RequestID string
	TraceID   string
}

// WithCorrelation attaches validated correlation identifiers to a context.
// Invalid identifiers are omitted rather than copied into logs.
func WithCorrelation(ctx context.Context, correlation Correlation) context.Context {
	correlation.RequestID = safeIdentifier(correlation.RequestID)
	correlation.TraceID = safeIdentifier(correlation.TraceID)
	return context.WithValue(ctx, correlationKey{}, correlation)
}

// CorrelationFromContext returns trusted, validated correlation identifiers.
func CorrelationFromContext(ctx context.Context) Correlation {
	correlation, _ := ctx.Value(correlationKey{}).(Correlation)
	return correlation
}

// ParseLevel converts the supported configuration vocabulary to slog levels.
func ParseLevel(value string) (slog.Level, error) {
	switch strings.ToLower(value) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("unsupported log level %q", value)
	}
}

// New constructs a JSON logger with centralized redaction and correlation.
func New(destination io.Writer, level string) (*slog.Logger, error) {
	parsed, err := ParseLevel(level)
	if err != nil {
		return nil, err
	}
	base := slog.NewJSONHandler(destination, &slog.HandlerOptions{Level: parsed})
	return slog.New(&Handler{next: base}), nil
}

// Handler redacts structured fields and adds context correlation at the root.
// It keeps WithAttrs and WithGroup immutable and safe for concurrent loggers.
type Handler struct {
	next   slog.Handler
	attrs  []slog.Attr
	groups []string
}

func (h *Handler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h *Handler) Handle(ctx context.Context, record slog.Record) error {
	clean := slog.NewRecord(record.Time, record.Level, record.Message, record.PC)
	clean.AddAttrs(h.attrs...)
	record.Attrs(func(attr slog.Attr) bool {
		if sanitized, ok := sanitizeAttr(attr, 0); ok {
			clean.AddAttrs(nest(sanitized, h.groups))
		}
		return true
	})

	correlation := CorrelationFromContext(ctx)
	if correlation.RequestID != "" {
		clean.AddAttrs(slog.String("request_id", correlation.RequestID))
	}
	if correlation.TraceID != "" {
		clean.AddAttrs(slog.String("trace_id", correlation.TraceID))
	}
	return h.next.Handle(ctx, clean)
}

func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	clone := h.clone()
	for _, attr := range attrs {
		if sanitized, ok := sanitizeAttr(attr, 0); ok {
			clone.attrs = append(clone.attrs, nest(sanitized, clone.groups))
		}
	}
	return clone
}

func (h *Handler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	clone := h.clone()
	clone.groups = append(clone.groups, name)
	return clone
}

func (h *Handler) clone() *Handler {
	return &Handler{
		next:   h.next,
		attrs:  append([]slog.Attr(nil), h.attrs...),
		groups: append([]string(nil), h.groups...),
	}
}

func nest(attr slog.Attr, groups []string) slog.Attr {
	for i := len(groups) - 1; i >= 0; i-- {
		attr = slog.Group(groups[i], attr)
	}
	return attr
}

func sanitizeAttr(attr slog.Attr, depth int) (slog.Attr, bool) {
	if isReservedCorrelationKey(attr.Key) {
		return slog.Attr{}, false
	}
	if isSensitiveKey(attr.Key) {
		return slog.String(attr.Key, RedactedValue), true
	}
	value := attr.Value.Resolve()
	if value.Kind() == slog.KindGroup {
		children := value.Group()
		clean := make([]slog.Attr, 0, len(children))
		for _, child := range children {
			if sanitized, ok := sanitizeAttr(child, depth+1); ok {
				clean = append(clean, sanitized)
			}
		}
		return slog.Attr{Key: attr.Key, Value: slog.GroupValue(clean...)}, true
	}
	if value.Kind() == slog.KindAny {
		value = slog.AnyValue(sanitizeAny(value.Any(), depth+1))
	}
	return slog.Attr{Key: attr.Key, Value: value}, true
}

func sanitizeAny(value any, depth int) any {
	if value == nil {
		return value
	}
	if depth > maxDepth {
		return RedactedValue
	}
	if _, isError := value.(error); isError {
		return RedactedValue
	}
	rv := reflect.ValueOf(value)
	for rv.IsValid() && (rv.Kind() == reflect.Interface || rv.Kind() == reflect.Pointer) {
		if rv.IsNil() {
			return nil
		}
		rv = rv.Elem()
	}
	if !rv.IsValid() {
		return nil
	}

	switch rv.Kind() {
	case reflect.Map:
		if rv.Type().Key().Kind() != reflect.String {
			return value
		}
		clean := make(map[string]any, rv.Len())
		iterator := rv.MapRange()
		for iterator.Next() {
			key := iterator.Key().String()
			if isReservedCorrelationKey(key) {
				continue
			}
			if isSensitiveKey(key) {
				clean[key] = RedactedValue
			} else {
				clean[key] = sanitizeAny(iterator.Value().Interface(), depth+1)
			}
		}
		return clean
	case reflect.Slice, reflect.Array:
		clean := make([]any, rv.Len())
		for i := range rv.Len() {
			clean[i] = sanitizeAny(rv.Index(i).Interface(), depth+1)
		}
		return clean
	case reflect.Struct:
		clean := make(map[string]any)
		for i := range rv.NumField() {
			fieldType := rv.Type().Field(i)
			fieldValue := rv.Field(i)
			if !fieldType.IsExported() || !fieldValue.CanInterface() {
				continue
			}
			key := fieldType.Name
			if tag := strings.Split(fieldType.Tag.Get("json"), ",")[0]; tag == "-" {
				continue
			} else if tag != "" {
				key = tag
			}
			if isReservedCorrelationKey(key) {
				continue
			}
			if isSensitiveKey(key) {
				clean[key] = RedactedValue
			} else {
				clean[key] = sanitizeAny(fieldValue.Interface(), depth+1)
			}
		}
		return clean
	default:
		return value
	}
}

func isSensitiveKey(key string) bool {
	normalized := normalizeKey(key)
	switch normalized {
	case "authorization", "proxy_authorization", "cookie", "set_cookie",
		"password", "passwd", "secret", "credentials", "credential",
		"token", "access_token", "refresh_token", "id_token", "api_key",
		"client_secret", "database_url", "database_dsn", "dsn", "valkey_url",
		"objective", "objectives", "input", "inputs", "prompt", "policy_input":
		return true
	}
	for _, suffix := range []string{"_password", "_passwd", "_secret", "_token", "_api_key", "_credential", "_credentials", "_dsn"} {
		if strings.HasSuffix(normalized, suffix) {
			return true
		}
	}
	return false
}

func isReservedCorrelationKey(key string) bool {
	normalized := normalizeKey(key)
	return normalized == "request_id" || normalized == "trace_id"
}

func normalizeKey(key string) string {
	key = strings.ToLower(strings.TrimSpace(key))
	return strings.NewReplacer("-", "_", ".", "_", " ", "_").Replace(key)
}

func safeIdentifier(value string) string {
	if value == "" || len(value) > maxIDLength || strings.TrimSpace(value) != value {
		return ""
	}
	for _, r := range value {
		if unicode.IsControl(r) || unicode.IsSpace(r) {
			return ""
		}
	}
	return value
}

var _ slog.Handler = (*Handler)(nil)
