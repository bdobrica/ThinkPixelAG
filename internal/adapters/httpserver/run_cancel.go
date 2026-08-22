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

const runCancelRoute = "/v1/runs/{run_id}/cancel"

type RunCancellationApplication interface {
	Cancel(context.Context, application.CancelRun) (domain.Run, error)
}

type RunCancellationHTTPConfig struct {
	SecurityState policy.SecurityState
	Lease, TTL    time.Duration
}

type cancelRunBody struct {
	ReasonCode           string `json:"reason_code,omitempty"`
	ExpectedStateVersion *int64 `json:"expected_state_version,omitempty"`
}

func RunCancellationHandler(verifier oidc.Verifier, service RunCancellationApplication, idempotency ports.IdempotencyStore, clock domain.Clock, config RunCancellationHTTPConfig) (http.Handler, error) {
	if verifier == nil || service == nil || idempotency == nil || clock == nil || config.Lease <= 0 || config.TTL < config.Lease {
		return nil, errors.New("run cancellation endpoint dependencies are invalid")
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
		var body cancelRunBody
		if err := DecodeJSON(request, &body); err != nil {
			writeProblem(writer, request, ProblemFromError(err))
			return
		}
		candidate := domain.RunCancellation{TenantID: tenantID, RunID: runID, ActorPrincipalID: principalID, IdempotencyKey: "cancel:" + key, ReasonCode: body.ReasonCode, ExpectedStateVersion: body.ExpectedStateVersion, CreatedAt: time.Unix(1, 0).UTC()}
		if err := candidate.Validate(); err != nil {
			writeProblem(writer, request, ProblemFromError(domain.WrapError(domain.CodeInvalidArgument, "run cancellation is invalid", err)))
			return
		}
		normalized, err := json.Marshal(struct {
			RunID string        `json:"run_id"`
			Body  cancelRunBody `json:"body"`
		}{runID.String(), body})
		if err != nil {
			writeProblem(writer, request, ProblemFromError(domain.NewError(domain.CodeInternal, "could not normalize cancellation request")))
			return
		}
		now, err := domain.RequireUTC(clock.Now())
		if err != nil {
			writeProblem(writer, request, ProblemFromError(domain.NewError(domain.CodeInternal, "idempotency clock is invalid")))
			return
		}
		acquisition, err := idempotency.AcquireIdempotency(request.Context(), tenantID, ports.IdempotencyRequest{PrincipalID: principalID, Route: runCancelRoute, Key: key, RequestHash: hashRequest(normalized), Lease: config.Lease, TTL: config.TTL}, now)
		if err != nil {
			writeProblem(writer, request, ProblemFromError(err))
			return
		}
		if acquisition.Outcome == ports.IdempotencyReplay {
			replayRunResponse(writer, request, acquisition.Response)
			return
		}
		run, err := service.Cancel(request.Context(), application.CancelRun{TenantID: tenantID, PrincipalID: principalID, RequestID: requestID, RunID: runID, Roles: principal.Roles, Issuer: principal.Issuer, IdempotencyKey: key, ReasonCode: body.ReasonCode, ExpectedStateVersion: body.ExpectedStateVersion, SecurityState: config.SecurityState})
		if err != nil {
			_ = idempotency.FailIdempotency(request.Context(), tenantID, acquisition, clock.Now())
			writeProblem(writer, request, ProblemFromError(err))
			return
		}
		response := publicRun(run)
		var encoded bytes.Buffer
		if encodeJSON(&encoded, response) != nil {
			_ = idempotency.FailIdempotency(request.Context(), tenantID, acquisition, clock.Now())
			writeProblem(writer, request, ProblemFromError(domain.NewError(domain.CodeInternal, "could not encode run cancellation response")))
			return
		}
		headers, _ := json.Marshal(map[string][]string{"Content-Type": {"application/json"}})
		if err := idempotency.CompleteIdempotency(request.Context(), tenantID, acquisition, ports.IdempotencyResponse{Status: http.StatusOK, Headers: headers, Body: encoded.Bytes()}, clock.Now()); err != nil {
			writeProblem(writer, request, ProblemFromError(domain.NewError(domain.CodeUnavailable, "could not establish idempotent response").WithRetryable()))
			return
		}
		writeJSON(writer, http.StatusOK, response)
	})), nil
}
