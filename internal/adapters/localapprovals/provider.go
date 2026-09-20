// Package localapprovals verifies authenticated immutable local decision receipts.
package localapprovals

import (
	"context"
	"errors"
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
)

type Provider struct{ Store ports.ApprovalReceiptReader }

func (p *Provider) RequestApproval(_ context.Context, c ports.ApprovalChallenge) (string, error) {
	if c.ApprovalID.IsZero() || c.TenantID.IsZero() || c.RequesterPrincipalID.IsZero() || !c.Action.Valid() {
		return "", errors.New("invalid local approval challenge")
	}
	return "local:" + c.ApprovalID.String(), nil
}
func (p *Provider) VerifyApproval(ctx context.Context, a ports.ApprovalAssertion) error {
	if p.Store == nil {
		return errors.New("local approval receipts unavailable")
	}
	stored, err := p.Store.LocalApprovalReceipt(ctx, a.ApprovalID)
	if err != nil {
		return err
	}
	if stored.ApprovalID != a.ApprovalID || stored.ProviderReference != a.ProviderReference || stored.DecisionReference != a.DecisionReference || stored.ApproverPrincipalID != a.ApproverPrincipalID || stored.RequestDigest != a.RequestDigest || stored.Approved != a.Approved || !stored.DecidedAt.Equal(a.DecidedAt) {
		return errors.New("local approval receipt mismatch")
	}
	return nil
}
