// Package localkeys implements ADR-0016's explicitly development-only key store.
package localkeys

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
	"os"
	"path/filepath"
)

type Key struct {
	private  ed25519.PrivateKey
	metadata ports.SigningKey
}

// Open never creates keys as an API startup side effect. Provision is an
// explicit operator operation. Mode checks deliberately reject permissive mounts.
func Open(path string) (*Key, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("local signing key must be a private regular file")
	}
	dir, err := os.Stat(filepath.Dir(path))
	if err != nil || dir.Mode().Perm()&0077 != 0 {
		return nil, errors.New("local signing directory must be private")
	}
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) != ed25519.SeedSize {
		return nil, errors.New("invalid local signing key")
	}
	private := ed25519.NewKeyFromSeed(raw)
	sum := sha256.Sum256(private.Public().(ed25519.PublicKey))
	version := hex.EncodeToString(sum[:])
	return &Key{private: private, metadata: ports.SigningKey{ID: "local-development-" + version, Version: version, Algorithm: ports.SignatureEd25519, Protection: ports.KeyProtectionSoftware, Enabled: true, Exportable: true, CanSign: true, CanVerify: true}}, nil
}
func Provision(path string) (*Key, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	if _, err := os.Lstat(path); err == nil {
		return Open(path)
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	seed := make([]byte, ed25519.SeedSize)
	if _, err := rand.Read(seed); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	if _, err = f.Write(seed); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return nil, err
	}
	if closeErr != nil {
		return nil, closeErr
	}
	return Open(path)
}
func (k *Key) ID() string { return k.metadata.ID }
func (k *Key) DescribeKey(_ context.Context, id string) (ports.SigningKey, error) {
	if id != k.metadata.ID {
		return ports.SigningKey{}, errors.New("unknown development key")
	}
	return k.metadata, nil
}
func (k *Key) Sign(_ context.Context, id string, d ports.SigningDigest) (ports.Signature, error) {
	if id != k.metadata.ID || d.Hash != "SHA-256" || len(d.Value) != 32 {
		return ports.Signature{}, errors.New("invalid development signing request")
	}
	return ports.Signature{KeyID: id, KeyVersion: k.metadata.Version, Algorithm: ports.SignatureEd25519, Value: ed25519.Sign(k.private, d.Value)}, nil
}
func (k *Key) Verify(_ context.Context, s ports.Signature, d ports.SigningDigest) error {
	if s.KeyID != k.metadata.ID || s.KeyVersion != k.metadata.Version || s.Algorithm != ports.SignatureEd25519 || d.Hash != "SHA-256" || len(d.Value) != 32 || !ed25519.Verify(k.private.Public().(ed25519.PublicKey), d.Value, s.Value) {
		return errors.New("invalid development signature")
	}
	return nil
}
