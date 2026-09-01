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

type usageHTTPStub struct {
	command application.RecordTrustedUsage
	calls   int
}

func (s *usageHTTPStub) Record(_ context.Context, c application.RecordTrustedUsage) (domain.UsageReceipt, error) {
	s.calls++
	s.command = c
	id, _ := domain.NewID()
	return domain.UsageReceipt{UsageID: id, AcceptedAt: time.Unix(2, 0).UTC()}, nil
}

func TestTrustedUsageBindsProducerToAuthenticatedIdentity(t *testing.T) {
	tenant, producer, runID := mustHTTPID(t), mustHTTPID(t), mustHTTPID(t)
	service := &usageHTTPStub{}
	handler, _ := TrustedUsageHandler(&fakeVerifier{principal: oidc.Principal{ID: producer.String(), TenantID: tenant.String(), Roles: []string{"trusted-meter"}}}, service, TrustedUsageHTTPConfig{})
	body := `{"producer_id":"` + producer.String() + `","source_event_id":"evt-1","resource_name":"llm_tokens","unit":"llm_tokens","quantity":2,"observed_at":"2026-08-26T10:00:00Z"}`
	request := httptest.NewRequest(http.MethodPost, "/v1/trusted/runs/"+runID.String()+"/usage", strings.NewReader(body))
	request.SetPathValue("run_id", runID.String())
	request = request.WithContext(context.WithValue(request.Context(), requestIDKey{}, mustHTTPID(t).String()))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusAccepted || service.calls != 1 || service.command.ProducerID != producer || service.command.Quantity != 2 {
		t.Fatalf("status=%d calls=%d body=%s", response.Code, service.calls, response.Body.String())
	}
	other := mustHTTPID(t)
	request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(strings.Replace(body, producer.String(), other.String(), 1)))
	request.SetPathValue("run_id", runID.String())
	request = request.WithContext(context.WithValue(request.Context(), requestIDKey{}, mustHTTPID(t).String()))
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || service.calls != 1 {
		t.Fatalf("spoof status=%d calls=%d", response.Code, service.calls)
	}
}
