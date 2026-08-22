package domain

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
)

const MaxRunEventCursorLength = 512

var (
	ErrInvalidRunEventCursor = errors.New("invalid run event cursor")
	ErrRunEventCursorGone    = errors.New("run event cursor is outside retention")
)

type runEventCursorPayload struct {
	Version  uint8  `json:"v"`
	RunID    string `json:"r"`
	Sequence int64  `json:"s"`
}

// RunEventCursorCodec authenticates a cursor and binds it to one run. A cursor
// is a position only; callers are authorized independently on every connect.
type RunEventCursorCodec struct{ key []byte }

func (event RunEvent) Validate() error {
	if event.ID.IsZero() || event.RunID.IsZero() || event.Sequence < 1 || !signalNamePattern.MatchString(event.Type) || event.Data == nil || event.OccurredAt.IsZero() {
		return errors.New("run event is invalid")
	}
	if _, err := RequireUTC(event.OccurredAt); err != nil {
		return errors.New("run event time is invalid")
	}
	return nil
}

func NewRunEventCursorCodec(key []byte) (*RunEventCursorCodec, error) {
	if len(key) < 32 {
		return nil, errors.New("run event cursor authentication key must contain at least 32 bytes")
	}
	return &RunEventCursorCodec{key: append([]byte(nil), key...)}, nil
}

func (c *RunEventCursorCodec) Encode(runID ID, sequence int64) (string, error) {
	if c == nil || runID.IsZero() || sequence < 0 {
		return "", ErrInvalidRunEventCursor
	}
	payload, _ := json.Marshal(runEventCursorPayload{Version: 1, RunID: runID.String(), Sequence: sequence})
	mac := hmac.New(sha256.New, c.key)
	_, _ = mac.Write(payload)
	return base64.RawURLEncoding.EncodeToString(append(payload, mac.Sum(nil)...)), nil
}

func (c *RunEventCursorCodec) Decode(value string, runID ID) (int64, error) {
	if c == nil || runID.IsZero() || value == "" || len(value) > MaxRunEventCursorLength {
		return 0, ErrInvalidRunEventCursor
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(raw) <= sha256.Size {
		return 0, ErrInvalidRunEventCursor
	}
	payload, signature := raw[:len(raw)-sha256.Size], raw[len(raw)-sha256.Size:]
	mac := hmac.New(sha256.New, c.key)
	_, _ = mac.Write(payload)
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return 0, ErrInvalidRunEventCursor
	}
	var cursor runEventCursorPayload
	if json.Unmarshal(payload, &cursor) != nil || cursor.Version != 1 || cursor.RunID != runID.String() || cursor.Sequence < 0 {
		return 0, ErrInvalidRunEventCursor
	}
	return cursor.Sequence, nil
}
