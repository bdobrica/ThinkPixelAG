//go:build integration

package valkey

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

func TestAuthenticatedValkeyRoundTripAndExpiry(t *testing.T) {
	raw := os.Getenv("THINKPIXELAG_TEST_VALKEY_URL")
	if raw == "" {
		t.Skip("THINKPIXELAG_TEST_VALKEY_URL is not set")
	}
	c, err := New(raw, time.Second, testIntegrityKey)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	key := "thinkpixelag:test:" + time.Now().UTC().Format("150405.000000000")
	if err := c.Set(ctx, key, []byte(`{"allow":true}`), 50*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	got, err := c.Get(ctx, key)
	if err != nil || string(got) != `{"allow":true}` {
		t.Fatalf("get=%q err=%v", got, err)
	}
	wrongKey := []byte("abcdef0123456789abcdef0123456789")
	other, err := New(raw, time.Second, wrongKey)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.Get(ctx, key); err == nil {
		t.Fatal("cache entry passed with a different integrity key")
	}
	time.Sleep(80 * time.Millisecond)
	if _, err := c.Get(ctx, key); !errors.Is(err, ErrMiss) {
		t.Fatalf("expired get error=%v", err)
	}
}
func TestWrongValkeyCredentialFails(t *testing.T) {
	raw := os.Getenv("THINKPIXELAG_TEST_VALKEY_BAD_URL")
	if raw == "" {
		t.Skip("THINKPIXELAG_TEST_VALKEY_BAD_URL is not set")
	}
	c, err := New(raw, time.Second, testIntegrityKey)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Get(context.Background(), "missing"); err == nil {
		t.Fatal("wrong credential succeeded")
	}
}

func TestThroughputBlockedMarkerExpires(t *testing.T) {
	raw := os.Getenv("THINKPIXELAG_TEST_VALKEY_URL")
	if raw == "" {
		t.Skip("THINKPIXELAG_TEST_VALKEY_URL is not set")
	}
	c, err := New(raw, time.Second, testIntegrityKey)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	key := "thinkpixelag:rate:test:" + time.Now().UTC().Format("150405.000000000")
	if err := c.MarkBlocked(ctx, key, time.Now().Add(50*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if blocked, err := c.Blocked(ctx, key); err != nil || !blocked {
		t.Fatalf("blocked=%v error=%v", blocked, err)
	}
	time.Sleep(80 * time.Millisecond)
	if blocked, err := c.Blocked(ctx, key); err != nil || blocked {
		t.Fatalf("expired blocked=%v error=%v", blocked, err)
	}
}
