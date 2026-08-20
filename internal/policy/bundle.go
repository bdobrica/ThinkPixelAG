package policy

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"
)

const MaxBundleBytes = 4 << 20

type SignatureVerifier interface {
	Verify(context.Context, string, []byte, []byte) error
}
type BundleValidator interface {
	Validate(context.Context, []byte) error
}
type Ed25519Verifier struct{ Keys map[string]ed25519.PublicKey }

func (v Ed25519Verifier) Verify(_ context.Context, keyID string, content, signature []byte) error {
	key, ok := v.Keys[keyID]
	if !ok || len(key) != ed25519.PublicKeySize || !ed25519.Verify(key, content, signature) {
		return errors.New("bundle signature verification failed")
	}
	return nil
}
func Digest(content []byte) (string, error) {
	if len(content) == 0 || len(content) > MaxBundleBytes {
		return "", errors.New("policy bundle size is outside bounds")
	}
	sum := sha256.Sum256(content)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}
func VerifyBundle(ctx context.Context, expected, keyID string, content, signature []byte, verifier SignatureVerifier, validator BundleValidator) error {
	digest, err := Digest(content)
	if err != nil {
		return err
	}
	if len(expected) != 71 || subtle.ConstantTimeCompare([]byte(digest), []byte(expected)) != 1 {
		return errors.New("policy bundle digest mismatch")
	}
	if verifier == nil || validator == nil {
		return errors.New("policy bundle verifier and validator are required")
	}
	if err := verifier.Verify(ctx, keyID, content, signature); err != nil {
		return err
	}
	if err := validator.Validate(ctx, content); err != nil {
		return fmt.Errorf("validate policy bundle: %w", err)
	}
	return nil
}

type ActiveBundle struct {
	TenantID, Channel, BundleID, Digest string
	Version                             int64
	ActivatedAt                         time.Time
	RefreshedAt                         time.Time
}
type Freshness struct {
	mu     sync.RWMutex
	active map[string]ActiveBundle
	maxAge time.Duration
	now    func() time.Time
}

func NewFreshness(maxAge time.Duration, now func() time.Time) (*Freshness, error) {
	if maxAge <= 0 || now == nil {
		return nil, errors.New("policy freshness bound and clock are required")
	}
	return &Freshness{active: map[string]ActiveBundle{}, maxAge: maxAge, now: now}, nil
}
func (f *Freshness) Set(a ActiveBundle) error {
	if a.TenantID == "" || a.Channel == "" || a.Digest == "" || a.Version < 1 || a.ActivatedAt.IsZero() {
		return errors.New("invalid active policy metadata")
	}
	if a.RefreshedAt.IsZero() {
		a.RefreshedAt = f.now()
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	key := a.TenantID + "\x00" + a.Channel
	if old, ok := f.active[key]; ok {
		if a.Version < old.Version || a.Version == old.Version && (a.Digest != old.Digest || a.BundleID != old.BundleID) {
			return errors.New("policy activation version must not regress or conflict")
		}
	}
	f.active[key] = a
	return nil
}
func (f *Freshness) Get(tenant, channel string) (ActiveBundle, bool) {
	f.mu.RLock()
	a, ok := f.active[tenant+"\x00"+channel]
	f.mu.RUnlock()
	now := f.now()
	return a, ok && now.Sub(a.RefreshedAt) >= 0 && now.Sub(a.RefreshedAt) <= f.maxAge
}
