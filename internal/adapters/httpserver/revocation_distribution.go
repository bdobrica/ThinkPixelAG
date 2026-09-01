package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/application"
	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
)

const revocationStreamBatch = 256

type RevocationDistributionService interface {
	Changes(context.Context, domain.ID, domain.ID, []string, int64, int) ([]ports.RevocationLogEntry, error)
	CheckpointStream(context.Context, domain.ID, domain.ID, []string, int64, domain.EpochVector) error
	Reconcile(context.Context, application.ReconcileRevocations) (application.RevocationReconciliation, error)
}
type RevocationStreamOptions struct{ HeartbeatInterval, PollInterval, WriteTimeout time.Duration }

func RevocationDistributionHandler(verifier WorkloadVerifier, service RevocationDistributionService, codec *domain.RevocationCursorCodec, options RevocationStreamOptions) (http.Handler, error) {
	if verifier == nil || service == nil || codec == nil || options.HeartbeatInterval <= 0 || options.PollInterval <= 0 || options.WriteTimeout <= 0 {
		return nil, errors.New("revocation distribution endpoint dependencies are unavailable")
	}
	return AuthenticateWorkload(verifier, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, ok := PrincipalFromContext(r.Context())
		if !ok {
			writeProblem(w, r, ProblemFromError(domain.NewError(domain.CodeUnauthenticated, "verified identity is required")))
			return
		}
		tenant, e1 := domain.ParseID(p.TenantID)
		gateway, e2 := domain.ParseID(p.ID)
		if e1 != nil || e2 != nil {
			writeProblem(w, r, ProblemFromError(domain.NewError(domain.CodeUnauthenticated, "verified identity is invalid")))
			return
		}
		if r.URL.Path == "/v1/trusted/revocations/reconcile" {
			handleRevocationReconcile(w, r, service, tenant, gateway, p.Roles)
			return
		}
		cursorValue := r.URL.Query().Get("after")
		if h := r.Header.Get("Last-Event-ID"); h != "" {
			if cursorValue != "" && cursorValue != h {
				writeProblem(w, r, ProblemFromError(domain.NewError(domain.CodeInvalidArgument, "revocation cursors disagree")))
				return
			}
			cursorValue = h
		}
		cursor := domain.RevocationCursor{}
		var err error
		if cursorValue != "" {
			cursor, err = codec.Decode(cursorValue, tenant)
		} else if raw := r.URL.Query().Get("after_sequence"); raw != "" {
			cursor.Sequence, err = strconv.ParseInt(raw, 10, 64)
		}
		if err != nil || cursor.Sequence < 0 {
			writeProblem(w, r, ProblemFromError(domain.NewError(domain.CodeInvalidArgument, "revocation cursor is invalid")))
			return
		}
		events, err := service.Changes(r.Context(), tenant, gateway, p.Roles, cursor.Sequence, revocationStreamBatch)
		if errors.Is(err, ports.ErrRevocationCursorGone) {
			writeProblem(w, r, problemFor(http.StatusGone, "cursor_gone", "Cursor Gone", "The revocation cursor is outside retention; reconcile authoritative state."))
			return
		}
		if err != nil {
			writeProblem(w, r, ProblemFromError(err))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache, no-store")
		w.Header().Set("X-Accel-Buffering", "no")
		w.WriteHeader(http.StatusOK)
		controller := http.NewResponseController(w)
		poll := time.NewTicker(options.PollInterval)
		defer poll.Stop()
		heartbeat := time.NewTicker(options.HeartbeatInterval)
		defer heartbeat.Stop()
		for {
			for _, event := range events {
				encoded, next, encodeErr := encodeRevocationEvent(codec, tenant, event)
				if encodeErr != nil {
					return
				}
				_ = controller.SetWriteDeadline(time.Now().Add(options.WriteTimeout))
				if _, err = w.Write(encoded); err != nil {
					return
				}
				if err = controller.Flush(); err != nil {
					return
				}
				cursor = next
				if err = service.CheckpointStream(r.Context(), tenant, gateway, p.Roles, cursor.Sequence, event.Epochs); err != nil {
					return
				}
			}
			select {
			case <-r.Context().Done():
				return
			case <-heartbeat.C:
				_ = controller.SetWriteDeadline(time.Now().Add(options.WriteTimeout))
				if _, err = fmt.Fprint(w, ": heartbeat\n\n"); err != nil {
					return
				}
				if controller.Flush() != nil {
					return
				}
			case <-poll.C:
			}
			events, err = service.Changes(r.Context(), tenant, gateway, p.Roles, cursor.Sequence, revocationStreamBatch)
			if err != nil {
				_, _ = fmt.Fprint(w, "event: gap\ndata: {\"reconcile_required\":true}\n\n")
				_ = controller.Flush()
				return
			}
		}
	})), nil
}
func encodeRevocationEvent(codec *domain.RevocationCursorCodec, tenant domain.ID, e ports.RevocationLogEntry) ([]byte, domain.RevocationCursor, error) {
	c := domain.RevocationCursor{Sequence: e.Sequence, SecurityEpoch: e.Epochs.Security}
	id, err := codec.Encode(tenant, c)
	if err != nil {
		return nil, c, err
	}
	data, err := json.Marshal(revocationLogJSON(e))
	if err != nil {
		return nil, c, err
	}
	var b bytes.Buffer
	fmt.Fprintf(&b, "id: %s\nevent: revocation.%s\ndata: %s\n\n", id, e.Change, data)
	return b.Bytes(), c, nil
}
func revocationLogJSON(e ports.RevocationLogEntry) map[string]any {
	return map[string]any{"event_id": e.EventID.String(), "sequence": e.Sequence, "change": e.Change, "revocation": revocationJSON(e.Revocation, e.Change, e.Epochs), "epochs": e.Epochs, "occurred_at": e.OccurredAt}
}
func revocationJSON(v domain.Revocation, state domain.RevocationChangeType, epochs domain.EpochVector) map[string]any {
	m := map[string]any{"id": v.ID.String(), "scope": v.Scope, "target": v.Target, "state": state, "reason_code": v.ReasonCode, "epochs": epochs, "effective_at": v.EffectiveAt, "created_at": v.CreatedAt}
	if v.ExpiresAt != nil {
		m["expires_at"] = v.ExpiresAt
	}
	return m
}
func handleRevocationReconcile(w http.ResponseWriter, r *http.Request, s RevocationDistributionService, tenant, gateway domain.ID, roles []string) {
	var body struct {
		GatewayID    string             `json:"gateway_id"`
		LastSequence int64              `json:"last_sequence"`
		Epochs       domain.EpochVector `json:"epochs"`
	}
	if err := DecodeJSON(r, &body); err != nil {
		writeProblem(w, r, ProblemFromError(err))
		return
	}
	if body.GatewayID != "" && body.GatewayID != gateway.String() {
		writeProblem(w, r, ProblemFromError(domain.NewError(domain.CodeForbidden, "gateway identity does not match authenticated principal")))
		return
	}
	result, err := s.Reconcile(r.Context(), application.ReconcileRevocations{TenantID: tenant, GatewayPrincipalID: gateway, Roles: roles, LastSequence: body.LastSequence, Epochs: body.Epochs})
	if err != nil {
		writeProblem(w, r, ProblemFromError(err))
		return
	}
	response := map[string]any{"mode": result.Mode, "authoritative_sequence": result.AuthoritativeSequence, "epochs": result.Epochs, "reconciled_at": result.ReconciledAt}
	if result.SnapshotDigest != "" {
		response["snapshot_digest"] = result.SnapshotDigest
	}
	if result.Snapshot != nil {
		items := make([]map[string]any, 0, len(result.Snapshot))
		for _, v := range result.Snapshot {
			items = append(items, revocationJSON(v, domain.RevocationCreated, result.Epochs))
		}
		response["snapshot"] = items
	}
	if result.Changes != nil {
		items := make([]map[string]any, 0, len(result.Changes))
		for _, v := range result.Changes {
			items = append(items, revocationLogJSON(v))
		}
		response["changes"] = items
	}
	writeJSON(w, http.StatusOK, response)
}
