package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/adapters/oidc"
	"github.com/bdobrica/ThinkPixelAG/internal/application"
	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/policy"
)

const runEventBatchSize = 128

type RunEventStreamService interface {
	Authorize(context.Context, application.GetRun) error
	Events(context.Context, domain.ID, int64, int) ([]domain.RunEvent, error)
}

type RunEventStreamOptions struct {
	HeartbeatInterval time.Duration
	PollInterval      time.Duration
	WriteTimeout      time.Duration
}

func RunEventStreamHandler(verifier oidc.Verifier, service RunEventStreamService, codec *domain.RunEventCursorCodec, securityState policy.SecurityState, options RunEventStreamOptions) (http.Handler, error) {
	if verifier == nil || service == nil || codec == nil || options.HeartbeatInterval <= 0 || options.PollInterval <= 0 || options.WriteTimeout <= 0 {
		return nil, domain.NewError(domain.CodeInternal, "run event endpoint dependencies are unavailable")
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
		cursor := request.URL.Query().Get("after")
		lastEventID := request.Header.Get("Last-Event-ID")
		if cursor != "" && lastEventID != "" && cursor != lastEventID {
			writeProblem(writer, request, ProblemFromError(domain.NewError(domain.CodeInvalidArgument, "event cursors disagree")))
			return
		}
		if cursor == "" {
			cursor = lastEventID
		}
		after := int64(0)
		var err error
		if cursor != "" {
			after, err = codec.Decode(cursor, runID)
			if err != nil {
				writeProblem(writer, request, ProblemFromError(domain.NewError(domain.CodeInvalidArgument, "event cursor is invalid")))
				return
			}
		}
		command := application.GetRun{TenantID: tenantID, PrincipalID: principalID, RequestID: requestID, RunID: runID, Roles: principal.Roles, Issuer: principal.Issuer, SecurityState: securityState}
		if err := service.Authorize(request.Context(), command); err != nil {
			writeProblem(writer, request, ProblemFromError(err))
			return
		}
		events, err := service.Events(request.Context(), runID, after, runEventBatchSize)
		if errors.Is(err, domain.ErrRunEventCursorGone) {
			writeProblem(writer, request, problemFor(http.StatusGone, "cursor_gone", "Cursor Gone", "The event cursor is outside retention; refetch authoritative run state."))
			return
		}
		if err != nil {
			writeProblem(writer, request, ProblemFromError(err))
			return
		}
		writer.Header().Set("Content-Type", "text/event-stream")
		writer.Header().Set("Cache-Control", "no-cache, no-store")
		writer.Header().Set("Connection", "keep-alive")
		writer.Header().Set("X-Accel-Buffering", "no")
		writer.WriteHeader(http.StatusOK)
		controller := http.NewResponseController(writer)
		poll := time.NewTicker(options.PollInterval)
		defer poll.Stop()
		heartbeat := time.NewTicker(options.HeartbeatInterval)
		defer heartbeat.Stop()
		for {
			for _, event := range events {
				encoded, encodeErr := encodeSSEEvent(codec, event)
				if encodeErr != nil {
					return
				}
				_ = controller.SetWriteDeadline(time.Now().Add(options.WriteTimeout))
				if _, err = writer.Write(encoded); err != nil {
					return
				}
				if err = controller.Flush(); err != nil {
					return
				}
				after = event.Sequence
			}
			select {
			case <-request.Context().Done():
				return
			case <-heartbeat.C:
				_ = controller.SetWriteDeadline(time.Now().Add(options.WriteTimeout))
				if _, err = fmt.Fprint(writer, ": heartbeat\n\n"); err != nil {
					return
				}
				if err = controller.Flush(); err != nil {
					return
				}
			case <-poll.C:
			}
			events, err = service.Events(request.Context(), runID, after, runEventBatchSize)
			if err != nil {
				return
			} // errors after headers close the stream; clients resume from the last emitted cursor.
		}
	})), nil
}

func encodeSSEEvent(codec *domain.RunEventCursorCodec, event domain.RunEvent) ([]byte, error) {
	cursor, err := codec.Encode(event.RunID, event.Sequence)
	if err != nil {
		return nil, err
	}
	data, err := json.Marshal(publicRunEvent(event))
	if err != nil {
		return nil, err
	}
	var result bytes.Buffer
	fmt.Fprintf(&result, "id: %s\nevent: %s\ndata: %s\n\n", cursor, event.Type, data)
	return result.Bytes(), nil
}
