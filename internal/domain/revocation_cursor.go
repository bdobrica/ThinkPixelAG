package domain

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
)

var ErrInvalidRevocationCursor = errors.New("invalid revocation cursor")

type RevocationCursor struct{ Sequence, SecurityEpoch int64 }
type revocationCursorPayload struct {
	Version       uint8  `json:"v"`
	Tenant        string `json:"t"`
	Sequence      int64  `json:"s"`
	SecurityEpoch int64  `json:"e"`
}
type RevocationCursorCodec struct{ key []byte }

func NewRevocationCursorCodec(key []byte) (*RevocationCursorCodec, error) {
	if len(key) < 32 {
		return nil, errors.New("revocation cursor authentication key must contain at least 32 bytes")
	}
	return &RevocationCursorCodec{key: append([]byte(nil), key...)}, nil
}
func (c *RevocationCursorCodec) Encode(tenant ID, cursor RevocationCursor) (string, error) {
	if c == nil || tenant.IsZero() || cursor.Sequence < 0 || cursor.SecurityEpoch < 0 {
		return "", ErrInvalidRevocationCursor
	}
	p, _ := json.Marshal(revocationCursorPayload{1, tenant.String(), cursor.Sequence, cursor.SecurityEpoch})
	m := hmac.New(sha256.New, c.key)
	_, _ = m.Write(p)
	return base64.RawURLEncoding.EncodeToString(append(p, m.Sum(nil)...)), nil
}
func (c *RevocationCursorCodec) Decode(value string, tenant ID) (RevocationCursor, error) {
	if c == nil || tenant.IsZero() || value == "" || len(value) > 512 {
		return RevocationCursor{}, ErrInvalidRevocationCursor
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(raw) <= sha256.Size || base64.RawURLEncoding.EncodeToString(raw) != value {
		return RevocationCursor{}, ErrInvalidRevocationCursor
	}
	p, sig := raw[:len(raw)-sha256.Size], raw[len(raw)-sha256.Size:]
	m := hmac.New(sha256.New, c.key)
	_, _ = m.Write(p)
	if !hmac.Equal(sig, m.Sum(nil)) {
		return RevocationCursor{}, ErrInvalidRevocationCursor
	}
	var v revocationCursorPayload
	if json.Unmarshal(p, &v) != nil || v.Version != 1 || v.Tenant != tenant.String() || v.Sequence < 0 || v.SecurityEpoch < 0 {
		return RevocationCursor{}, ErrInvalidRevocationCursor
	}
	return RevocationCursor{v.Sequence, v.SecurityEpoch}, nil
}
