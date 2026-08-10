package domain

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

// ID is an opaque UUID version 7 identifier. Its byte representation is kept
// private so callers cannot accidentally derive authorization or tenancy from it.
type ID struct{ bytes [16]byte }

var ErrInvalidID = errors.New("invalid UUIDv7 identifier")

// NewID creates a UUIDv7 using the current UTC time and cryptographic randomness.
func NewID() (ID, error) { return newID(time.Now(), rand.Reader) }

func newID(now time.Time, random io.Reader) (ID, error) {
	var id ID
	milliseconds := now.UTC().UnixMilli()
	if milliseconds < 0 || milliseconds > 1<<48-1 {
		return id, fmt.Errorf("%w: timestamp outside UUIDv7 range", ErrInvalidID)
	}
	if _, err := io.ReadFull(random, id.bytes[:]); err != nil {
		return ID{}, fmt.Errorf("generate ID randomness: %w", err)
	}
	for i := 5; i >= 0; i-- {
		id.bytes[i] = byte(milliseconds)
		milliseconds >>= 8
	}
	id.bytes[6] = id.bytes[6]&0x0f | 0x70 // version 7
	id.bytes[8] = id.bytes[8]&0x3f | 0x80 // RFC 9562 variant
	return id, nil
}

func ParseID(value string) (ID, error) {
	var id ID
	if len(value) != 36 || value != strings.ToLower(value) || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return id, ErrInvalidID
	}
	compact := value[0:8] + value[9:13] + value[14:18] + value[19:23] + value[24:36]
	if _, err := hex.Decode(id.bytes[:], []byte(compact)); err != nil || id.bytes[6]>>4 != 7 || id.bytes[8]>>6 != 2 {
		return ID{}, ErrInvalidID
	}
	return id, nil
}

func (id ID) IsZero() bool { return id == ID{} }

func (id ID) String() string {
	var result [36]byte
	hex.Encode(result[0:8], id.bytes[0:4])
	result[8] = '-'
	hex.Encode(result[9:13], id.bytes[4:6])
	result[13] = '-'
	hex.Encode(result[14:18], id.bytes[6:8])
	result[18] = '-'
	hex.Encode(result[19:23], id.bytes[8:10])
	result[23] = '-'
	hex.Encode(result[24:36], id.bytes[10:16])
	return string(result[:])
}

func (id ID) MarshalText() ([]byte, error) {
	if id.IsZero() {
		return nil, ErrInvalidID
	}
	return []byte(id.String()), nil
}

func (id *ID) UnmarshalText(text []byte) error {
	parsed, err := ParseID(string(text))
	if err != nil {
		return err
	}
	*id = parsed
	return nil
}

func (id ID) MarshalJSON() ([]byte, error) {
	if id.IsZero() {
		return nil, ErrInvalidID
	}
	return json.Marshal(id.String())
}

func (id *ID) UnmarshalJSON(data []byte) error {
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return ErrInvalidID
	}
	return id.UnmarshalText([]byte(value))
}
