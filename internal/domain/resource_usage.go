package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

var ErrInvalidTrustedUsage = errors.New("invalid trusted usage event")

// TrustedUsage is an authenticated producer's immutable, incremental usage
// observation. Quantity is never a correction or a release.
type TrustedUsage struct {
	ID, TenantID, RunID, ProducerID ID
	SourceEventID, ResourceName     string
	Quantity                        Quantity
	ObservedAt, RecordedAt          time.Time
	ContentDigest                   string
}

type UsageReceipt struct {
	UsageID    ID
	Duplicate  bool
	AcceptedAt time.Time
}

func ValidateTrustedUsage(usage TrustedUsage) (TrustedUsage, error) {
	if usage.ID.IsZero() || usage.TenantID.IsZero() || usage.RunID.IsZero() || usage.ProducerID.IsZero() ||
		len(usage.SourceEventID) < 1 || len(usage.SourceEventID) > 256 || strings.TrimSpace(usage.SourceEventID) != usage.SourceEventID ||
		!resourceDimensionNamePattern.MatchString(usage.ResourceName) {
		return TrustedUsage{}, ErrInvalidTrustedUsage
	}
	if _, err := NewQuantity(usage.Quantity.Amount(), usage.Quantity.Unit()); err != nil {
		return TrustedUsage{}, fmt.Errorf("%w: %v", ErrInvalidTrustedUsage, err)
	}
	observed, err := RequireUTC(usage.ObservedAt)
	if err != nil {
		return TrustedUsage{}, fmt.Errorf("%w: observed time", ErrInvalidTrustedUsage)
	}
	recorded, err := RequireUTC(usage.RecordedAt)
	if err != nil {
		return TrustedUsage{}, fmt.Errorf("%w: recorded time", ErrInvalidTrustedUsage)
	}
	usage.ObservedAt, usage.RecordedAt = observed, recorded
	material := fmt.Sprintf("trusted-usage/v1\x00%s\x00%s\x00%s\x00%s\x00%s\x00%d\x00%d\x00%s\x00%s",
		usage.TenantID, usage.RunID, usage.ProducerID, usage.SourceEventID, usage.ResourceName,
		usage.Quantity.Amount().Coefficient(), usage.Quantity.Amount().Scale(), usage.Quantity.Unit(), observed.Format(time.RFC3339Nano))
	digest := sha256.Sum256([]byte(material))
	usage.ContentDigest = "sha256:" + hex.EncodeToString(digest[:])
	return usage, nil
}
