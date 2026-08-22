//go:build integration

package postgres

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRunAdmissionCommitsCompleteAggregateAndRollsBackAtomically(t *testing.T) {
	databaseURL := os.Getenv("THINKPIXELAG_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("THINKPIXELAG_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	connection, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	migrator, _ := NewMigrator(ctx, connection, os.DirFS(projectMigrationsDir(t)))
	if err := migrator.Up(ctx); err != nil {
		t.Fatal(err)
	}
	_ = connection.Close(ctx)
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	tenant, principal, sponsor, agentID, versionID, approvalID := mustNewRepositoryID(t), mustNewRepositoryID(t), mustNewRepositoryID(t), mustNewRepositoryID(t), mustNewRepositoryID(t), mustNewRepositoryID(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	_, err = pool.Exec(ctx, `INSERT INTO tenants(id,slug,display_name,created_at,updated_at)VALUES($1,$2,$2,$3,$3)`, tenant.String(), "run002-"+tenant.String(), now)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []domain.ID{principal, sponsor} {
		if _, err := pool.Exec(ctx, `INSERT INTO principals(id,tenant_id,external_issuer,external_subject,principal_type,created_at)VALUES($1,$2,'https://run002.test',$3,'HUMAN',$4)`, id.String(), tenant.String(), id.String(), now); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO agents(id,tenant_id,name,owner_principal_id,sponsor_principal_id,risk_class,created_at,updated_at)VALUES($1,$2,$3,$4,$5,'HIGH',$6,$6)`, agentID.String(), tenant.String(), "admission-"+agentID.String(), principal.String(), sponsor.String(), now); err != nil {
		t.Fatal(err)
	}
	repositories, _ := NewRepositories(pool)
	repository, _ := repositories.ForTenant(tenant)
	manifest, _ := domain.NewAgentManifest("registry.example/agent@sha256:"+strings.Repeat("a", 64), nil, nil, nil, nil, domain.AgentLimits{})
	digest, _ := manifest.ContentDigest()
	version := domain.AgentVersion{ID: versionID, TenantID: tenant, AgentID: agentID, ContentDigest: digest, ImageDigest: "sha256:" + strings.Repeat("a", 64), Manifest: manifest, CreatedBy: principal, CreatedAt: now}
	if err := repository.RegisterAgentVersion(ctx, version, nil); err != nil {
		t.Fatal(err)
	}
	approvalPolicyID := mustNewRepositoryID(t)
	if _, err := pool.Exec(ctx, `INSERT INTO agent_version_approvals(id,tenant_id,agent_id,agent_version_id,decision,actor_principal_id,policy_decision_id,reason_code,created_at)VALUES($1,$2,$3,$4,'APPROVED',$5,$6,'registry.version.approved',$7)`, approvalID.String(), tenant.String(), agentID.String(), versionID.String(), principal.String(), approvalPolicyID.String(), now); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM outbox_messages WHERE tenant_id=$1`, tenant.String())
	})
	makeAdmission := func() (domain.RunAdmission, domain.RunVersionResolution, ports.RunAdmissionEvidence) {
		runID, envelopeID, decisionID := mustNewRepositoryID(t), mustNewRepositoryID(t), mustNewRepositoryID(t)
		constraints := map[string]any{"max_execution_time_seconds": float64(60), "max_llm_tokens": float64(100)}
		deadline := now.Add(time.Minute)
		admission := domain.RunAdmission{RunID: runID, EnvelopeID: envelopeID, TenantID: tenant, AgentID: agentID, AgentVersionID: versionID, AgentVersionDigest: digest, RequestedBy: principal, PolicyDecisionID: decisionID, State: domain.RunAdmitted, StateVersion: 1, Constraints: constraints, DeadlineAt: &deadline, CreatedAt: now, UpdatedAt: now}
		resolution := domain.RunVersionResolution{RunID: runID, TenantID: tenant, AgentID: agentID, AgentVersionID: versionID, ApprovalID: approvalID, AgentContentDigest: digest, PolicyBundleDigest: "sha256:" + strings.Repeat("f", 64), PolicyActivationVersion: 3, Mode: domain.ResolutionAutomatic, InvocationDecisionID: decisionID, ResolvedConstraints: constraints, ResolvedAt: now}
		evidence := ports.RunAdmissionEvidence{EventID: mustNewRepositoryID(t), AuditID: mustNewRepositoryID(t), OutboxID: mustNewRepositoryID(t), RequestID: mustNewRepositoryID(t), ReasonCodes: []string{"agent.invoke.allowed"}}
		return admission, resolution, evidence
	}
	admission, resolution, evidence := makeAdmission()
	if err := repository.AdmitRun(ctx, admission, resolution, evidence); err != nil {
		t.Fatal(err)
	}
	var state string
	var stateVersion, events, envelopes, resolutions, audits, outbox int
	if err := pool.QueryRow(ctx, `SELECT state,state_version FROM runs WHERE tenant_id=$1 AND id=$2`, tenant.String(), admission.RunID.String()).Scan(&state, &stateVersion); err != nil {
		t.Fatal(err)
	}
	if state != "ADMITTED" || stateVersion != 1 {
		t.Fatalf("state=%s version=%d", state, stateVersion)
	}
	projection, err := repository.GetRun(ctx, admission.RunID)
	if err != nil || projection.Run.ID != admission.RunID || projection.Run.TenantID != tenant || projection.Run.VersionDigest != digest || projection.Run.EnvelopeVersion != 1 || projection.AgentRiskClass != domain.AgentRiskHigh || projection.AgentOwnerID != principal {
		t.Fatalf("run projection=%+v error=%v", projection, err)
	}
	otherTenant := mustNewRepositoryID(t)
	if _, err := pool.Exec(ctx, `INSERT INTO tenants(id,slug,display_name,created_at,updated_at)VALUES($1,$2,$2,$3,$3)`, otherTenant.String(), "run-query-"+otherTenant.String(), now); err != nil {
		t.Fatal(err)
	}
	otherRepository, _ := repositories.ForTenant(otherTenant)
	if _, err := otherRepository.GetRun(ctx, admission.RunID); domain.ErrorCodeOf(err) != domain.CodeNotFound {
		t.Fatalf("cross-tenant run query error=%v", err)
	}
	for query, target := range map[string]*int{
		`SELECT count(*) FROM run_events WHERE tenant_id=$1 AND run_id=$2`:                                     &events,
		`SELECT count(*) FROM resource_envelopes WHERE tenant_id=$1 AND run_id=$2`:                             &envelopes,
		`SELECT count(*) FROM run_version_resolutions WHERE tenant_id=$1 AND run_id=$2`:                        &resolutions,
		`SELECT count(*) FROM audit_events WHERE tenant_id=$1 AND resource_type='run' AND resource_id=$2`:      &audits,
		`SELECT count(*) FROM outbox_messages WHERE tenant_id=$1 AND aggregate_type='run' AND aggregate_id=$2`: &outbox,
	} {
		if err := pool.QueryRow(ctx, query, tenant.String(), admission.RunID.String()).Scan(target); err != nil {
			t.Fatal(err)
		}
	}
	if events != 1 || envelopes != 1 || resolutions != 1 || audits != 1 || outbox != 1 {
		t.Fatalf("events=%d envelopes=%d resolutions=%d audits=%d outbox=%d", events, envelopes, resolutions, audits, outbox)
	}

	expectedVersion := int64(1)
	signal := domain.RunSignal{ID: mustNewRepositoryID(t), TenantID: tenant, RunID: admission.RunID, ActorPrincipalID: principal, Type: domain.RunSignalCustom, Payload: []byte(`{"name":"runtime.refresh","data":{"scope":"tools"}}`), IdempotencyKey: "integration-signal-0001", ExpectedStateVersion: &expectedVersion, CreatedAt: now.Add(time.Second)}
	signalEvidence := ports.RunSignalEvidence{EventID: mustNewRepositoryID(t), AuditID: mustNewRepositoryID(t), OutboxID: mustNewRepositoryID(t), RequestID: mustNewRepositoryID(t), PolicyDecisionID: mustNewRepositoryID(t), ReasonCodes: []string{"run.access.allowed"}}
	accepted, err := repository.AppendRunSignal(ctx, signal, signalEvidence)
	if err != nil || accepted.Sequence != 2 || accepted.Type != "run.signal.accepted" {
		t.Fatalf("accepted signal=%+v error=%v", accepted, err)
	}
	replayed, err := repository.AppendRunSignal(ctx, signal, signalEvidence)
	if err != nil || replayed.ID != accepted.ID || replayed.Sequence != accepted.Sequence {
		t.Fatalf("replayed signal=%+v error=%v", replayed, err)
	}
	if _, err := otherRepository.AppendRunSignal(ctx, signal, signalEvidence); err == nil {
		t.Fatal("cross-tenant signal unexpectedly succeeded")
	}
	eventWindow, err := repository.ListRunEvents(ctx, admission.RunID, 0, 128)
	if err != nil || eventWindow.OldestRetained != 1 || eventWindow.Latest != 2 || len(eventWindow.Events) != 2 || eventWindow.Events[0].Sequence != 1 || eventWindow.Events[1].Sequence != 2 {
		t.Fatalf("ordered event window=%+v error=%v", eventWindow, err)
	}
	resumedWindow, err := repository.ListRunEvents(ctx, admission.RunID, 1, 128)
	if err != nil || len(resumedWindow.Events) != 1 || resumedWindow.Events[0].ID != accepted.ID || resumedWindow.Events[0].Sequence != 2 {
		t.Fatalf("resumed event window=%+v error=%v", resumedWindow, err)
	}
	if _, err := otherRepository.ListRunEvents(ctx, admission.RunID, 0, 128); domain.ErrorCodeOf(err) != domain.CodeNotFound {
		t.Fatalf("cross-tenant event query error=%v", err)
	}
	stale := signal
	stale.ID, stale.IdempotencyKey = mustNewRepositoryID(t), "integration-signal-0002"
	wrongVersion := int64(2)
	stale.ExpectedStateVersion = &wrongVersion
	if _, err := repository.AppendRunSignal(ctx, stale, ports.RunSignalEvidence{EventID: mustNewRepositoryID(t), AuditID: mustNewRepositoryID(t), OutboxID: mustNewRepositoryID(t), RequestID: mustNewRepositoryID(t), PolicyDecisionID: mustNewRepositoryID(t), ReasonCodes: []string{"run.access.allowed"}}); domain.ErrorCodeOf(err) != domain.CodeConflict {
		t.Fatalf("stale signal error=%v", err)
	}
	const parallelSignals = 8
	var group sync.WaitGroup
	errorsBySignal := make(chan error, parallelSignals)
	for index := range parallelSignals {
		parallel := signal
		parallel.ID = mustNewRepositoryID(t)
		parallel.IdempotencyKey = fmt.Sprintf("integration-signal-parallel-%02d", index)
		evidence := ports.RunSignalEvidence{EventID: mustNewRepositoryID(t), AuditID: mustNewRepositoryID(t), OutboxID: mustNewRepositoryID(t), RequestID: mustNewRepositoryID(t), PolicyDecisionID: mustNewRepositoryID(t), ReasonCodes: []string{"run.access.allowed"}}
		group.Add(1)
		go func() {
			defer group.Done()
			_, appendErr := repository.AppendRunSignal(ctx, parallel, evidence)
			errorsBySignal <- appendErr
		}()
	}
	group.Wait()
	close(errorsBySignal)
	for appendErr := range errorsBySignal {
		if appendErr != nil {
			t.Fatalf("parallel signal: %v", appendErr)
		}
	}
	var signalCount, signalEvents, signalAudits, signalOutbox int
	for query, target := range map[string]*int{
		`SELECT count(*) FROM run_signals WHERE tenant_id=$1 AND run_id=$2`:                                                &signalCount,
		`SELECT count(*) FROM run_events WHERE tenant_id=$1 AND run_id=$2 AND event_type='run.signal.accepted'`:            &signalEvents,
		`SELECT count(*) FROM audit_events WHERE tenant_id=$1 AND resource_id=$2 AND action='runs.signal'`:                 &signalAudits,
		`SELECT count(*) FROM outbox_messages WHERE tenant_id=$1 AND aggregate_id=$2 AND event_type='run.signal.accepted'`: &signalOutbox,
	} {
		if err := pool.QueryRow(ctx, query, tenant.String(), admission.RunID.String()).Scan(target); err != nil {
			t.Fatal(err)
		}
	}
	if signalCount != 1+parallelSignals || signalEvents != 1+parallelSignals || signalAudits != 1+parallelSignals || signalOutbox != 1+parallelSignals {
		t.Fatalf("signals=%d events=%d audits=%d outbox=%d", signalCount, signalEvents, signalAudits, signalOutbox)
	}
	var distinctSequences, minimumSequence, maximumSequence int
	if err := pool.QueryRow(ctx, `SELECT count(DISTINCT sequence),min(sequence),max(sequence) FROM run_events WHERE tenant_id=$1 AND run_id=$2 AND event_type='run.signal.accepted'`, tenant.String(), admission.RunID.String()).Scan(&distinctSequences, &minimumSequence, &maximumSequence); err != nil {
		t.Fatal(err)
	}
	if distinctSequences != 1+parallelSignals || minimumSequence != 2 || maximumSequence != 2+parallelSignals {
		t.Fatalf("ordered signal sequences distinct=%d range=%d..%d", distinctSequences, minimumSequence, maximumSequence)
	}

	cancelExpected := int64(1)
	cancelAdmission, cancelResolution, cancelAdmissionEvidence := makeAdmission()
	if err := repository.AdmitRun(ctx, cancelAdmission, cancelResolution, cancelAdmissionEvidence); err != nil {
		t.Fatal(err)
	}
	cancellation := domain.RunCancellation{TenantID: tenant, RunID: cancelAdmission.RunID, ActorPrincipalID: principal, IdempotencyKey: "cancel:integration-cancel-0001", ReasonCode: "caller.request", ExpectedStateVersion: &cancelExpected, CreatedAt: now.Add(2 * time.Second)}
	cancellationEvidence := ports.RunCancellationEvidence{SignalID: mustNewRepositoryID(t), EventID: mustNewRepositoryID(t), AuditID: mustNewRepositoryID(t), OutboxID: mustNewRepositoryID(t), RequestID: mustNewRepositoryID(t), PolicyDecisionID: mustNewRepositoryID(t), ReasonCodes: []string{"run.access.allowed"}}
	cancelled, err := repository.CancelRun(ctx, cancellation, cancellationEvidence)
	if err != nil || cancelled.State != domain.RunCancelled || cancelled.StateVersion != 2 {
		t.Fatalf("cancelled run=%+v error=%v", cancelled, err)
	}
	replayedCancellation, err := repository.CancelRun(ctx, cancellation, cancellationEvidence)
	if err != nil || replayedCancellation.State != domain.RunCancelled || replayedCancellation.StateVersion != 2 {
		t.Fatalf("replayed cancellation=%+v error=%v", replayedCancellation, err)
	}
	var cancellationSignals, cancellationEvents, cancellationAudits, cancellationOutbox int
	for query, target := range map[string]*int{
		`SELECT count(*) FROM run_signals WHERE tenant_id=$1 AND run_id=$2 AND signal_type='CANCEL'`:                 &cancellationSignals,
		`SELECT count(*) FROM run_events WHERE tenant_id=$1 AND run_id=$2 AND event_type='run.cancelled'`:            &cancellationEvents,
		`SELECT count(*) FROM audit_events WHERE tenant_id=$1 AND resource_id=$2 AND action='runs.cancel'`:           &cancellationAudits,
		`SELECT count(*) FROM outbox_messages WHERE tenant_id=$1 AND aggregate_id=$2 AND event_type='run.cancelled'`: &cancellationOutbox,
	} {
		if err := pool.QueryRow(ctx, query, tenant.String(), cancelAdmission.RunID.String()).Scan(target); err != nil {
			t.Fatal(err)
		}
	}
	if cancellationSignals != 1 || cancellationEvents != 1 || cancellationAudits != 1 || cancellationOutbox != 1 {
		t.Fatalf("cancellation evidence signals=%d events=%d audits=%d outbox=%d", cancellationSignals, cancellationEvents, cancellationAudits, cancellationOutbox)
	}
	if _, err := otherRepository.CancelRun(ctx, cancellation, cancellationEvidence); err == nil {
		t.Fatal("cross-tenant cancellation unexpectedly succeeded")
	}

	staleAdmission, staleResolution, staleAdmissionEvidence := makeAdmission()
	if err := repository.AdmitRun(ctx, staleAdmission, staleResolution, staleAdmissionEvidence); err != nil {
		t.Fatal(err)
	}
	staleExpected := int64(2)
	staleCancellation := cancellation
	staleCancellation.RunID, staleCancellation.IdempotencyKey, staleCancellation.ExpectedStateVersion = staleAdmission.RunID, "cancel:integration-cancel-stale", &staleExpected
	if _, err := repository.CancelRun(ctx, staleCancellation, ports.RunCancellationEvidence{SignalID: mustNewRepositoryID(t), EventID: mustNewRepositoryID(t), AuditID: mustNewRepositoryID(t), OutboxID: mustNewRepositoryID(t), RequestID: mustNewRepositoryID(t), PolicyDecisionID: mustNewRepositoryID(t), ReasonCodes: []string{"run.access.allowed"}}); domain.ErrorCodeOf(err) != domain.CodeConflict {
		t.Fatalf("stale cancellation error=%v", err)
	}

	// Race cancellation against a worker terminal transition. Both contenders
	// lock the same row; exactly one terminal state wins and cancellation never
	// overwrites an established terminal state.
	raceAdmission, raceResolution, raceAdmissionEvidence := makeAdmission()
	if err := repository.AdmitRun(ctx, raceAdmission, raceResolution, raceAdmissionEvidence); err != nil {
		t.Fatal(err)
	}
	raceCancellation := cancellation
	raceCancellation.RunID, raceCancellation.IdempotencyKey = raceAdmission.RunID, "cancel:integration-cancel-race"
	raceEvidence := ports.RunCancellationEvidence{SignalID: mustNewRepositoryID(t), EventID: mustNewRepositoryID(t), AuditID: mustNewRepositoryID(t), OutboxID: mustNewRepositoryID(t), RequestID: mustNewRepositoryID(t), PolicyDecisionID: mustNewRepositoryID(t), ReasonCodes: []string{"run.access.allowed"}}
	startRace := make(chan struct{})
	raceErrors := make(chan error, 2)
	go func() {
		<-startRace
		_, cancelErr := repository.CancelRun(ctx, raceCancellation, raceEvidence)
		raceErrors <- cancelErr
	}()
	go func() {
		<-startRace
		_, workerErr := pool.Exec(ctx, `UPDATE runs SET state='COMPLETED',state_version=state_version+1,updated_at=$3,terminal_at=$3 WHERE tenant_id=$1 AND id=$2 AND state='ADMITTED'`, tenant.String(), raceAdmission.RunID.String(), now.Add(3*time.Second))
		raceErrors <- workerErr
	}()
	close(startRace)
	for range 2 {
		if raceErr := <-raceErrors; raceErr != nil {
			t.Fatalf("terminal race: %v", raceErr)
		}
	}
	raceProjection, err := repository.GetRun(ctx, raceAdmission.RunID)
	if err != nil || (raceProjection.Run.State != domain.RunCancelled && raceProjection.Run.State != domain.RunCompleted) || raceProjection.Run.StateVersion != 2 {
		t.Fatalf("terminal race projection=%+v error=%v", raceProjection, err)
	}
	var raceCancellationEvents int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM run_events WHERE tenant_id=$1 AND run_id=$2 AND event_type='run.cancelled'`, tenant.String(), raceAdmission.RunID.String()).Scan(&raceCancellationEvents); err != nil {
		t.Fatal(err)
	}
	if raceProjection.Run.State == domain.RunCancelled && raceCancellationEvents != 1 || raceProjection.Run.State == domain.RunCompleted && raceCancellationEvents != 0 {
		t.Fatalf("terminal race state=%s cancellation_events=%d", raceProjection.Run.State, raceCancellationEvents)
	}

	failedAdmission, failedResolution, failedEvidence := makeAdmission()
	failedEvidence.OutboxID = evidence.OutboxID
	if err := repository.AdmitRun(ctx, failedAdmission, failedResolution, failedEvidence); domain.ErrorCodeOf(err) != domain.CodeConflict {
		t.Fatalf("duplicate evidence error=%v", err)
	}
	var remaining int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM runs WHERE tenant_id=$1 AND id=$2`, tenant.String(), failedAdmission.RunID.String()).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 0 {
		t.Fatalf("failed admission left %d run rows", remaining)
	}

	// Drain earlier claimable fixtures so lease recovery can be asserted against
	// one deterministic run in this tenant.
	workerID := mustNewRepositoryID(t)
	claimAt := now.Add(10 * time.Second)
	for {
		lease, claimErr := repositories.ClaimRun(ctx, tenant, workerID, mustNewRepositoryID(t), claimAt, claimAt.Add(time.Minute))
		if domain.ErrorCodeOf(claimErr) == domain.CodeNotFound {
			break
		}
		if claimErr != nil {
			t.Fatal(claimErr)
		}
		if _, err := repositories.MutateWorkerRun(ctx, lease, domain.WorkerRunStart, mustNewRepositoryID(t), claimAt.Add(time.Second)); err != nil {
			t.Fatal(err)
		}
		if _, err := repositories.MutateWorkerRun(ctx, lease, domain.WorkerRunComplete, mustNewRepositoryID(t), claimAt.Add(2*time.Second)); err != nil {
			t.Fatal(err)
		}
	}

	workerAdmission, workerResolution, workerEvidence := makeAdmission()
	if err := repository.AdmitRun(ctx, workerAdmission, workerResolution, workerEvidence); err != nil {
		t.Fatal(err)
	}
	firstLease, err := repositories.ClaimRun(ctx, tenant, workerID, mustNewRepositoryID(t), claimAt, claimAt.Add(time.Minute))
	if err != nil || firstLease.RunID != workerAdmission.RunID || firstLease.FencingToken != 1 {
		t.Fatalf("first lease=%+v error=%v", firstLease, err)
	}
	firstLease, err = repositories.HeartbeatRun(ctx, firstLease, claimAt.Add(30*time.Second), claimAt.Add(90*time.Second))
	if err != nil || firstLease.ExpiresAt != claimAt.Add(90*time.Second) {
		t.Fatalf("heartbeat=%+v error=%v", firstLease, err)
	}
	secondWorker := mustNewRepositoryID(t)
	secondLease, err := repositories.ClaimRun(ctx, tenant, secondWorker, mustNewRepositoryID(t), claimAt.Add(91*time.Second), claimAt.Add(3*time.Minute))
	if err != nil || secondLease.RunID != firstLease.RunID || secondLease.FencingToken != firstLease.FencingToken+1 {
		t.Fatalf("reclaimed lease=%+v error=%v", secondLease, err)
	}
	if _, err := repositories.HeartbeatRun(ctx, firstLease, claimAt.Add(92*time.Second), claimAt.Add(4*time.Minute)); domain.ErrorCodeOf(err) != domain.CodeConflict {
		t.Fatalf("stale heartbeat error=%v", err)
	}
	if _, err := repositories.MutateWorkerRun(ctx, firstLease, domain.WorkerRunStart, mustNewRepositoryID(t), claimAt.Add(92*time.Second)); domain.ErrorCodeOf(err) != domain.CodeConflict {
		t.Fatalf("stale start error=%v", err)
	}
	running, err := repositories.MutateWorkerRun(ctx, secondLease, domain.WorkerRunStart, mustNewRepositoryID(t), claimAt.Add(92*time.Second))
	if err != nil || running.State != domain.RunRunning || running.StateVersion != 2 {
		t.Fatalf("running=%+v error=%v", running, err)
	}
	completed, err := repositories.MutateWorkerRun(ctx, secondLease, domain.WorkerRunComplete, mustNewRepositoryID(t), claimAt.Add(93*time.Second))
	if err != nil || completed.State != domain.RunCompleted || completed.StateVersion != 3 {
		t.Fatalf("completed=%+v error=%v", completed, err)
	}
	if _, err := repositories.MutateWorkerRun(ctx, secondLease, domain.WorkerRunFail, mustNewRepositoryID(t), claimAt.Add(94*time.Second)); domain.ErrorCodeOf(err) != domain.CodeConflict {
		t.Fatalf("post-terminal stale mutation error=%v", err)
	}
	var workerEvents, workerDistinctSequences int
	if err := pool.QueryRow(ctx, `SELECT count(*),count(DISTINCT sequence) FROM run_events WHERE tenant_id=$1 AND run_id=$2`, tenant.String(), workerAdmission.RunID.String()).Scan(&workerEvents, &workerDistinctSequences); err != nil {
		t.Fatal(err)
	}
	if workerEvents != 6 || workerDistinctSequences != workerEvents {
		t.Fatalf("worker events=%d distinct sequences=%d", workerEvents, workerDistinctSequences)
	}

	timeoutAdmission, timeoutResolution, timeoutEvidence := makeAdmission()
	if err := repository.AdmitRun(ctx, timeoutAdmission, timeoutResolution, timeoutEvidence); err != nil {
		t.Fatal(err)
	}
	type claimResult struct {
		lease domain.RunLease
		err   error
	}
	claimResults := make(chan claimResult, 2)
	claimStart := make(chan struct{})
	claimWorkers := []domain.ID{mustNewRepositoryID(t), mustNewRepositoryID(t)}
	claimLeases := []domain.ID{mustNewRepositoryID(t), mustNewRepositoryID(t)}
	for index := range 2 {
		go func(workerID, leaseID domain.ID) {
			<-claimStart
			lease, claimErr := repositories.ClaimRun(ctx, tenant, workerID, leaseID, claimAt.Add(4*time.Minute), claimAt.Add(5*time.Minute))
			claimResults <- claimResult{lease: lease, err: claimErr}
		}(claimWorkers[index], claimLeases[index])
	}
	close(claimStart)
	var timeoutLease domain.RunLease
	var successfulClaims, unavailableClaims int
	for range 2 {
		result := <-claimResults
		if result.err == nil {
			successfulClaims++
			timeoutLease = result.lease
		} else if domain.ErrorCodeOf(result.err) == domain.CodeNotFound {
			unavailableClaims++
		} else {
			t.Fatalf("concurrent claim error=%v", result.err)
		}
	}
	if successfulClaims != 1 || unavailableClaims != 1 || timeoutLease.RunID != timeoutAdmission.RunID {
		t.Fatalf("concurrent claims successful=%d unavailable=%d lease=%+v", successfulClaims, unavailableClaims, timeoutLease)
	}
	timedOut, err := repositories.MutateWorkerRun(ctx, timeoutLease, domain.WorkerRunTimeout, mustNewRepositoryID(t), claimAt.Add(4*time.Minute+time.Second))
	if err != nil || timedOut.State != domain.RunTimedOut || timedOut.StateVersion != 2 {
		t.Fatalf("timed out=%+v error=%v", timedOut, err)
	}
}
