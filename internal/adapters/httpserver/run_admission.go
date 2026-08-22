package httpserver

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/adapters/oidc"
	"github.com/bdobrica/ThinkPixelAG/internal/application"
	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/policy"
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
)

const runAdmissionRoute = "/v1/agents/{agent_id}/runs"

var idempotencyKeyPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{16,128}$`)

type RunAdmissionService interface {
	Admit(context.Context, application.AdmitRun) (domain.RunAdmission, error)
}

type RunAdmissionHTTPConfig struct {
	AuthorityConstraints map[string]any
	SecurityState        policy.SecurityState
	Lease, TTL           time.Duration
}

type createRunBody struct {
	Objective              string                     `json:"objective"`
	Input                  map[string]json.RawMessage `json:"input,omitempty"`
	Constraints            requestedConstraints       `json:"constraints,omitempty"`
	RequestedVersionDigest string                     `json:"requested_version_digest,omitempty"`
}

type requestedConstraints struct {
	MaxExecutionTimeSeconds *int64 `json:"max_execution_time_seconds,omitempty"`
	MaxBudgetUSDMicrounits  *int64 `json:"max_budget_usd_microunits,omitempty"`
	MaxLLMTokens            *int64 `json:"max_llm_tokens,omitempty"`
	MaxToolCalls            *int64 `json:"max_tool_calls,omitempty"`
	MaxToolCallsPerMinute   *int64 `json:"max_tool_calls_per_minute,omitempty"`
	MaxActiveChildren       *int64 `json:"max_active_children,omitempty"`
	MaxTotalChildren        *int64 `json:"max_total_children,omitempty"`
	MaxDelegationDepth      *int64 `json:"max_delegation_depth,omitempty"`
}

func RunAdmissionHandler(verifier oidc.Verifier, service RunAdmissionService, idempotency ports.IdempotencyStore, clock domain.Clock, config RunAdmissionHTTPConfig) (http.Handler, error) {
	if verifier == nil || service == nil || idempotency == nil || clock == nil || config.AuthorityConstraints == nil || config.Lease <= 0 || config.TTL < config.Lease {
		return nil, errors.New("run admission endpoint dependencies are invalid")
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
		agentID, agentErr := domain.ParseID(request.PathValue("agent_id"))
		if tenantErr != nil || principalErr != nil {
			writeProblem(writer, request, ProblemFromError(domain.NewError(domain.CodeUnauthenticated, "authenticated identity is invalid")))
			return
		}
		if requestErr != nil {
			writeProblem(writer, request, ProblemFromError(domain.NewError(domain.CodeInternal, "request identifier is invalid")))
			return
		}
		if agentErr != nil {
			writeProblem(writer, request, ProblemFromError(domain.NewError(domain.CodeNotFound, "agent not found")))
			return
		}
		key := request.Header.Get("Idempotency-Key")
		if len(request.Header.Values("Idempotency-Key")) != 1 || !idempotencyKeyPattern.MatchString(key) {
			writeProblem(writer, request, ProblemFromError(domain.NewError(domain.CodeInvalidArgument, "a valid Idempotency-Key header is required")))
			return
		}
		var body createRunBody
		if err := DecodeJSON(request, &body); err != nil {
			writeProblem(writer, request, ProblemFromError(err))
			return
		}
		constraints, err := body.validate()
		if err != nil {
			writeProblem(writer, request, ProblemFromError(err))
			return
		}
		normalized, err := json.Marshal(struct {
			AgentID string        `json:"agent_id"`
			Body    createRunBody `json:"body"`
		}{agentID.String(), body})
		if err != nil {
			writeProblem(writer, request, ProblemFromError(domain.NewError(domain.CodeInternal, "could not normalize request")))
			return
		}
		now, err := domain.RequireUTC(clock.Now())
		if err != nil {
			writeProblem(writer, request, ProblemFromError(domain.NewError(domain.CodeInternal, "idempotency clock is invalid")))
			return
		}
		acquisition, err := idempotency.AcquireIdempotency(request.Context(), tenantID, ports.IdempotencyRequest{PrincipalID: principalID, Route: runAdmissionRoute, Key: key, RequestHash: hashRequest(normalized), Lease: config.Lease, TTL: config.TTL}, now)
		if err != nil {
			writeProblem(writer, request, ProblemFromError(err))
			return
		}
		if acquisition.Outcome == ports.IdempotencyReplay {
			replayRunResponse(writer, request, acquisition.Response)
			return
		}
		admission, err := service.Admit(request.Context(), application.AdmitRun{TenantID: tenantID, PrincipalID: principalID, AgentID: agentID, RequestID: requestID, Roles: principal.Roles, RequestedVersionDigest: body.RequestedVersionDigest, RequestedConstraints: constraints, AuthorityConstraints: cloneJSONMap(config.AuthorityConstraints), SecurityState: config.SecurityState})
		if err != nil {
			_ = idempotency.FailIdempotency(request.Context(), tenantID, acquisition, clock.Now())
			writeProblem(writer, request, ProblemFromError(err))
			return
		}
		response := publicRunAdmission(admission)
		var encoded bytes.Buffer
		if err := encodeJSON(&encoded, response); err != nil {
			_ = idempotency.FailIdempotency(request.Context(), tenantID, acquisition, clock.Now())
			writeProblem(writer, request, ProblemFromError(domain.NewError(domain.CodeInternal, "could not encode run response")))
			return
		}
		location := "/v1/runs/" + admission.RunID.String()
		headers, _ := json.Marshal(map[string][]string{"Content-Type": {"application/json"}, "Location": {location}})
		if err := idempotency.CompleteIdempotency(request.Context(), tenantID, acquisition, ports.IdempotencyResponse{Status: http.StatusCreated, Headers: headers, Body: encoded.Bytes()}, clock.Now()); err != nil {
			writeProblem(writer, request, ProblemFromError(domain.NewError(domain.CodeUnavailable, "could not establish idempotent response").WithRetryable()))
			return
		}
		writer.Header().Set("Location", location)
		writeJSON(writer, http.StatusCreated, response)
	})), nil
}

func (body createRunBody) validate() (map[string]any, error) {
	if len(body.Objective) < 1 || len(body.Objective) > 16384 || len(body.Input) > 100 || (body.RequestedVersionDigest != "" && !domain.ValidDigest(body.RequestedVersionDigest)) {
		return nil, domain.NewError(domain.CodeInvalidArgument, "run request is invalid or exceeds bounds")
	}
	values := map[string]any{}
	encoded, _ := json.Marshal(body.Constraints)
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	if err := decoder.Decode(&values); err != nil {
		return nil, domain.NewError(domain.CodeInvalidArgument, "run constraints are invalid")
	}
	for key, value := range values {
		number, ok := value.(json.Number)
		if !ok {
			continue
		}
		integer, err := number.Int64()
		if err != nil || integer < 0 || (key == "max_execution_time_seconds" && (integer < 1 || integer > 604800)) {
			return nil, domain.NewError(domain.CodeInvalidArgument, "run constraints are outside allowed bounds")
		}
	}
	return values, nil
}

func publicRunAdmission(a domain.RunAdmission) map[string]any {
	// Resource dimensions and grants are introduced in Phase 5. The admission
	// endpoint exposes the versioned envelope header now without inventing
	// accounting quantities from policy constraints.
	envelope := map[string]any{"version": int64(1), "resources": []any{}}
	if a.DeadlineAt != nil {
		envelope["run_deadline"] = a.DeadlineAt
	}
	return map[string]any{"id": a.RunID.String(), "agent_id": a.AgentID.String(), "version_digest": a.AgentVersionDigest, "state": string(a.State), "state_version": a.StateVersion, "envelope": envelope, "created_at": a.CreatedAt, "updated_at": a.UpdatedAt}
}

func replayRunResponse(writer http.ResponseWriter, request *http.Request, response *ports.IdempotencyResponse) {
	if response == nil {
		writeProblem(writer, request, ProblemFromError(domain.NewError(domain.CodeInternal, "stored idempotency response is invalid")))
		return
	}
	var headers map[string][]string
	if json.Unmarshal(response.Headers, &headers) != nil {
		writeProblem(writer, request, ProblemFromError(domain.NewError(domain.CodeInternal, "stored idempotency response is invalid")))
		return
	}
	for _, name := range []string{"Content-Type", "Location"} {
		for _, value := range headers[name] {
			writer.Header().Add(name, value)
		}
	}
	writer.WriteHeader(response.Status)
	_, _ = writer.Write(response.Body)
}

func hashRequest(value []byte) string { return "sha256:" + fmt.Sprintf("%x", sha256.Sum256(value)) }

func cloneJSONMap(value map[string]any) map[string]any {
	encoded, _ := json.Marshal(value)
	var cloned map[string]any
	_ = json.Unmarshal(encoded, &cloned)
	return cloned
}
