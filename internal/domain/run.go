package domain

import (
	"errors"
	"math"
	"time"
)

// RunState is the durable lifecycle state of a run.
type RunState string

const (
	RunPending         RunState = "PENDING"
	RunRejected        RunState = "REJECTED"
	RunAdmitted        RunState = "ADMITTED"
	RunRunning         RunState = "RUNNING"
	RunCompleted       RunState = "COMPLETED"
	RunFailed          RunState = "FAILED"
	RunCancelled       RunState = "CANCELLED"
	RunTimedOut        RunState = "TIMED_OUT"
	RunBudgetExhausted RunState = "BUDGET_EXHAUSTED"
	RunPausedForBudget RunState = "PAUSED_FOR_BUDGET"
	RunFailedBudget    RunState = "FAILED_BUDGET"
)

// RunActor is the trusted initiator category used by the lifecycle boundary.
// Actor identity and policy authorization are verified by the application layer.
type RunActor string

const (
	RunActorCaller   RunActor = "CALLER"
	RunActorWorker   RunActor = "WORKER"
	RunActorGovernor RunActor = "GOVERNOR"
	RunActorOperator RunActor = "OPERATOR"
	RunActorSystem   RunActor = "SYSTEM"
)

// Run is the tenant-owned public lifecycle projection. Policy-only agent
// metadata is kept alongside it by the application repository, not exposed.
type Run struct {
	ID, TenantID, AgentID, AgentVersionID, RequestedBy ID
	VersionDigest                                      string
	ParentRunID                                        *ID
	State                                              RunState
	StateVersion, EnvelopeVersion                      int64
	DeadlineAt                                         *time.Time
	CreatedAt, UpdatedAt                               time.Time
}

func (run Run) Validate() error {
	if run.ID.IsZero() || run.TenantID.IsZero() || run.AgentID.IsZero() || run.AgentVersionID.IsZero() || run.RequestedBy.IsZero() || !ValidDigest(run.VersionDigest) || run.StateVersion < 1 || run.EnvelopeVersion < 1 {
		return errors.New("run projection is invalid")
	}
	created, err := RequireUTC(run.CreatedAt)
	if err != nil || created.IsZero() {
		return errors.New("run creation time must be non-zero UTC")
	}
	updated, err := RequireUTC(run.UpdatedAt)
	if err != nil || updated.Before(created) || !run.State.Valid() {
		return errors.New("run lifecycle projection is invalid")
	}
	if run.ParentRunID != nil && (run.ParentRunID.IsZero() || *run.ParentRunID == run.ID) {
		return errors.New("run parent is invalid")
	}
	if run.DeadlineAt != nil {
		deadline, err := RequireUTC(*run.DeadlineAt)
		if err != nil || !deadline.After(created) {
			return errors.New("run deadline is invalid")
		}
	}
	return nil
}

var (
	ErrInvalidRunState      = errors.New("invalid run state")
	ErrInvalidRunActor      = errors.New("invalid run actor")
	ErrRunActorNotPermitted = errors.New("run actor is not permitted for transition")
	ErrRunVersionConflict   = errors.New("run state version conflict")
	ErrRunTerminal          = errors.New("terminal run cannot transition")
	ErrRunTransition        = errors.New("invalid run state transition")
)

// RunLifecycle is the pure mutable portion of a run aggregate. Version starts
// at one and increases exactly once for every successful state change.
type RunLifecycle struct {
	State      RunState
	Version    int64
	UpdatedAt  time.Time
	TerminalAt *time.Time
}

// RunTransition is a compare-and-swap lifecycle command. At must be an
// authoritative UTC timestamp. ExpectedVersion is mandatory and positive.
type RunTransition struct {
	To              RunState
	Actor           RunActor
	ExpectedVersion int64
	At              time.Time
}

