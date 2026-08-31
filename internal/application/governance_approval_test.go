package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
)

type approvalProviderStub struct{ verifyErr error }

func (approvalProviderStub) RequestApproval(context.Context, ports.ApprovalChallenge) (string, error) {
	return "provider-request", nil
}
func (provider approvalProviderStub) VerifyApproval(context.Context, ports.ApprovalAssertion) error {
	return provider.verifyErr
}

type approvalStoreStub struct{ approval domain.GovernanceApproval }

func (store *approvalStoreStub) CreateGovernanceApproval(_ context.Context, approval domain.GovernanceApproval) error {
	store.approval = approval
	return nil
}
func (store *approvalStoreStub) GovernanceApproval(context.Context, domain.ID) (domain.GovernanceApproval, error) {
	return store.approval, nil
}
func (store *approvalStoreStub) RecordGovernanceApprovalDecision(_ context.Context, approval domain.GovernanceApproval) error {
	store.approval = approval
	return nil
}
func (store *approvalStoreStub) ConsumeGovernanceApproval(_ context.Context, _ domain.ID, digest string, at time.Time) (domain.GovernanceApproval, error) {
	approval, err := store.approval.Consume(digest, at)
	if err == nil {
		store.approval = approval
	}
	return approval, err
}

func TestGovernanceApprovalServiceProviderBoundFourEyesFlow(t *testing.T) {
	now := time.Date(2026, 8, 31, 10, 0, 0, 0, time.UTC)
	clock := &fixedClock{now: now}
	store := &approvalStoreStub{}
	service, err := NewGovernanceApprovalService(store, approvalProviderStub{}, "test-provider", clock)
	if err != nil {
		t.Fatal(err)
	}
	requester := mustApplicationID(t, "01990a20-0000-7000-8000-000000000001")
	approver := mustApplicationID(t, "01990a20-0000-7000-8000-000000000002")
	tenant := mustApplicationID(t, "01990a20-0000-7000-8000-000000000003")
	id := mustApplicationID(t, "01990a20-0000-7000-8000-000000000004")
	digest := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	approval, err := service.Request(context.Background(), RequestGovernanceApproval{ID: id, TenantID: tenant, RequesterPrincipalID: requester,
		Action: domain.ApprovalGlobalRevocationChange, ResourceType: "revocation", ResourceID: "global", RequestDigest: digest,
		ReasonCode: "incident.containment", ExpiresAt: now.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	clock.now = now.Add(time.Minute)
	approval, err = service.RecordDecision(context.Background(), ports.ApprovalAssertion{ApprovalID: id, ApproverPrincipalID: approver,
		ProviderReference: approval.ProviderReference, DecisionReference: "provider-decision", RequestDigest: digest, Approved: true, DecidedAt: clock.now})
	if err != nil {
		t.Fatal(err)
	}
	clock.now = now.Add(2 * time.Minute)
	consumed, err := service.Consume(context.Background(), tenant, id, digest)
	if err != nil || consumed.State != domain.GovernanceApprovalConsumed {
		t.Fatalf("consume: state=%s err=%v", consumed.State, err)
	}
}

func TestGovernanceApprovalServiceFailsClosedOnProviderAndBinding(t *testing.T) {
	now := time.Date(2026, 8, 31, 10, 0, 0, 0, time.UTC)
	store := &approvalStoreStub{}
	service, _ := NewGovernanceApprovalService(store, approvalProviderStub{verifyErr: errors.New("bad signature")}, "test-provider", &fixedClock{now: now})
	requester := mustApplicationID(t, "01990a20-0000-7000-8000-000000000001")
	approver := mustApplicationID(t, "01990a20-0000-7000-8000-000000000002")
	tenant := mustApplicationID(t, "01990a20-0000-7000-8000-000000000003")
	id := mustApplicationID(t, "01990a20-0000-7000-8000-000000000004")
	digest := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	approval, _ := service.Request(context.Background(), RequestGovernanceApproval{ID: id, TenantID: tenant, RequesterPrincipalID: requester,
		Action: domain.ApprovalPolicyBypass, ResourceType: "policy", ResourceID: "production", RequestDigest: digest,
		ReasonCode: "incident.bypass", ExpiresAt: now.Add(time.Hour)})
	_, err := service.RecordDecision(context.Background(), ports.ApprovalAssertion{ApprovalID: id, ApproverPrincipalID: approver,
		ProviderReference: approval.ProviderReference, DecisionReference: "provider-decision", RequestDigest: digest, Approved: true, DecidedAt: now.Add(time.Minute)})
	if err == nil {
		t.Fatal("accepted unverifiable provider assertion")
	}
}

func mustApplicationID(t *testing.T, value string) domain.ID {
	t.Helper()
	id, err := domain.ParseID(value)
	if err != nil {
		t.Fatal(err)
	}
	return id
}
