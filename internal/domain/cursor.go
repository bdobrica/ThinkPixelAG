package domain

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
)

const MaxCursorLength = 512

var ErrInvalidCursor = errors.New("invalid pagination cursor")

type PageCursor struct {
	SortKey string
	ID      ID
}
type cursorPayload struct {
	Version uint8  `json:"v"`
	SortKey string `json:"k"`
	ID      string `json:"id"`
}
type CursorCodec struct{ key []byte }

func NewCursorCodec(key []byte) (*CursorCodec, error) {
	if len(key) < 32 {
		return nil, errors.New("cursor authentication key must contain at least 32 bytes")
	}
	return &CursorCodec{key: append([]byte(nil), key...)}, nil
}

func (codec *CursorCodec) Encode(cursor PageCursor) (string, error) {
	if cursor.ID.IsZero() || len(cursor.SortKey) == 0 || len(cursor.SortKey) > 256 {
		return "", ErrInvalidCursor
	}
	payload, err := json.Marshal(cursorPayload{Version: 1, SortKey: cursor.SortKey, ID: cursor.ID.String()})
	if err != nil {
		return "", ErrInvalidCursor
	}
	mac := hmac.New(sha256.New, codec.key)
	_, _ = mac.Write(payload)
	result := base64.RawURLEncoding.EncodeToString(append(payload, mac.Sum(nil)...))
	if len(result) > MaxCursorLength {
		return "", ErrInvalidCursor
	}
	return result, nil
}

func (codec *CursorCodec) Decode(value string) (PageCursor, error) {
	if value == "" || len(value) > MaxCursorLength {
		return PageCursor{}, ErrInvalidCursor
	}
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || base64.RawURLEncoding.EncodeToString(decoded) != value || len(decoded) <= sha256.Size {
		return PageCursor{}, ErrInvalidCursor
	}
	payload, signature := decoded[:len(decoded)-sha256.Size], decoded[len(decoded)-sha256.Size:]
	mac := hmac.New(sha256.New, codec.key)
	_, _ = mac.Write(payload)
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return PageCursor{}, ErrInvalidCursor
	}
	var raw cursorPayload
	if err := json.Unmarshal(payload, &raw); err != nil || raw.Version != 1 || raw.SortKey == "" || len(raw.SortKey) > 256 {
		return PageCursor{}, ErrInvalidCursor
	}
	id, err := ParseID(raw.ID)
	if err != nil {
		return PageCursor{}, ErrInvalidCursor
	}
	return PageCursor{SortKey: raw.SortKey, ID: id}, nil
}
