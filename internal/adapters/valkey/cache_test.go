package valkey

import (
	"context"
	"testing"
	"time"
)

var testIntegrityKey = []byte("0123456789abcdef0123456789abcdef")

func TestNewRejectsUnsafeConfiguration(t *testing.T) {
	for _, raw := range []string{"http://localhost", "redis://", "redis://localhost/99", "redis://localhost?token=x"} {
		if _, err := New(raw, time.Second, testIntegrityKey); err == nil {
			t.Errorf("accepted %q", raw)
		}
	}
	if _, err := New("redis://localhost", time.Second, []byte("short")); err == nil {
		t.Fatal("accepted short integrity key")
	}
}
func TestCacheRejectsInvalidWrites(t *testing.T) {
	c, err := New("redis://localhost:6379", time.Second, testIntegrityKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Set(context.Background(), "key", nil, time.Second); err == nil {
		t.Fatal("accepted empty value")
	}
	if err := c.Set(context.Background(), "key", []byte("x"), 0); err == nil {
		t.Fatal("accepted zero TTL")
	}
}
