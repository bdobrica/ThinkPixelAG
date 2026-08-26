package httpserver

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/adapters/oidc"
	"github.com/bdobrica/ThinkPixelAG/internal/application"
	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/policy"
)

type TrustedUsageApplication interface {
	Record(context.Context, application.RecordTrustedUsage) (domain.UsageReceipt, error)
}

type TrustedUsageHTTPConfig struct{ SecurityState policy.SecurityState }

type trustedUsageBody struct {
	ProducerID    string    `json:"producer_id"`
	SourceEventID string    `json:"source_event_id"`
	ResourceName  string    `json:"resource_name"`
	Unit          string    `json:"unit"`
	Quantity      int64     `json:"quantity"`
	ObservedAt    time.Time `json:"observed_at"`
}

func TrustedUsageHandler(verifier oidc.Verifier, service TrustedUsageApplication, config TrustedUsageHTTPConfig) (http.Handler, error) {
	if verifier == nil || service == nil {
		return nil, errors.New("trusted usage endpoint dependencies are invalid")
	}
	return AuthenticateBearer(verifier, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		principal, ok := PrincipalFromContext(r.Context())
		if !ok {
			writeProblem(w, r, ProblemFromError(domain.NewError(domain.CodeUnauthenticated, "verified identity is required")))
			return
		}
		tenantID, tenantErr := domain.ParseID(principal.TenantID)
		producerID, producerErr := domain.ParseID(principal.ID)
		requestID, requestErr := domain.ParseID(requestIDFromContext(r.Context()))
		runID, runErr := domain.ParseID(r.PathValue("run_id"))
		if tenantErr != nil || producerErr != nil || requestErr != nil {
			writeProblem(w, r, ProblemFromError(domain.NewError(domain.CodeUnauthenticated, "verified identity is invalid")))
			return
		}
		if runErr != nil {
			writeProblem(w, r, ProblemFromError(domain.NewError(domain.CodeNotFound, "run not found")))
			return
		}
		var body trustedUsageBody
		if err := DecodeJSON(r, &body); err != nil {
			writeProblem(w, r, ProblemFromError(err))
			return
		}
		claimed, err := domain.ParseID(body.ProducerID)
		if err != nil || claimed != producerID {
			writeProblem(w, r, ProblemFromError(domain.NewError(domain.CodeForbidden, "producer identity does not match authenticated identity")))
			return
		}
		receipt, err := service.Record(r.Context(), application.RecordTrustedUsage{TenantID: tenantID, ProducerID: producerID, RequestID: requestID, RunID: runID, Roles: principal.Roles, Issuer: principal.Issuer, SourceEventID: body.SourceEventID, ResourceName: body.ResourceName, Unit: body.Unit, Quantity: body.Quantity, ObservedAt: body.ObservedAt, SecurityState: config.SecurityState})
		if err != nil {
			writeProblem(w, r, ProblemFromError(err))
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"usage_id": receipt.UsageID.String(), "duplicate": receipt.Duplicate, "accepted_at": receipt.AcceptedAt})
	})), nil
}
