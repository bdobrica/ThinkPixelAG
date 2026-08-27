package application

import (
	"context"
	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
	"testing"
	"time"
)

type distributionRepoStub struct {
	snapshot   ports.RevocationSnapshot
	changes    []ports.RevocationLogEntry
	changeErr  error
	checkpoint ports.GatewayCheckpoint
}

func (s *distributionRepoStub) RevocationChanges(context.Context, domain.ID, int64, int, time.Time) ([]ports.RevocationLogEntry, error) {
	return s.changes, s.changeErr
}
func (s *distributionRepoStub) RevocationSnapshot(context.Context, domain.ID, time.Time) (ports.RevocationSnapshot, error) {
	return s.snapshot, nil
}
func (s *distributionRepoStub) SaveGatewayCheckpoint(_ context.Context, c ports.GatewayCheckpoint) error {
	s.checkpoint = c
	return nil
}
func TestRevocationReconcileFallsBackToSnapshotAndPersistsCheckpoint(t *testing.T) {
	tenant, _ := domain.NewID()
	gateway, _ := domain.NewID()
	rev, _ := domain.NewID()
	actor, _ := domain.NewID()
	now := time.Now().UTC()
	repo := &distributionRepoStub{snapshot: ports.RevocationSnapshot{Sequence: 12, Epochs: domain.EpochVector{Security: 8, TenantRevocation: 4}, Active: []domain.Revocation{{ID: rev, TenantID: &tenant, ActorPrincipalID: actor, Scope: domain.RevocationToolID, Target: "shell", ReasonCode: "security.compromise", EffectiveAt: now, CreatedAt: now}}}, changeErr: ports.ErrRevocationCursorGone}
	service, _ := NewRevocationDistribution(repo, fixedClock{now: now}, time.Hour)
	got, err := service.Reconcile(context.Background(), ReconcileRevocations{TenantID: tenant, GatewayPrincipalID: gateway, Roles: []string{"gateway"}, LastSequence: 1})
	if err != nil || got.Mode != "snapshot" || got.SnapshotDigest == "" || len(got.Snapshot) != 1 || repo.checkpoint.LastSequence != 12 || repo.checkpoint.LastReconciledAt == nil {
		t.Fatalf("result=%+v checkpoint=%+v err=%v", got, repo.checkpoint, err)
	}
}
