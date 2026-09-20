package ports

import (
	"context"
	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"time"
)

type PolicyDraft struct {
	ID        domain.ID `json:"id"`
	Revision  int64     `json:"revision"`
	Digest    string    `json:"digest"`
	Source    string    `json:"source,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}
type ApprovalView struct {
	ID        domain.ID                       `json:"id"`
	Action    domain.GovernanceApprovalAction `json:"action"`
	Resource  string                          `json:"resource"`
	Digest    string                          `json:"request_digest"`
	Requester domain.ID                       `json:"requester_id"`
	Approver  *domain.ID                      `json:"approver_id,omitempty"`
	State     domain.GovernanceApprovalState  `json:"state"`
	ExpiresAt time.Time                       `json:"expires_at"`
}

func ApprovalProjection(a domain.GovernanceApproval, now time.Time) ApprovalView {
	state := a.State
	if (state == domain.GovernanceApprovalPending || state == domain.GovernanceApprovalApproved) && !now.Before(a.ExpiresAt) {
		state = domain.GovernanceApprovalExpired
	}
	v := ApprovalView{ID: a.ID, Action: a.Action, Resource: a.ResourceID, Digest: a.RequestDigest, Requester: a.RequesterPrincipalID, State: state, ExpiresAt: a.ExpiresAt}
	if !a.ApproverPrincipalID.IsZero() {
		v.Approver = &a.ApproverPrincipalID
	}
	return v
}

type ApprovalReceiptReader interface {
	LocalApprovalReceipt(context.Context, domain.ID) (ApprovalAssertion, error)
}
type PolicyEditorStore interface {
	PolicyDraft(context.Context, domain.ID, int64) (PolicyDraft, error)
	SavePolicyDraft(context.Context, AdministrationOperation, domain.ID, int64, string, string) (PolicyDraft, error)
	ListPolicyDrafts(context.Context, domain.ID, int) ([]PolicyDraft, error)
	ListPolicies(context.Context, string, domain.ID, int) ([]PolicyArtifact, error)
	ListPolicyActivations(context.Context, string, domain.ID, int) ([]PolicyActivation, error)
	CreateLocalApproval(context.Context, AdministrationOperation, domain.GovernanceApproval) (ApprovalView, error)
	DecideLocalApproval(context.Context, AdministrationOperation, domain.ID, bool) (ApprovalView, error)
	GovernanceApproval(context.Context, domain.ID) (domain.GovernanceApproval, error)
}
