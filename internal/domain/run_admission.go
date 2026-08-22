package domain

import (
	"errors"
	"time"
)

// RunAdmission is the immutable result of an authorized root-run admission.
// Restricted objective and input data deliberately do not enter this durable
// governance record.
type RunAdmission struct {
	RunID, EnvelopeID, TenantID, AgentID, AgentVersionID ID
	AgentVersionDigest                                   string
	RequestedBy, PolicyDecisionID                        ID
	State                                                RunState
	StateVersion                                         int64
	Constraints                                          map[string]any
	DeadlineAt                                           *time.Time
	CreatedAt, UpdatedAt                                 time.Time
}

func (admission RunAdmission) Validate() error {
	if admission.RunID.IsZero() || admission.EnvelopeID.IsZero() || admission.TenantID.IsZero() || admission.AgentID.IsZero() || admission.AgentVersionID.IsZero() || admission.RequestedBy.IsZero() || admission.PolicyDecisionID.IsZero() {
		return errors.New("run admission identifiers must be set")
	}
	if !ValidDigest(admission.AgentVersionDigest) {
		return errors.New("run admission version digest must be canonical")
	}
	if admission.State != RunAdmitted || admission.StateVersion != 1 || admission.Constraints == nil {
		return errors.New("run admission must establish admitted state version one and constraints")
	}
	created, err := RequireUTC(admission.CreatedAt)
	if err != nil || created.IsZero() {
		return errors.New("run admission creation time must be non-zero UTC")
	}
	updated, err := RequireUTC(admission.UpdatedAt)
	if err != nil || !updated.Equal(created) {
		return errors.New("run admission update time must equal creation time")
	}
	if admission.DeadlineAt != nil {
		deadline, err := RequireUTC(*admission.DeadlineAt)
		if err != nil || !deadline.After(created) {
			return errors.New("run admission deadline must be UTC and after creation")
		}
	}
	return nil
}
