package policy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/artifact"
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
)

const MaxBundleBytes = 4 << 20

type BundleValidator interface {
	Validate(context.Context, []byte) error
}

func Digest(content []byte) (string, error) {
	if len(content) == 0 || len(content) > MaxBundleBytes {
		return "", errors.New("policy bundle size is outside bounds")
	}
	sum := sha256.Sum256(content)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}
func VerifyBundle(ctx context.Context, revision uint64, contractVersion, expected string, content []byte, signature ports.Signature, verifier ports.Verifier, validator BundleValidator) error {
	if validator == nil {
		return errors.New("policy bundle verifier and validator are required")
	}
	versions := artifact.Versions{artifact.PolicyBundle: {ContractVersion: {}}}
	if err := artifact.Verify(ctx, artifact.Envelope{Kind: artifact.PolicyBundle, FormatVersion: contractVersion, Revision: revision, Digest: expected, Payload: content, Signature: signature}, verifier, versions); err != nil {
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
