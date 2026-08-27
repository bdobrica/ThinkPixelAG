package domain

import "testing"

func TestRevocationCursorAuthenticatedAndTenantBound(t *testing.T) {
	tenant, _ := NewID()
	other, _ := NewID()
	codec, _ := NewRevocationCursorCodec([]byte("01234567890123456789012345678901"))
	encoded, err := codec.Encode(tenant, RevocationCursor{Sequence: 42, SecurityEpoch: 9})
	if err != nil {
		t.Fatal(err)
	}
	got, err := codec.Decode(encoded, tenant)
	if err != nil || got.Sequence != 42 || got.SecurityEpoch != 9 {
		t.Fatalf("cursor=%+v err=%v", got, err)
	}
	if _, err = codec.Decode(encoded, other); err == nil {
		t.Fatal("cross-tenant cursor accepted")
	}
	tampered := encoded[:len(encoded)-1] + "A"
	if _, err = codec.Decode(tampered, tenant); err == nil {
		t.Fatal("tampered cursor accepted")
	}
}
