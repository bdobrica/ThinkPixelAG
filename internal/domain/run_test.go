package domain

import (
	"errors"
	"testing"
	"time"
)

func TestRunLifecycleTransitionTable(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name  string
		from  RunState
		to    RunState
		actor RunActor
	}{
		{"admit", RunPending, RunAdmitted, RunActorSystem},
		{"reject", RunPending, RunRejected, RunActorSystem},
		{"start", RunAdmitted, RunRunning, RunActorWorker},
		{"cancel admitted", RunAdmitted, RunCancelled, RunActorCaller},
		{"cancel running", RunRunning, RunCancelled, RunActorOperator},
		{"cancel paused", RunPausedForBudget, RunCancelled, RunActorSystem},
		{"timeout admitted", RunAdmitted, RunTimedOut, RunActorGovernor},
		{"timeout running", RunRunning, RunTimedOut, RunActorSystem},
		{"timeout paused", RunPausedForBudget, RunTimedOut, RunActorGovernor},
		{"complete", RunRunning, RunCompleted, RunActorWorker},
		{"fail", RunRunning, RunFailed, RunActorWorker},
		{"exhaust", RunRunning, RunBudgetExhausted, RunActorGovernor},
		{"pause for extension", RunBudgetExhausted, RunPausedForBudget, RunActorGovernor},
		{"fail budget", RunBudgetExhausted, RunFailedBudget, RunActorSystem},
		{"resume after extension", RunPausedForBudget, RunRunning, RunActorGovernor},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			run := lifecycleAt(test.from, 7, now)
			next, changed, err := run.Transition(RunTransition{To: test.to, Actor: test.actor, ExpectedVersion: 7, At: now.Add(time.Second)})
			if err != nil || !changed {
				t.Fatalf("Transition(%s -> %s by %s) = changed %v, err %v", test.from, test.to, test.actor, changed, err)
			}
			if next.State != test.to || next.Version != 8 || !next.UpdatedAt.Equal(now.Add(time.Second)) {
				t.Fatalf("unexpected transition result: %+v", next)
			}
			if test.to.Terminal() != (next.TerminalAt != nil) {
				t.Fatalf("terminal timestamp mismatch: %+v", next)
			}
			if run.State != test.from || run.Version != 7 {
				t.Fatal("transition mutated its receiver")
			}
		})
	}
}

func TestRunLifecycleRejectsIllegalActorsTransitionsAndVersions(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		run     RunLifecycle
		command RunTransition
		want    error
	}{
		{"caller cannot start", lifecycleAt(RunAdmitted, 2, now), command(RunRunning, RunActorCaller, 2, now), ErrRunActorNotPermitted},
		{"worker cannot cancel", lifecycleAt(RunRunning, 2, now), command(RunCancelled, RunActorWorker, 2, now), ErrRunActorNotPermitted},
		{"system cannot complete", lifecycleAt(RunRunning, 2, now), command(RunCompleted, RunActorSystem, 2, now), ErrRunActorNotPermitted},
		{"system cannot approve extension", lifecycleAt(RunBudgetExhausted, 2, now), command(RunPausedForBudget, RunActorSystem, 2, now), ErrRunActorNotPermitted},
		{"worker cannot resume", lifecycleAt(RunPausedForBudget, 2, now), command(RunRunning, RunActorWorker, 2, now), ErrRunActorNotPermitted},
		{"illegal edge", lifecycleAt(RunAdmitted, 2, now), command(RunCompleted, RunActorWorker, 2, now), ErrRunTransition},
		{"stale version", lifecycleAt(RunRunning, 2, now), command(RunCompleted, RunActorWorker, 1, now), ErrRunVersionConflict},
		{"future version", lifecycleAt(RunRunning, 2, now), command(RunCompleted, RunActorWorker, 3, now), ErrRunVersionConflict},
		{"unknown actor", lifecycleAt(RunRunning, 2, now), command(RunCompleted, "ADMIN", 2, now), ErrInvalidRunActor},
		{"unknown target", lifecycleAt(RunRunning, 2, now), command("LOST", RunActorWorker, 2, now), ErrInvalidRunState},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			original := test.run
			_, changed, err := test.run.Transition(test.command)
			if !errors.Is(err, test.want) || changed {
				t.Fatalf("Transition() = changed %v, err %v; want %v", changed, err, test.want)
			}
			if test.run.State != original.State || test.run.Version != original.Version {
				t.Fatal("rejected transition mutated its receiver")
			}
		})
	}
}

