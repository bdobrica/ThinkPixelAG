package domain

import (
	"errors"
	"time"
)

var ErrTimestampNotUTC = errors.New("timestamp must use UTC")

// Clock makes authoritative time replaceable in deterministic domain tests.
type Clock interface{ Now() time.Time }

type SystemClock struct{}

func (SystemClock) Now() time.Time { return time.Now().UTC() }

// RequireUTC rejects values carrying a non-UTC location or offset and strips
// the monotonic component before persistence or comparison.
func RequireUTC(value time.Time) (time.Time, error) {
	name, offset := value.Zone()
	if offset != 0 || name != "UTC" || value.Location() != time.UTC {
		return time.Time{}, ErrTimestampNotUTC
	}
	return time.Unix(0, value.UnixNano()).UTC(), nil
}
