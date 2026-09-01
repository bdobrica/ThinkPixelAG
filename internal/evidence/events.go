// Package evidence defines the versioned, provider-neutral security evidence
// contract. Persistence and export adapters consume these validated envelopes;
// the package does not own either transport.
package evidence

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
)

const ContractVersion = "thinkpixelag.evidence/v1"

type Type string

const (
	Policy           Type = "POLICY"
	Revocation       Type = "REVOCATION"
	ResourceOverride Type = "RESOURCE_OVERRIDE"
	Approval         Type = "APPROVAL"
	KeyLifecycle     Type = "KEY_LIFECYCLE"
	VersionApproval  Type = "VERSION_APPROVAL"
	BreakGlass       Type = "BREAK_GLASS"
)

type Outcome string

const (
	Succeeded Outcome = "SUCCEEDED"
	Denied    Outcome = "DENIED"
	Failed    Outcome = "FAILED"
)

type Actor struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

// Event is the stable export envelope. Data is a closed shape selected by
// EventType and is canonicalized by New before it crosses a persistence port.
type Event struct {
	SchemaVersion    string          `json:"schema_version"`
	ID               domain.ID       `json:"id"`
	EventType        Type            `json:"event_type"`
	TenantID         *domain.ID      `json:"tenant_id,omitempty"`
	Actor            Actor           `json:"actor"`
	Action           string          `json:"action"`
	Outcome          Outcome         `json:"outcome"`
	ReasonCodes      []string        `json:"reason_codes"`
	RequestID        *domain.ID      `json:"request_id,omitempty"`
	TraceID          string          `json:"trace_id,omitempty"`
	PolicyDecisionID *domain.ID      `json:"policy_decision_id,omitempty"`
	OccurredAt       time.Time       `json:"occurred_at"`
	Data             json.RawMessage `json:"data"`
}

type ArtifactData struct {
	ArtifactID         string `json:"artifact_id"`
	ArtifactKind       string `json:"artifact_kind"`
	FormatVersion      string `json:"format_version"`
	Revision           int64  `json:"revision"`
	PayloadDigest      string `json:"payload_digest"`
	PriorDigest        string `json:"prior_digest,omitempty"`
	KeyID              string `json:"key_id"`
	KeyVersion         string `json:"key_version"`
	SignatureAlgorithm string `json:"signature_algorithm"`
	ApprovalID         string `json:"approval_id,omitempty"`
}

type RevocationData struct {
	RevocationID          string `json:"revocation_id"`
	Change                string `json:"change"`
	Scope                 string `json:"scope"`
	Target                string `json:"target"`
	SecurityEpoch         int64  `json:"security_epoch"`
	TenantPolicyEpoch     *int64 `json:"tenant_policy_epoch,omitempty"`
	TenantRevocationEpoch *int64 `json:"tenant_revocation_epoch,omitempty"`
	AgentRevocationEpoch  *int64 `json:"agent_revocation_epoch,omitempty"`
	ApprovalID            string `json:"approval_id,omitempty"`
}

type ApprovalData struct {
	ApprovalID    string    `json:"approval_id"`
	ActionClass   string    `json:"action_class"`
	ResourceType  string    `json:"resource_type"`
	ResourceID    string    `json:"resource_id"`
	RequestDigest string    `json:"request_digest"`
	State         string    `json:"state"`
	RequesterID   string    `json:"requester_id"`
	ApproverID    string    `json:"approver_id,omitempty"`
	ExpiresAt     time.Time `json:"expires_at"`
}

type KeyLifecycleData struct {
	KeyID           string `json:"key_id"`
	KeyVersion      string `json:"key_version"`
	Algorithm       string `json:"algorithm"`
	Protection      string `json:"protection"`
	Change          string `json:"change"`
	PriorKeyVersion string `json:"prior_key_version,omitempty"`
	ApprovalID      string `json:"approval_id,omitempty"`
}

type VersionApprovalData struct {
	ApprovalID        string `json:"approval_id"`
	AgentID           string `json:"agent_id"`
	AgentVersionID    string `json:"agent_version_id"`
	ContentDigest     string `json:"content_digest"`
	Decision          string `json:"decision"`
	ApprovalReference string `json:"approval_reference,omitempty"`
}

