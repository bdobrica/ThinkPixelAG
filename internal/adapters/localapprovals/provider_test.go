package localapprovals

import (
	"context"
	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
	"testing"
	"time"
)

type receiptReader struct{ a ports.ApprovalAssertion }

func (r receiptReader) LocalApprovalReceipt(context.Context, domain.ID) (ports.ApprovalAssertion, error) {
	return r.a, nil
}
func TestReceiptBindsExactAuthenticatedDecision(t *testing.T) {
	id, _ := domain.NewID()
	actor, _ := domain.NewID()
	a := ports.ApprovalAssertion{ApprovalID: id, ApproverPrincipalID: actor, ProviderReference: "local:request", DecisionReference: "receipt", RequestDigest: "digest", Approved: true, DecidedAt: time.Now().UTC()}
	p := &Provider{Store: receiptReader{a}}
	if err := p.VerifyApproval(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*ports.ApprovalAssertion){func(v *ports.ApprovalAssertion) { v.ApprovalID, _ = domain.NewID() }, func(v *ports.ApprovalAssertion) { v.ApproverPrincipalID, _ = domain.NewID() }, func(v *ports.ApprovalAssertion) { v.RequestDigest = "other" }, func(v *ports.ApprovalAssertion) { v.Approved = false }, func(v *ports.ApprovalAssertion) { v.DecidedAt = v.DecidedAt.Add(time.Second) }} {
		bad := a
		mutate(&bad)
		if p.VerifyApproval(context.Background(), bad) == nil {
			t.Fatal("accepted substituted receipt")
		}
	}
}