func TestRunLifecycleTerminalReplayAndImmutability(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		state RunState
		actor RunActor
	}{
		{RunRejected, RunActorSystem}, {RunCompleted, RunActorWorker}, {RunFailed, RunActorWorker},
		{RunCancelled, RunActorCaller}, {RunTimedOut, RunActorGovernor}, {RunFailedBudget, RunActorSystem},
	} {
		run := lifecycleAt(test.state, 9, now)
		next, changed, err := run.Transition(command(test.state, test.actor, 1, now.Add(time.Minute)))
		if err != nil || changed || next.State != run.State || next.Version != run.Version || !next.UpdatedAt.Equal(run.UpdatedAt) {
			t.Errorf("terminal replay for %s = %+v, changed %v, err %v", test.state, next, changed, err)
		}
		other := RunFailed
		if test.state == other {
			other = RunCompleted
		}
		if _, _, err := run.Transition(command(other, test.actor, 9, now.Add(time.Minute))); !errors.Is(err, ErrRunTerminal) {
			t.Errorf("terminal %s changed to %s: %v", test.state, other, err)
		}
		if _, _, err := run.Transition(command(test.state, RunActorOperator, 9, now.Add(time.Minute))); test.state != RunCancelled && !errors.Is(err, ErrRunTerminal) {
			t.Errorf("unauthorized terminal replay for %s: %v", test.state, err)
		}
	}
}

func TestRunLifecycleValidation(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
	terminal := now
	invalid := []RunLifecycle{
		{State: "UNKNOWN", Version: 1, UpdatedAt: now},
		{State: RunPending, Version: 0, UpdatedAt: now},
		{State: RunPending, Version: 1},
		{State: RunRunning, Version: 1, UpdatedAt: now, TerminalAt: &terminal},
		{State: RunCompleted, Version: 1, UpdatedAt: now},
	}
	for _, run := range invalid {
		if err := run.Validate(); err == nil {
			t.Errorf("invalid lifecycle accepted: %+v", run)
		}
	}
}

func FuzzRunLifecycleProperties(f *testing.F) {
	f.Add(uint8(0), uint8(2), uint8(1), int64(1))
	f.Add(uint8(3), uint8(4), uint8(1), int64(7))
	f.Fuzz(func(t *testing.T, fromIndex, toIndex, actorIndex uint8, expected int64) {
		states := []RunState{RunPending, RunRejected, RunAdmitted, RunRunning, RunCompleted, RunFailed, RunCancelled, RunTimedOut, RunBudgetExhausted, RunPausedForBudget, RunFailedBudget}
		actors := []RunActor{RunActorCaller, RunActorWorker, RunActorGovernor, RunActorOperator, RunActorSystem}
		now := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
		run := lifecycleAt(states[int(fromIndex)%len(states)], 7, now)
		next, changed, err := run.Transition(RunTransition{To: states[int(toIndex)%len(states)], Actor: actors[int(actorIndex)%len(actors)], ExpectedVersion: expected, At: now.Add(time.Second)})
		if err != nil {
			if changed {
				t.Fatal("failed transition reported a state change")
			}
			return
		}
		if err := next.Validate(); err != nil {
			t.Fatalf("successful transition violated lifecycle invariants: %v", err)
		}
		if changed {
			if next.Version != run.Version+1 || next.State == run.State || !transitionAllowed(run.State, next.State) || !actorAllowed(run.State, next.State, actors[int(actorIndex)%len(actors)]) {
				t.Fatalf("successful transition violated graph/version/actor invariants: before %+v after %+v", run, next)
			}
		} else if !run.State.Terminal() || next.State != run.State || next.Version != run.Version {
			t.Fatalf("only an unchanged terminal replay may report changed=false: before %+v after %+v", run, next)
		}
	})
}

func lifecycleAt(state RunState, version int64, at time.Time) RunLifecycle {
	run := RunLifecycle{State: state, Version: version, UpdatedAt: at}
	if state.Terminal() {
		terminalAt := at
		run.TerminalAt = &terminalAt
	}
	return run
}

func command(to RunState, actor RunActor, version int64, at time.Time) RunTransition {
	return RunTransition{To: to, Actor: actor, ExpectedVersion: version, At: at.Add(time.Second)}
}