type BreakGlassData struct {
	SessionID   string    `json:"session_id"`
	Scope       string    `json:"scope"`
	GrantDigest string    `json:"grant_digest"`
	ApprovalID  string    `json:"approval_id"`
	ExpiresAt   time.Time `json:"expires_at"`
	Change      string    `json:"change"`
}

var digestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
var tracePattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

func New[T any](event Event, data T) (Event, error) {
	encoded, err := json.Marshal(data)
	if err != nil {
		return Event{}, fmt.Errorf("encode evidence data: %w", err)
	}
	event.SchemaVersion, event.Data = ContractVersion, encoded
	if err := event.Validate(); err != nil {
		return Event{}, err
	}
	return event, nil
}

func (event Event) Validate() error {
	if event.SchemaVersion != ContractVersion || event.ID.IsZero() || !event.EventType.valid() {
		return errors.New("evidence envelope identity or version is invalid")
	}
	if event.Actor.Type != "PRINCIPAL" && event.Actor.Type != "WORKLOAD" && event.Actor.Type != "SYSTEM" {
		return errors.New("evidence actor type is invalid")
	}
	if !bounded(event.Actor.ID, 512) || !bounded(event.Action, 128) {
		return errors.New("evidence attribution is invalid")
	}
	if event.Outcome != Succeeded && event.Outcome != Denied && event.Outcome != Failed {
		return errors.New("evidence outcome is invalid")
	}
	if len(event.ReasonCodes) == 0 || len(event.ReasonCodes) > 16 {
		return errors.New("evidence reason codes are invalid")
	}
	for _, code := range event.ReasonCodes {
		if !bounded(code, 128) {
			return errors.New("evidence reason code is invalid")
		}
	}
	if event.TraceID != "" && !tracePattern.MatchString(event.TraceID) {
		return errors.New("evidence trace ID is invalid")
	}
	if _, err := domain.RequireUTC(event.OccurredAt); err != nil || event.OccurredAt.IsZero() {
		return errors.New("evidence occurrence time must be non-zero UTC")
	}
	return event.validateData()
}

func (kind Type) valid() bool {
	switch kind {
	case Policy, Revocation, ResourceOverride, Approval, KeyLifecycle, VersionApproval, BreakGlass:
		return true
	default:
		return false
	}
}

