package httpserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/adapters/oidc"
	"github.com/bdobrica/ThinkPixelAG/internal/application"
	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
)

type revocationDistributionStub struct {
	event      ports.RevocationLogEntry
	cancel     context.CancelFunc
	checkpoint int64
	changeErr  error
}

func (s *revocationDistributionStub) Changes(context.Context, domain.ID, domain.ID, []string, int64, int) ([]ports.RevocationLogEntry, error) {
	if s.changeErr != nil {
		return nil, s.changeErr
	}
	if s.checkpoint > 0 {
		return nil, nil
	}
	return []ports.RevocationLogEntry{s.event}, nil
}
func (s *revocationDistributionStub) CheckpointStream(_ context.Context, _ domain.ID, _ domain.ID, _ []string, sequence int64, _ domain.EpochVector) error {
	s.checkpoint = sequence
	s.cancel()
	return nil
}
func (s *revocationDistributionStub) Reconcile(context.Context, application.ReconcileRevocations) (application.RevocationReconciliation, error) {
	return application.RevocationReconciliation{}, nil
}

func TestRevocationSSEEmitsAuthenticatedCursorAndPersistsReceipt(t *testing.T) {
	tenant, _ := domain.NewID()
	gateway, _ := domain.NewID()
	eventID, _ := domain.NewID()
	revID, _ := domain.NewID()
	actor, _ := domain.NewID()
	now := time.Now().UTC()
	ctx, cancel := context.WithCancel(context.Background())
	stub := &revocationDistributionStub{cancel: cancel, event: ports.RevocationLogEntry{EventID: eventID, Sequence: 5, Change: domain.RevocationCreated, Epochs: domain.EpochVector{Security: 3}, OccurredAt: now, Revocation: domain.Revocation{ID: revID, TenantID: &tenant, ActorPrincipalID: actor, Scope: domain.RevocationRunID, Target: revID.String(), ReasonCode: "security.compromise", EffectiveAt: now, CreatedAt: now}}}
	codec, _ := domain.NewRevocationCursorCodec([]byte("01234567890123456789012345678901"))
	handler, _ := RevocationDistributionHandler(&fakeVerifier{principal: oidc.Principal{ID: gateway.String(), TenantID: tenant.String(), Roles: []string{"gateway"}}}, stub, codec, RevocationStreamOptions{HeartbeatInterval: time.Second, PollInterval: time.Millisecond, WriteTimeout: time.Second})
	req := httptest.NewRequest(http.MethodGet, "/v1/trusted/revocations/events", nil).WithContext(ctx)
	req.Header.Set("Authorization", "Bearer token")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	if response.Code != http.StatusOK || stub.checkpoint != 5 || !strings.Contains(response.Body.String(), "event: revocation.CREATED") || !strings.Contains(response.Body.String(), "security_epoch") {
		t.Fatalf("status=%d checkpoint=%d body=%s", response.Code, stub.checkpoint, response.Body.String())
	}
}

func TestRevocationSSESignalsExpiredCursor(t *testing.T) {
	tenant, _ := domain.NewID()
	gateway, _ := domain.NewID()
	stub := &revocationDistributionStub{changeErr: ports.ErrRevocationCursorGone}
	codec, _ := domain.NewRevocationCursorCodec([]byte("01234567890123456789012345678901"))
	handler, _ := RevocationDistributionHandler(&fakeVerifier{principal: oidc.Principal{ID: gateway.String(), TenantID: tenant.String(), Roles: []string{"gateway"}}}, stub, codec, RevocationStreamOptions{HeartbeatInterval: time.Second, PollInterval: time.Second, WriteTimeout: time.Second})
	req := httptest.NewRequest(http.MethodGet, "/v1/trusted/revocations/events", nil)
	req.Header.Set("Authorization", "Bearer token")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	if response.Code != http.StatusGone {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}
