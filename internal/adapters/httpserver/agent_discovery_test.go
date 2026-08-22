package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/adapters/oidc"
	"github.com/bdobrica/ThinkPixelAG/internal/application"
	"github.com/bdobrica/ThinkPixelAG/internal/domain"
)

type discoveryServiceStub struct {
	page    application.AgentPage
	agent   domain.Agent
	err     error
	command application.DiscoverAgents
}

func (s *discoveryServiceStub) List(_ context.Context, c application.DiscoverAgents) (application.AgentPage, error) {
	s.command = c
	return s.page, s.err
}
func (s *discoveryServiceStub) Describe(_ context.Context, c application.DiscoverAgents, _ domain.ID) (domain.Agent, error) {
	s.command = c
	return s.agent, s.err
}

func mustHTTPID(t *testing.T) domain.ID {
	t.Helper()
	id, err := domain.NewID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestAgentDiscoveryHTTPListCursorAndVerifiedAuthority(t *testing.T) {
	tenant, principal, agentID, owner, sponsor := mustHTTPID(t), mustHTTPID(t), mustHTTPID(t), mustHTTPID(t), mustHTTPID(t)
	now := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
	agent := domain.Agent{ID: agentID, TenantID: tenant, Name: "reviewer", Description: "safe", OwnerPrincipalID: owner, SponsorPrincipalID: sponsor, RiskClass: domain.AgentRiskMedium, Status: domain.AgentActive, CreatedAt: now, UpdatedAt: now}
	service := &discoveryServiceStub{page: application.AgentPage{Items: []domain.Agent{agent}, Next: agentID}}
	codec, _ := domain.NewCursorCodec(bytes.Repeat([]byte{7}, 32))
	verifier := &fakeVerifier{principal: oidc.Principal{ID: principal.String(), TenantID: tenant.String(), Issuer: "https://issuer.example", Roles: []string{"agent-invoker"}}}
	discovery, err := AgentDiscoveryHandler(verifier, service, codec)
	if err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	deps := testDependencies(t, &logs, false)
	deps.AgentDiscovery = discovery
	handler := newHandler(testConfig(), deps, &Readiness{})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/v1/agents?page_size=1", nil)
	request.Header.Set("Authorization", "Bearer valid")
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || service.command.TenantID != tenant || service.command.PrincipalID != principal || service.command.Issuer != verifier.principal.Issuer || service.command.Limit != 1 {
		t.Fatalf("response=%d %s command=%+v", recorder.Code, recorder.Body.String(), service.command)
	}
	var body struct {
		Items []map[string]any `json:"items"`
		Next  string           `json:"next_cursor"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil || len(body.Items) != 1 || body.Next == "" {
		t.Fatalf("body=%s err=%v", recorder.Body.String(), err)
	}
	cursor, err := codec.Decode(body.Next)
	if err != nil || cursor.ID != agentID || cursor.SortKey != agentCursorSortKey {
		t.Fatalf("cursor=%+v err=%v", cursor, err)
	}

	recorder = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, "/v1/agents?cursor="+body.Next+"x", nil)
	request.Header.Set("Authorization", "Bearer valid")
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("tampered cursor=%d %s", recorder.Code, recorder.Body.String())
	}
}

func TestAgentDiscoveryHTTPEnumerationSafeDescribe(t *testing.T) {
	tenant, principal, agentID := mustHTTPID(t), mustHTTPID(t), mustHTTPID(t)
	service := &discoveryServiceStub{err: domain.NewError(domain.CodeNotFound, "agent not found")}
	codec, _ := domain.NewCursorCodec(bytes.Repeat([]byte{9}, 32))
	discovery, _ := AgentDiscoveryHandler(&fakeVerifier{principal: oidc.Principal{ID: principal.String(), TenantID: tenant.String()}}, service, codec)
	var logs bytes.Buffer
	deps := testDependencies(t, &logs, false)
	deps.AgentDiscovery = discovery
	handler := newHandler(testConfig(), deps, &Readiness{})
	responses := make([]string, 0, 2)
	for _, id := range []string{agentID.String(), "not-a-valid-id"} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/v1/agents/"+id, nil)
		request.Header.Set("Authorization", "Bearer valid")
		handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusNotFound {
			t.Fatalf("describe %q=%d %s", id, recorder.Code, recorder.Body.String())
		}
		responses = append(responses, strings.ReplaceAll(recorder.Body.String(), fixedRequestID, "request"))
	}
	if responses[0] != responses[1] {
		t.Fatalf("enumeration responses differ: %q %q", responses[0], responses[1])
	}
}