// Transition returns a new lifecycle value and never mutates its receiver.
// changed is false only for an authorized replay of the established terminal
// state. That replay succeeds even when its expected version is stale.
func (run RunLifecycle) Transition(command RunTransition) (next RunLifecycle, changed bool, err error) {
	if err := run.Validate(); err != nil {
		return RunLifecycle{}, false, err
	}
	if !command.To.Valid() {
		return RunLifecycle{}, false, ErrInvalidRunState
	}
	if !command.Actor.Valid() {
		return RunLifecycle{}, false, ErrInvalidRunActor
	}
	at, err := RequireUTC(command.At)
	if err != nil || command.At.IsZero() || at.Before(run.UpdatedAt) {
		return RunLifecycle{}, false, errors.New("run transition time must be UTC and not precede the last update")
	}
	if command.ExpectedVersion <= 0 {
		return RunLifecycle{}, false, ErrRunVersionConflict
	}

	if run.State.Terminal() {
		if run.State == command.To && actorCanEstablishTerminal(command.Actor, command.To) {
			return run, false, nil
		}
		return RunLifecycle{}, false, ErrRunTerminal
	}
	if command.ExpectedVersion != run.Version {
		return RunLifecycle{}, false, ErrRunVersionConflict
	}
	if !transitionAllowed(run.State, command.To) {
		return RunLifecycle{}, false, ErrRunTransition
	}
	if !actorAllowed(run.State, command.To, command.Actor) {
		return RunLifecycle{}, false, ErrRunActorNotPermitted
	}
	if run.Version == math.MaxInt64 {
		return RunLifecycle{}, false, ErrRunVersionConflict
	}

	next = run
	next.State = command.To
	next.Version++
	next.UpdatedAt = at
	if command.To.Terminal() {
		terminalAt := at
		next.TerminalAt = &terminalAt
	} else {
		next.TerminalAt = nil
	}
	return next, true, nil
}

func (run RunLifecycle) Validate() error {
	if !run.State.Valid() {
		return ErrInvalidRunState
	}
	if run.Version <= 0 {
		return ErrRunVersionConflict
	}
	updated, err := RequireUTC(run.UpdatedAt)
	if err != nil || run.UpdatedAt.IsZero() {
		return errors.New("run update time must be a non-zero UTC timestamp")
	}
	if run.State.Terminal() != (run.TerminalAt != nil) {
		return errors.New("run terminal timestamp does not match state")
	}
	if run.TerminalAt != nil {
		terminal, err := RequireUTC(*run.TerminalAt)
		if err != nil || run.TerminalAt.IsZero() || !terminal.Equal(updated) {
			return errors.New("run terminal timestamp must equal the last update time in UTC")
		}
	}
	return nil
}

func (state RunState) Valid() bool {
	switch state {
	case RunPending, RunRejected, RunAdmitted, RunRunning, RunCompleted, RunFailed,
		RunCancelled, RunTimedOut, RunBudgetExhausted, RunPausedForBudget, RunFailedBudget:
		return true
	default:
		return false
	}
}

func (state RunState) Terminal() bool {
	switch state {
	case RunRejected, RunCompleted, RunFailed, RunCancelled, RunTimedOut, RunFailedBudget:
		return true
	default:
		return false
	}
}

func (actor RunActor) Valid() bool {
	switch actor {
	case RunActorCaller, RunActorWorker, RunActorGovernor, RunActorOperator, RunActorSystem:
		return true
	default:
		return false
	}
}

func transitionAllowed(from, to RunState) bool {
	switch from {
	case RunPending:
		return to == RunAdmitted || to == RunRejected
	case RunAdmitted:
		return to == RunRunning || to == RunCancelled || to == RunTimedOut
	case RunRunning:
		return to == RunCompleted || to == RunFailed || to == RunCancelled || to == RunTimedOut || to == RunBudgetExhausted
	case RunBudgetExhausted:
		return to == RunPausedForBudget || to == RunFailedBudget
	case RunPausedForBudget:
		return to == RunRunning || to == RunCancelled || to == RunTimedOut
	default:
		return false
	}
}

func actorAllowed(from, to RunState, actor RunActor) bool {
	switch {
	case from == RunPending:
		return actor == RunActorSystem
	case from == RunAdmitted && to == RunRunning:
		return actor == RunActorWorker
	case to == RunCompleted || to == RunFailed:
		return actor == RunActorWorker
	case to == RunCancelled:
		return actor == RunActorCaller || actor == RunActorOperator || actor == RunActorSystem
	case to == RunTimedOut || to == RunBudgetExhausted || to == RunFailedBudget:
		return actor == RunActorGovernor || actor == RunActorSystem
	case from == RunBudgetExhausted && to == RunPausedForBudget:
		return actor == RunActorGovernor
	case from == RunPausedForBudget && to == RunRunning:
		return actor == RunActorGovernor
	default:
		return false
	}
}

func actorCanEstablishTerminal(actor RunActor, state RunState) bool {
	switch state {
	case RunRejected:
		return actor == RunActorSystem
	case RunCompleted, RunFailed:
		return actor == RunActorWorker
	case RunCancelled:
		return actor == RunActorCaller || actor == RunActorOperator || actor == RunActorSystem
	case RunTimedOut, RunFailedBudget:
		return actor == RunActorGovernor || actor == RunActorSystem
	default:
		return false
	}
}
