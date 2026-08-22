package domain

import (
	"strings"
	"testing"
)

func TestRunEventCursorIsAuthenticatedAndRunBound(t *testing.T) {
	codec, err := NewRunEventCursorCodec([]byte(strings.Repeat("k", 32)))
	if err != nil {
		t.Fatal(err)
	}
	run, _ := NewID()
	other, _ := NewID()
	cursor, err := codec.Encode(run, 42)
	if err != nil {
		t.Fatal(err)
	}
	if sequence, err := codec.Decode(cursor, run); err != nil || sequence != 42 {
		t.Fatalf("sequence=%d err=%v", sequence, err)
	}
	if _, err := codec.Decode(cursor, other); err != ErrInvalidRunEventCursor {
		t.Fatalf("cross-run err=%v", err)
	}
	replacement := byte('A')
	if cursor[len(cursor)/2] == replacement {
		replacement = 'B'
	}
	tampered := cursor[:len(cursor)/2] + string(replacement) + cursor[len(cursor)/2+1:]
	if _, err := codec.Decode(tampered, run); err != ErrInvalidRunEventCursor {
		t.Fatalf("tamper err=%v", err)
	}
}

func TestRunEventCursorRejectsInvalidConstructionAndValues(t *testing.T) {
	if _, err := NewRunEventCursorCodec([]byte("short")); err == nil {
		t.Fatal("accepted short key")
	}
	codec, _ := NewRunEventCursorCodec([]byte(strings.Repeat("z", 32)))
	run, _ := NewID()
	for _, value := range []string{"", "not-base64", strings.Repeat("x", MaxRunEventCursorLength+1)} {
		if _, err := codec.Decode(value, run); err != ErrInvalidRunEventCursor {
			t.Fatalf("value %q err=%v", value, err)
		}
	}
}
