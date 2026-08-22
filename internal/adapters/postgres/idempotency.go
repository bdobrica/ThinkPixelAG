package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
	"github.com/jackc/pgx/v5"
)

var (
	ErrIdempotencyConflict  = errors.New("idempotency key reused with a different request")
	ErrIdempotencyInFlight  = errors.New("idempotent request is already in progress")
	ErrIdempotencyOwnership = errors.New("idempotency ownership lost")
	requestHashPattern      = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

type IdempotencyOutcome = ports.IdempotencyOutcome
type IdempotencyRequest = ports.IdempotencyRequest
type IdempotencyResponse = ports.IdempotencyResponse
type IdempotencyAcquisition = ports.IdempotencyAcquisition

const IdempotencyAcquired = ports.IdempotencyAcquired
const IdempotencyReplay = ports.IdempotencyReplay

// HashIdempotencyRequest hashes a caller-normalized request representation.
// Normalization is transport-specific; this helper deliberately hashes bytes
// exactly as supplied.
func HashIdempotencyRequest(normalized []byte) string {
	sum := sha256.Sum256(normalized)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// AcquireIdempotency atomically establishes ownership, replays a completed
// response, reports an active owner, or rejects a key reused with another hash.
// Expired records may be replaced; abandoned/failed same-hash work may be
// reacquired with a fresh owner token.
func (r *TenantRepository) AcquireIdempotency(ctx context.Context, request IdempotencyRequest, now time.Time) (IdempotencyAcquisition, error) {
	if err := r.validateIdempotencyRequest(request, now); err != nil {
		return IdempotencyAcquisition{}, err
	}
	recordID, err := domain.NewID()
	if err != nil {
		return IdempotencyAcquisition{}, fmt.Errorf("generate idempotency record ID: %w", err)
	}
	ownerToken, err := domain.NewID()
	if err != nil {
		return IdempotencyAcquisition{}, fmt.Errorf("generate idempotency owner token: %w", err)
	}

	const statement = `
INSERT INTO idempotency_records (
    id, tenant_id, principal_id, route, idempotency_key, request_hash, state,
    owner_token, locked_until, created_at, expires_at
) VALUES ($6, $1, $2, $3, $4, $5, 'IN_PROGRESS', $7, $8, $9, $10)
ON CONFLICT (tenant_id, principal_id, route, idempotency_key) DO UPDATE SET
    id = EXCLUDED.id,
    request_hash = EXCLUDED.request_hash,
    state = 'IN_PROGRESS',
    response_status = NULL,
    response_headers = NULL,
    response_body = NULL,
    owner_token = EXCLUDED.owner_token,
    locked_until = EXCLUDED.locked_until,
    created_at = EXCLUDED.created_at,
    completed_at = NULL,
    expires_at = EXCLUDED.expires_at
WHERE idempotency_records.expires_at <= EXCLUDED.created_at
   OR (idempotency_records.request_hash = EXCLUDED.request_hash
       AND (idempotency_records.state = 'FAILED'
            OR (idempotency_records.state = 'IN_PROGRESS'
                AND idempotency_records.locked_until <= EXCLUDED.created_at)))
RETURNING id::text, request_hash, state, owner_token::text, response_status,
          response_headers, response_body`

	acquisition, err := scanIdempotency(r.db.QueryRow(ctx, statement,
		r.tenantID.String(), request.PrincipalID.String(), request.Route, request.Key,
		request.RequestHash, recordID.String(), ownerToken.String(), now.Add(request.Lease),
		now, now.Add(request.TTL)), request.RequestHash, true)
	if err == nil {
		return acquisition, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return IdempotencyAcquisition{}, fmt.Errorf("acquire idempotency record: %w", err)
	}

	const existingStatement = `
SELECT id::text, request_hash, state, owner_token::text, response_status,
       response_headers, response_body
FROM idempotency_records
WHERE tenant_id = $1 AND principal_id = $2 AND route = $3 AND idempotency_key = $4`
	acquisition, err = scanIdempotency(r.db.QueryRow(ctx, existingStatement,
		r.tenantID.String(), request.PrincipalID.String(), request.Route, request.Key), request.RequestHash, false)
	if err != nil {
		return IdempotencyAcquisition{}, fmt.Errorf("read contended idempotency record: %w", err)
	}
	return acquisition, nil
}

func (r *TenantRepository) CompleteIdempotency(ctx context.Context, acquisition IdempotencyAcquisition, response IdempotencyResponse, now time.Time) error {
	if r == nil || r.db == nil || r.tenantID.IsZero() || acquisition.RecordID.IsZero() || acquisition.OwnerToken.IsZero() {
		return errors.New("invalid idempotency completion ownership")
	}
	if _, err := domain.RequireUTC(now); err != nil {
		return err
	}
	if response.Status < 100 || response.Status > 599 || !validJSONObject(response.Headers) {
		return errors.New("invalid idempotency response")
	}
	const statement = `
UPDATE idempotency_records
SET state = 'COMPLETED', response_status = $4, response_headers = $5,
    response_body = $6, completed_at = $7, locked_until = $7
WHERE tenant_id = $1 AND id = $2 AND owner_token = $3 AND state = 'IN_PROGRESS'
RETURNING id::text`
	var ignored string
	err := r.db.QueryRow(ctx, statement, r.tenantID.String(), acquisition.RecordID.String(),
		acquisition.OwnerToken.String(), response.Status, []byte(response.Headers), response.Body, now).Scan(&ignored)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrIdempotencyOwnership
	}
	if err != nil {
		return fmt.Errorf("complete idempotency record: %w", err)
	}
	return nil
}

func (r *TenantRepository) FailIdempotency(ctx context.Context, acquisition IdempotencyAcquisition, now time.Time) error {
	if r == nil || r.db == nil || r.tenantID.IsZero() || acquisition.RecordID.IsZero() || acquisition.OwnerToken.IsZero() {
		return errors.New("invalid idempotency failure ownership")
	}
	if _, err := domain.RequireUTC(now); err != nil {
		return err
	}
	tag, err := r.db.Exec(ctx, `UPDATE idempotency_records SET state = 'FAILED', locked_until = $4 WHERE tenant_id = $1 AND id = $2 AND owner_token = $3 AND state = 'IN_PROGRESS'`, r.tenantID.String(), acquisition.RecordID.String(), acquisition.OwnerToken.String(), now)
	if err != nil {
		return fmt.Errorf("fail idempotency record: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return ErrIdempotencyOwnership
	}
	return nil
}

func (r *TenantRepository) validateIdempotencyRequest(request IdempotencyRequest, now time.Time) error {
	if r == nil || r.db == nil || r.tenantID.IsZero() || request.PrincipalID.IsZero() {
		return errors.New("idempotency request requires tenant and principal")
	}
	if len(request.Route) < 1 || len(request.Route) > 256 || len(request.Key) < 1 || len(request.Key) > 256 {
		return errors.New("idempotency route and key must contain 1 to 256 bytes")
	}
	if !requestHashPattern.MatchString(request.RequestHash) {
		return errors.New("idempotency request hash must be canonical SHA-256")
	}
	if request.Lease <= 0 || request.TTL <= 0 || request.Lease > request.TTL {
		return errors.New("idempotency lease and TTL must be positive and lease must not exceed TTL")
	}
	_, err := domain.RequireUTC(now)
	return err
}

func scanIdempotency(row pgx.Row, expectedHash string, ownsRecord bool) (IdempotencyAcquisition, error) {
	var id, requestHash, state, owner string
	var status *int
	var headers, body []byte
	if err := row.Scan(&id, &requestHash, &state, &owner, &status, &headers, &body); err != nil {
		return IdempotencyAcquisition{}, err
	}
	if requestHash != expectedHash {
		return IdempotencyAcquisition{}, ErrIdempotencyConflict
	}
	recordID, err := domain.ParseID(id)
	if err != nil {
		return IdempotencyAcquisition{}, fmt.Errorf("decode idempotency record ID: %w", err)
	}
	ownerToken, err := domain.ParseID(owner)
	if err != nil {
		return IdempotencyAcquisition{}, fmt.Errorf("decode idempotency owner token: %w", err)
	}
	result := IdempotencyAcquisition{RecordID: recordID, OwnerToken: ownerToken}
	switch state {
	case "IN_PROGRESS":
		if !ownsRecord {
			return IdempotencyAcquisition{}, ErrIdempotencyInFlight
		}
		result.Outcome = IdempotencyAcquired
		return result, nil
	case "COMPLETED":
		if status == nil || !validJSONObject(headers) {
			return IdempotencyAcquisition{}, errors.New("invalid completed idempotency record")
		}
		result.Outcome = IdempotencyReplay
		result.Response = &IdempotencyResponse{Status: *status, Headers: append(json.RawMessage(nil), headers...), Body: append([]byte(nil), body...)}
		return result, nil
	default:
		return IdempotencyAcquisition{}, ErrIdempotencyInFlight
	}
}

func validJSONObject(value []byte) bool {
	var object map[string]json.RawMessage
	return len(value) > 0 && json.Unmarshal(value, &object) == nil && object != nil
}
