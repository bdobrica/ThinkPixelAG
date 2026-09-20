package localkeys

import (
	"context"
	"crypto/sha256"
	"github.com/bdobrica/ThinkPixelAG/internal/cryptography"
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
	"os"
	"path/filepath"
	"testing"
)

func TestLocalKeyPersistenceIsolationAndProductionRejection(t *testing.T) {
	dir, err := os.MkdirTemp("", "ag-local-key-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "key")
	key, err := Provision(path)
	if err != nil {
		t.Fatal(err)
	}
	again, err := Open(path)
	if err != nil || again.ID() != key.ID() {
		t.Fatalf("key changed: %v", err)
	}
	sum := sha256.Sum256([]byte("artifact"))
	d := ports.SigningDigest{Hash: "SHA-256", Value: sum[:]}
	sig, err := key.Sign(context.Background(), key.ID(), d)
	if err != nil || again.Verify(context.Background(), sig, d) != nil {
		t.Fatalf("signature failed: %v", err)
	}
	sig.KeyVersion = "other"
	if again.Verify(context.Background(), sig, d) == nil {
		t.Fatal("accepted substituted key version")
	}
	if _, err := cryptography.NewManagedSigner(context.Background(), key.ID(), ports.SignatureEd25519, key, key); err == nil {
		t.Fatal("software key accepted as managed")
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path); err == nil {
		t.Fatal("accepted exposed key")
	}
}
