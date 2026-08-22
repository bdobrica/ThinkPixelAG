package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/adapters/oidc"
	"github.com/bdobrica/ThinkPixelAG/internal/application"
	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/policy"
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
)

const runSignalRoute = "/v1/runs/{run_id}/signals"

type RunSignalApplication interface {
	Signal(context.Context, application.SignalRun) (domain.RunEvent, error)
}
type RunSignalHTTPConfig struct {
	SecurityState policy.SecurityState
	Lease, TTL    time.Duration
}
type signalRunBody struct {
	Type                 domain.RunSignalType       `json:"type"`
	Payload              map[string]json.RawMessage `json:"payload"`
	ExpectedStateVersion *int64                     `json:"expected_state_version,omitempty"`
}

func RunSignalHandler(verifier oidc.Verifier, service RunSignalApplication, idempotency ports.IdempotencyStore, clock domain.Clock, config RunSignalHTTPConfig) (http.Handler, error) {
	if verifier == nil || service == nil || idempotency == nil || clock == nil || config.Lease <= 0 || config.TTL < config.Lease {
		return nil, errors.New("run signal endpoint dependencies are invalid")
	}
	return AuthenticateBearer(verifier, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		principal, ok := PrincipalFromContext(request.Context())
		if !ok {
			writeProblem(writer, request, ProblemFromError(domain.NewError(domain.CodeUnauthenticated, "verified identity is required")))
			return
		}
		tenantID, tenantErr := domain.ParseID(principal.TenantID)
		principalID, principalErr := domain.ParseID(principal.ID)
		requestID, requestErr := domain.ParseID(requestIDFromContext(request.Context()))
		runID, runErr := domain.ParseID(request.PathValue("run_id"))
		if tenantErr != nil || principalErr != nil || requestErr != nil {
			writeProblem(writer, request, ProblemFromError(domain.NewError(domain.CodeUnauthenticated, "verified identity is invalid")))
			return
		}
		if runErr != nil {
			writeProblem(writer, request, ProblemFromError(domain.NewError(domain.CodeNotFound, "run not found")))
			return
		}
		key := request.Header.Get("Idempotency-Key")
		if len(request.Header.Values("Idempotency-Key")) != 1 || !idempotencyKeyPattern.MatchString(key) {
			writeProblem(writer, request, ProblemFromError(domain.NewError(domain.CodeInvalidArgument, "a valid Idempotency-Key header is required")))
			return
		}
		var body signalRunBody
		if err := DecodeJSON(request, &body); err != nil {
			writeProblem(writer, request, ProblemFromError(err))
			return
		}
		payload, err := json.Marshal(body.Payload)
		if err != nil {
			writeProblem(writer, request, ProblemFromError(domain.NewError(domain.CodeInvalidArgument, "run signal payload is invalid")))
			return
		}
		candidate := domain.RunSignal{ID: requestID, TenantID: tenantID, RunID: runID, ActorPrincipalID: principalID, Type: body.Type, Payload: payload, IdempotencyKey: key, ExpectedStateVersion: body.ExpectedStateVersion, CreatedAt: time.Unix(1, 0).UTC()}
		if err := candidate.Validate(); err != nil {
			writeProblem(writer, request, ProblemFromError(domain.WrapError(domain.CodeInvalidArgument, "run signal payload is invalid", err)))
			return
		}
		normalized, _ := json.Marshal(struct {
			RunID string        `json:"run_id"`
			Body  signalRunBody `json:"body"`
		}{runID.String(), body})
		now, err := domain.RequireUTC(clock.Now())
		if err != nil {
			writeProblem(writer, request, ProblemFromError(domain.NewError(domain.CodeInternal, "idempotency clock is invalid")))
			return
		}
		acquisition, err := idempotency.AcquireIdempotency(request.Context(), tenantID, ports.IdempotencyRequest{PrincipalID: principalID, Route: runSignalRoute, Key: key, RequestHash: hashRequest(normalized), Lease: config.Lease, TTL: config.TTL}, now)
		if err != nil {
			writeProblem(writer, request, ProblemFromError(err))
			return
		}
		if acquisition.Outcome == ports.IdempotencyReplay {
			replayRunResponse(writer, request, acquisition.Response)
			return
		}
		event, err := service.Signal(request.Context(), application.SignalRun{TenantID: tenantID, PrincipalID: principalID, RequestID: requestID, RunID: runID, Roles: principal.Roles, Issuer: principal.Issuer, IdempotencyKey: key, Type: body.Type, Payload: payload, ExpectedStateVersion: body.ExpectedStateVersion, SecurityState: config.SecurityState})
		if err != nil {
			_ = idempotency.FailIdempotency(request.Context(), tenantID, acquisition, clock.Now())
			writeProblem(writer, request, ProblemFromError(err))
			return
		}
		response := publicRunEvent(event)
		var encoded bytes.Buffer
		if encodeJSON(&encoded, response) != nil {
			_ = idempotency.FailIdempotency(request.Context(), tenantID, acquisition, clock.Now())
			writeProblem(writer, request, ProblemFromError(domain.NewError(domain.CodeInternal, "could not encode run signal response")))
			return
		}
		headers, _ := json.Marshal(map[string][]string{"Content-Type": {"application/json"}})
		if err := idempotency.CompleteIdempotency(request.Context(), tenantID, acquisition, ports.IdempotencyResponse{Status: http.StatusAccepted, Headers: headers, Body: encoded.Bytes()}, clock.Now()); err != nil {
			writeProblem(writer, request, ProblemFromError(domain.NewError(domain.CodeUnavailable, "could not establish idempotent response").WithRetryable()))
			return
		}
		writeJSON(writer, http.StatusAccepted, response)
	})), nil
}

func publicRunEvent(event domain.RunEvent) map[string]any {
	return map[string]any{"id": event.ID.String(), "run_id": event.RunID.String(), "sequence": event.Sequence, "type": event.Type, "data": event.Data, "occurred_at": event.OccurredAt}
}
