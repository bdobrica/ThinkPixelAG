package domain

import (
	"encoding/json"
	"errors"
	"regexp"
	"time"
)

type RunSignalType string

const (
	RunSignalPause  RunSignalType = "PAUSE"
	RunSignalResume RunSignalType = "RESUME"
	RunSignalCustom RunSignalType = "CUSTOM"
)

var signalNamePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9._:-]{0,127}$`)

type RunSignal struct {
	ID, TenantID, RunID, ActorPrincipalID ID
	Type                                  RunSignalType
	Payload                               json.RawMessage
	IdempotencyKey                        string
	ExpectedStateVersion                  *int64
	CreatedAt                             time.Time
}

type RunEvent struct {
	ID, RunID  ID
	Sequence   int64
	Type       string
	Data       map[string]any
	OccurredAt time.Time
}

func (signal RunSignal) Validate() error {
	if signal.ID.IsZero() || signal.TenantID.IsZero() || signal.RunID.IsZero() || signal.ActorPrincipalID.IsZero() || len(signal.IdempotencyKey) < 1 || len(signal.IdempotencyKey) > 256 {
		return errors.New("run signal identity is invalid")
	}
	if signal.ExpectedStateVersion != nil && *signal.ExpectedStateVersion < 1 {
		return errors.New("expected state version is invalid")
	}
	if _, err := RequireUTC(signal.CreatedAt); err != nil || signal.CreatedAt.IsZero() || len(signal.Payload) == 0 || len(signal.Payload) > 64<<10 || !json.Valid(signal.Payload) {
		return errors.New("run signal payload or time is invalid")
	}
	var payload map[string]json.RawMessage
	if json.Unmarshal(signal.Payload, &payload) != nil || payload == nil || len(payload) > 100 {
		return errors.New("run signal payload must be a bounded object")
	}
	switch signal.Type {
	case RunSignalPause:
		if !onlySignalKeys(payload, "reason_code") || !validOptionalSignalName(payload["reason_code"]) {
			return errors.New("pause signal payload is invalid")
		}
	case RunSignalResume:
		if len(payload) != 0 {
			return errors.New("resume signal payload must be empty")
		}
	case RunSignalCustom:
		if !onlySignalKeys(payload, "name", "data") || !validRequiredSignalName(payload["name"]) || payload["data"] == nil {
			return errors.New("custom signal payload is invalid")
		}
		var data map[string]json.RawMessage
		if json.Unmarshal(payload["data"], &data) != nil || data == nil || len(data) > 100 {
			return errors.New("custom signal data must be a bounded object")
		}
	default:
		return errors.New("unsupported run signal type")
	}
	return nil
}

func onlySignalKeys(payload map[string]json.RawMessage, allowed ...string) bool {
	set := make(map[string]bool, len(allowed))
	for _, key := range allowed {
		set[key] = true
	}
	for key := range payload {
		if !set[key] {
			return false
		}
	}
	return true
}

func validRequiredSignalName(raw json.RawMessage) bool {
	var value string
	return json.Unmarshal(raw, &value) == nil && signalNamePattern.MatchString(value)
}

func validOptionalSignalName(raw json.RawMessage) bool {
	return raw == nil || validRequiredSignalName(raw)
}