func (event Event) validateData() error {
	switch event.EventType {
	case Policy, ResourceOverride:
		var value ArtifactData
		if err := strictDecode(event.Data, &value); err != nil {
			return err
		}
		if !bounded(value.ArtifactID, 512) || !bounded(value.ArtifactKind, 128) || !bounded(value.FormatVersion, 128) || value.Revision < 1 || !digest(value.PayloadDigest) || value.PriorDigest != "" && !digest(value.PriorDigest) || !bounded(value.KeyID, 512) || !bounded(value.KeyVersion, 256) || !bounded(value.SignatureAlgorithm, 64) || !optional(value.ApprovalID, 512) {
			return errors.New("artifact evidence data is invalid")
		}
		if !oneOf(value.SignatureAlgorithm, "ED25519", "ECDSA_SHA256", "RSA_PSS_SHA256") {
			return errors.New("artifact signature algorithm is invalid")
		}
		if event.EventType == Policy && value.ArtifactKind != "POLICY_BUNDLE" || event.EventType == ResourceOverride && value.ArtifactKind != "RESOURCE_OVERRIDE_CONFIG" {
			return errors.New("artifact kind does not match evidence type")
		}
	case Revocation:
		var value RevocationData
		if err := strictDecode(event.Data, &value); err != nil {
			return err
		}
		if !bounded(value.RevocationID, 512) || (value.Change != "CREATED" && value.Change != "LIFTED" && value.Change != "EXPIRED") || !bounded(value.Scope, 128) || !bounded(value.Target, 512) || value.SecurityEpoch < 0 || !epochs(value.TenantPolicyEpoch, value.TenantRevocationEpoch, value.AgentRevocationEpoch) || !optional(value.ApprovalID, 512) {
			return errors.New("revocation evidence data is invalid")
		}
	case Approval:
		var value ApprovalData
		if err := strictDecode(event.Data, &value); err != nil {
			return err
		}
		if !bounded(value.ApprovalID, 512) || !bounded(value.ActionClass, 128) || !bounded(value.ResourceType, 128) || !bounded(value.ResourceID, 512) || !digest(value.RequestDigest) || !oneOf(value.State, "PENDING", "APPROVED", "REJECTED", "CONSUMED", "EXPIRED") || !bounded(value.RequesterID, 512) || !optional(value.ApproverID, 512) || value.ExpiresAt.IsZero() {
			return errors.New("approval evidence data is invalid")
		}
		decided := value.State == "APPROVED" || value.State == "REJECTED" || value.State == "CONSUMED"
		if decided != (value.ApproverID != "") || value.ApproverID == value.RequesterID {
			return errors.New("approval evidence violates four-eyes attribution")
		}
		if _, err := domain.RequireUTC(value.ExpiresAt); err != nil {
			return errors.New("approval expiry must be UTC")
		}
	case KeyLifecycle:
		var value KeyLifecycleData
		if err := strictDecode(event.Data, &value); err != nil {
			return err
		}
		if !bounded(value.KeyID, 512) || !bounded(value.KeyVersion, 256) || !oneOf(value.Algorithm, "ED25519", "ECDSA_SHA256", "RSA_PSS_SHA256") || !oneOf(value.Protection, "KMS", "HSM") || !oneOf(value.Change, "CREATED", "ENABLED", "DISABLED", "ROTATED", "DESTROY_SCHEDULED", "DESTROYED") || !optional(value.PriorKeyVersion, 256) || !optional(value.ApprovalID, 512) || value.Change == "ROTATED" && value.PriorKeyVersion == "" {
			return errors.New("key lifecycle evidence data is invalid")
		}
	case VersionApproval:
		var value VersionApprovalData
		if err := strictDecode(event.Data, &value); err != nil {
			return err
		}
		if !bounded(value.ApprovalID, 512) || !bounded(value.AgentID, 512) || !bounded(value.AgentVersionID, 512) || !digest(value.ContentDigest) || !oneOf(value.Decision, "APPROVED", "REJECTED", "DEPRECATED", "REVOKED") || !optional(value.ApprovalReference, 512) {
			return errors.New("version approval evidence data is invalid")
		}
	case BreakGlass:
		var value BreakGlassData
		if err := strictDecode(event.Data, &value); err != nil {
			return err
		}
		if !bounded(value.SessionID, 512) || !bounded(value.Scope, 512) || !digest(value.GrantDigest) || !bounded(value.ApprovalID, 512) || !oneOf(value.Change, "REQUESTED", "ACTIVATED", "USED", "EXPIRED", "REVOKED") || value.ExpiresAt.IsZero() || !value.ExpiresAt.After(event.OccurredAt) && (value.Change == "REQUESTED" || value.Change == "ACTIVATED") {
			return errors.New("break-glass evidence data is invalid")
		}
		if _, err := domain.RequireUTC(value.ExpiresAt); err != nil {
			return errors.New("break-glass expiry must be UTC")
		}
	}
	return nil
}

func strictDecode(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode evidence data: %w", err)
	}
	if decoder.Decode(&struct{}{}) == nil {
		return errors.New("evidence data contains trailing JSON")
	}
	return nil
}
func bounded(value string, max int) bool {
	return value != "" && strings.TrimSpace(value) == value && len(value) <= max
}
func optional(value string, max int) bool { return value == "" || bounded(value, max) }
func digest(value string) bool            { return digestPattern.MatchString(value) }
func oneOf(value string, allowed ...string) bool {
	for _, item := range allowed {
		if value == item {
			return true
		}
	}
	return false
}
func epochs(values ...*int64) bool {
	for _, value := range values {
		if value != nil && *value < 0 {
			return false
		}
	}
	return true
}
