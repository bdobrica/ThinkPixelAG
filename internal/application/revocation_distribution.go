package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/policy"
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
)

const maxRevocationDelta = 10000

type RevocationDistribution struct {
	repository ports.RevocationDistributionRepository
	evaluator  policy.Evaluator
	clock      domain.Clock
	retention  time.Duration
}
type ReconcileRevocations struct {
	TenantID, GatewayPrincipalID domain.ID
	Roles                        []string
	LastSequence                 int64
	Epochs                       domain.EpochVector
}
type RevocationReconciliation struct {
	Mode                  string                     `json:"mode"`
	AuthoritativeSequence int64                      `json:"authoritative_sequence"`
	Epochs                domain.EpochVector         `json:"epochs"`
	Changes               []ports.RevocationLogEntry `json:"changes,omitempty"`
	Snapshot              []domain.Revocation        `json:"snapshot,omitempty"`
	SnapshotDigest        string                     `json:"snapshot_digest,omitempty"`
	ReconciledAt          time.Time                  `json:"reconciled_at"`
}

func NewRevocationDistribution(repository ports.RevocationDistributionRepository, evaluator policy.Evaluator, clock domain.Clock, retention time.Duration) (*RevocationDistribution, error) {
	if repository == nil || evaluator == nil || clock == nil || retention <= 0 {
		return nil, errors.New("revocation distribution requires repository, policy evaluator, clock, and retention")
	}
	return &RevocationDistribution{repository, evaluator, clock, retention}, nil
}
func (s *RevocationDistribution) authorizeGateway(ctx context.Context, tenant, principal domain.ID, roles []string) error {
	if tenant.IsZero() || principal.IsZero() {
		return domain.NewError(domain.CodeUnauthenticated, "verified gateway identity is invalid")
	}
	now, err := domain.RequireUTC(s.clock.Now())
	if err != nil {
		return domain.NewError(domain.CodeInternal, "revocation distribution clock is invalid")
	}
	decisionID, err := domain.NewID()
	if err != nil {
		return domain.NewError(domain.CodeInternal, "could not generate gateway authorization identifier")
	}
	roles = policy.NormalizeStrings(roles)
	result, err := s.evaluator.Decide(ctx, policy.Input{
		ContractVersion: policy.ContractVersion, DecisionID: decisionID.String(), RequestTime: now,
		Subject: policy.Subject{PrincipalID: principal.String(), TenantID: tenant.String(), PrincipalType: "gateway", Roles: roles},
		Action:  "revocations.reconcile", Resource: policy.Resource{Type: "revocation", ID: tenant.String(), TenantID: tenant.String(), Attributes: map[string]any{}},
		RequestedConstraints: map[string]any{}, AuthorityConstraints: map[string]any{}, SecurityState: policy.SecurityState{Authoritative: true}, Context: policy.RequestContext{RequestID: decisionID.String()},
	})
	if err != nil {
		return domain.WrapError(domain.CodeUnavailable, "gateway authorization policy is unavailable", err).WithRetryable()
	}
	if result.Decision.DecisionID != decisionID.String() {
		return domain.NewError(domain.CodeUnavailable, "gateway authorization policy returned invalid evidence").WithRetryable()
	}
	if !result.Decision.Allow {
		return domain.NewError(domain.CodeForbidden, "gateway revocation access is not authorized")
	}
	return nil
}
func (s *RevocationDistribution) Changes(ctx context.Context, tenant, gateway domain.ID, roles []string, after int64, limit int) ([]ports.RevocationLogEntry, error) {
	if err := s.authorizeGateway(ctx, tenant, gateway, roles); err != nil {
		return nil, err
	}
	now, err := domain.RequireUTC(s.clock.Now())
	if err != nil {
		return nil, err
	}
	return s.repository.RevocationChanges(ctx, tenant, after, limit, now.Add(-s.retention))
}
func (s *RevocationDistribution) CheckpointStream(ctx context.Context, tenant, gateway domain.ID, roles []string, sequence int64, epochs domain.EpochVector) error {
	if err := s.authorizeGateway(ctx, tenant, gateway, roles); err != nil {
		return err
	}
	now, err := domain.RequireUTC(s.clock.Now())
	if err != nil {
		return err
	}
	return s.repository.SaveGatewayCheckpoint(ctx, ports.GatewayCheckpoint{TenantID: tenant, GatewayPrincipalID: gateway, LastSequence: sequence, Epochs: epochs, LastStreamReceivedAt: &now, UpdatedAt: now})
}
func (s *RevocationDistribution) Reconcile(ctx context.Context, c ReconcileRevocations) (RevocationReconciliation, error) {
	var out RevocationReconciliation
	if err := s.authorizeGateway(ctx, c.TenantID, c.GatewayPrincipalID, c.Roles); err != nil {
		return out, err
	}
	if c.LastSequence < 0 {
		return out, domain.NewError(domain.CodeInvalidArgument, "reconciliation cursor is invalid")
	}
	now, err := domain.RequireUTC(s.clock.Now())
	if err != nil {
		return out, err
	}
	snapshot, err := s.repository.RevocationSnapshot(ctx, c.TenantID, now)
	if err != nil {
		return out, err
	}
	out = RevocationReconciliation{Mode: "delta", AuthoritativeSequence: snapshot.Sequence, Epochs: snapshot.Epochs, ReconciledAt: now}
	if c.LastSequence > snapshot.Sequence || c.Epochs.Security > snapshot.Epochs.Security || c.Epochs.TenantPolicy > snapshot.Epochs.TenantPolicy || c.Epochs.TenantRevocation > snapshot.Epochs.TenantRevocation {
		return out, domain.NewError(domain.CodeConflict, "gateway state is ahead of authority")
	}
	changes, deltaErr := s.repository.RevocationChanges(ctx, c.TenantID, c.LastSequence, maxRevocationDelta+1, now.Add(-s.retention))
	if errors.Is(deltaErr, ports.ErrRevocationCursorGone) || len(changes) > maxRevocationDelta {
		out.Mode = "snapshot"
		out.Snapshot = snapshot.Active
		canonical := append([]domain.Revocation(nil), snapshot.Active...)
		sort.Slice(canonical, func(i, j int) bool { return canonical[i].ID.String() < canonical[j].ID.String() })
		encoded, _ := json.Marshal(canonical)
		sum := sha256.Sum256(encoded)
		out.SnapshotDigest = "sha256:" + hex.EncodeToString(sum[:])
	} else if deltaErr != nil {
		return out, deltaErr
	} else {
		out.Changes = changes
	}
	if err = s.repository.SaveGatewayCheckpoint(ctx, ports.GatewayCheckpoint{TenantID: c.TenantID, GatewayPrincipalID: c.GatewayPrincipalID, LastSequence: out.AuthoritativeSequence, Epochs: out.Epochs, LastReconciledAt: &now, UpdatedAt: now}); err != nil {
		return RevocationReconciliation{}, err
	}
	return out, nil
}
