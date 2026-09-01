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
)

type settlementHTTPStub struct{ command application.SettleReservation }

func (s *settlementHTTPStub) Settle(_ context.Context, c application.SettleReservation) (domain.ResourceSettlementResult, error) {
	s.command = c
	return domain.ResourceSettlementResult{ID: c.ReservationID, ReservationID: c.ReservationID, SettledAt: time.Unix(1, 0).UTC()}, nil
}
func TestResourceSettlementBindsAuthenticatedActor(t *testing.T) {
	tenant, _ := domain.NewID()
	actor, _ := domain.NewID()
	reservation, _ := domain.NewID()
	requestID, _ := domain.NewID()
	service := &settlementHTTPStub{}
	handler, _ := ResourceSettlementHandler(&fakeVerifier{principal: oidc.Principal{ID: actor.String(), TenantID: tenant.String(), Roles: []string{"trusted-settler"}}}, service, ResourceSettlementHTTPConfig{})
	req := httptest.NewRequest(http.MethodPost, "/v1/trusted/reservations/"+reservation.String()+"/settle", strings.NewReader(`{"terminal_run_state":"COMPLETED"}`))
	req.SetPathValue("reservation_id", reservation.String())
	req.Header.Set("Idempotency-Key", "settle-1")
	req = req.WithContext(context.WithValue(req.Context(), requestIDKey{}, requestID.String()))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK || service.command.PrincipalID != actor || service.command.TenantID != tenant || service.command.IdempotencyKey != "settle-1" {
		t.Fatalf("status=%d command=%+v body=%s", rr.Code, service.command, rr.Body.String())
	}
}
