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
	llmDimension, toolDimension := mustNewRepositoryID(t), mustNewRepositoryID(t)
	if _, err := pool.Exec(ctx, `INSERT INTO resource_dimensions(id,tenant_id,name,class,unit,scale,minimum_value,maximum_value,aggregation,created_at) VALUES($1,$2,'llm_tokens','CONSUMABLE','llm_tokens',0,0,1000,'SUM',$3)`, llmDimension.String(), tenant.String(), now); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO resource_dimensions(id,tenant_id,name,class,unit,scale,minimum_value,maximum_value,aggregation,created_at) VALUES($1,$2,'tool_calls','CONSUMABLE','calls',0,0,1000,'SUM',$3)`, toolDimension.String(), tenant.String(), now); err != nil {
		t.Fatal(err)
	}
	for _, definition := range []struct {
		id   domain.ID
		name string
	}{{mustNewRepositoryID(t), "active_children"}, {mustNewRepositoryID(t), "total_children"}, {mustNewRepositoryID(t), "delegation_depth"}} {
		if _, err := pool.Exec(ctx, `INSERT INTO resource_dimensions(id,tenant_id,name,class,unit,scale,minimum_value,maximum_value,aggregation,created_at) VALUES($1,$2,$3,'STRUCTURAL','children',0,0,1000,'MAX',$4)`, definition.id.String(), tenant.String(), definition.name, now); err != nil {
			t.Fatal(err)
		}
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
		constraints := map[string]any{"max_execution_time_seconds": float64(60), "max_llm_tokens": float64(100), "max_tool_calls": float64(50), "max_active_children": float64(10), "max_total_children": float64(20), "max_delegation_depth": float64(4)}
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
	var granted, available, consumed, allocated, balanceVersion int64
	var grantUnit string
	var grantScale int16
	if err := pool.QueryRow(ctx, `SELECT g.granted_value,g.unit,g.scale,b.available_value,b.direct_consumed_value,b.allocated_open_value,b.state_version FROM resource_envelope_grants g JOIN resource_balances b USING(tenant_id,envelope_id,dimension_id) WHERE g.tenant_id=$1 AND g.envelope_id=$2 AND g.dimension_id=$3`, tenant.String(), admission.EnvelopeID.String(), llmDimension.String()).Scan(&granted, &grantUnit, &grantScale, &available, &consumed, &allocated, &balanceVersion); err != nil {
		t.Fatal(err)
	}
	if granted != 100 || available != granted || consumed != 0 || allocated != 0 || balanceVersion != 1 || grantUnit != "llm_tokens" || grantScale != 0 {
		t.Fatalf("grant=%d %s/%d balance=%d/%d/%d v%d", granted, grantUnit, grantScale, available, consumed, allocated, balanceVersion)
	}
	if _, err := pool.Exec(ctx, `UPDATE resource_envelope_grants SET granted_value=101 WHERE tenant_id=$1 AND envelope_id=$2 AND dimension_id=$3`, tenant.String(), admission.EnvelopeID.String(), llmDimension.String()); err == nil {
		t.Fatal("immutable root grant accepted an update")
	}
	makeChild := func(parent domain.ID) (domain.RunAdmission, domain.RunVersionResolution, ports.RunAdmissionEvidence) {
		child, childResolution, childEvidence := makeAdmission()
		child.Constraints = map[string]any{"max_active_children": float64(10), "max_total_children": float64(20), "max_delegation_depth": float64(4)}
		childResolution.ResolvedConstraints = child.Constraints
		if err := repository.AdmitRun(ctx, child, childResolution, childEvidence); err != nil {
			t.Fatal(err)
		}
		if tag, err := pool.Exec(ctx, `UPDATE resource_envelopes SET parent_envelope_id=$1 WHERE tenant_id=$2 AND id=$3`, parent.String(), tenant.String(), child.EnvelopeID.String()); err != nil || tag.RowsAffected() != 1 {
			t.Fatalf("link child envelope: tag=%v err=%v", tag, err)
		}
		return child, childResolution, childEvidence
	}
	child, _, _ := makeChild(admission.EnvelopeID)
	reservation := domain.ResourceReservation{
		ID: mustNewRepositoryID(t), TenantID: tenant, ParentEnvelopeID: admission.EnvelopeID, ChildEnvelopeID: child.EnvelopeID, ChildRunID: child.RunID, CreatedAt: now.Add(time.Second),
		Amounts: []domain.ResourceReservationAmount{{DimensionID: toolDimension, Coefficient: 20}, {DimensionID: llmDimension, Coefficient: 40}},
	}
	reserved, err := repository.ReserveChildResources(ctx, reservation)
	if err != nil || reserved.Amounts[0].DimensionID.String() > reserved.Amounts[1].DimensionID.String() {
		t.Fatalf("reserve child resources=%+v error=%v", reserved, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE resource_reservation_items SET reserved_value=reserved_value+1 WHERE tenant_id=$1 AND reservation_id=$2 AND dimension_id=$3`, tenant.String(), reservation.ID.String(), llmDimension.String()); err == nil {
		t.Fatal("immutable reservation item accepted an update")
	}
	for _, expected := range []struct {
		dimension                          domain.ID
		amount, parentAvailable, allocated int64
	}{{llmDimension, 40, 60, 40}, {toolDimension, 20, 30, 20}} {
		var reservedValue, childGranted, childAvailable, parentAvailable, allocated int64
		err := pool.QueryRow(ctx, `SELECT i.reserved_value,g.granted_value,child.available_value,parent.available_value,parent.allocated_open_value FROM resource_reservation_items i JOIN resource_envelope_grants g ON g.tenant_id=i.tenant_id AND g.envelope_id=$3 AND g.dimension_id=i.dimension_id JOIN resource_balances child ON child.tenant_id=g.tenant_id AND child.envelope_id=g.envelope_id AND child.dimension_id=g.dimension_id JOIN resource_balances parent ON parent.tenant_id=i.tenant_id AND parent.envelope_id=$4 AND parent.dimension_id=i.dimension_id WHERE i.tenant_id=$1 AND i.reservation_id=$2 AND i.dimension_id=$5`, tenant.String(), reservation.ID.String(), child.EnvelopeID.String(), admission.EnvelopeID.String(), expected.dimension.String()).Scan(&reservedValue, &childGranted, &childAvailable, &parentAvailable, &allocated)
		if err != nil || reservedValue != expected.amount || childGranted != expected.amount || childAvailable != expected.amount || parentAvailable != expected.parentAvailable || allocated != expected.allocated {
			t.Fatalf("dimension %s reservation=%d child=%d/%d parent=%d/%d err=%v", expected.dimension, reservedValue, childGranted, childAvailable, parentAvailable, allocated, err)
		}
	}

	insufficientChild, _, _ := makeChild(admission.EnvelopeID)
	insufficient := domain.ResourceReservation{ID: mustNewRepositoryID(t), TenantID: tenant, ParentEnvelopeID: admission.EnvelopeID, ChildEnvelopeID: insufficientChild.EnvelopeID, ChildRunID: insufficientChild.RunID, CreatedAt: now.Add(2 * time.Second), Amounts: []domain.ResourceReservationAmount{{DimensionID: toolDimension, Coefficient: 10}, {DimensionID: llmDimension, Coefficient: 61}}}
	if _, err := repository.ReserveChildResources(ctx, insufficient); domain.ErrorCodeOf(err) != domain.CodeConflict {
		t.Fatalf("insufficient multi-resource reservation error=%v", err)
	}
	var failedReservations, failedItems, failedGrants int
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM resource_reservations WHERE tenant_id=$1 AND id=$2),(SELECT count(*) FROM resource_reservation_items WHERE tenant_id=$1 AND reservation_id=$2),(SELECT count(*) FROM resource_envelope_grants g JOIN resource_dimensions d ON d.tenant_id=g.tenant_id AND d.id=g.dimension_id WHERE g.tenant_id=$1 AND g.envelope_id=$3 AND d.class='CONSUMABLE')`, tenant.String(), insufficient.ID.String(), insufficientChild.EnvelopeID.String()).Scan(&failedReservations, &failedItems, &failedGrants); err != nil {
		t.Fatal(err)
	}
	var toolAvailable int64
	if err := pool.QueryRow(ctx, `SELECT available_value FROM resource_balances WHERE tenant_id=$1 AND envelope_id=$2 AND dimension_id=$3`, tenant.String(), admission.EnvelopeID.String(), toolDimension.String()).Scan(&toolAvailable); err != nil {
		t.Fatal(err)
	}
	if failedReservations != 0 || failedItems != 0 || failedGrants != 0 || toolAvailable != 30 {
		t.Fatalf("failed vector left reservation=%d items=%d grants=%d tool_available=%d", failedReservations, failedItems, failedGrants, toolAvailable)
	}

	parallelChildren := make([]domain.RunAdmission, 2)
	parallelReservationIDs := make([]domain.ID, 2)
	for index := range parallelChildren {
		parallelChildren[index], _, _ = makeChild(admission.EnvelopeID)
		parallelReservationIDs[index] = mustNewRepositoryID(t)
	}
	parallelErrors := make(chan error, len(parallelChildren))
	var reservationWait sync.WaitGroup
	for index, parallelChild := range parallelChildren {
		reservationWait.Add(1)
		go func(index int, child domain.RunAdmission) {
			defer reservationWait.Done()
			amounts := []domain.ResourceReservationAmount{{DimensionID: llmDimension, Coefficient: 40}, {DimensionID: toolDimension, Coefficient: 20}}
			if index == 1 {
				amounts[0], amounts[1] = amounts[1], amounts[0]
			}
			_, err := repository.ReserveChildResources(ctx, domain.ResourceReservation{ID: parallelReservationIDs[index], TenantID: tenant, ParentEnvelopeID: admission.EnvelopeID, ChildEnvelopeID: child.EnvelopeID, ChildRunID: child.RunID, CreatedAt: now.Add(time.Duration(3+index) * time.Second), Amounts: amounts})
			parallelErrors <- err
		}(index, parallelChild)
	}
	reservationWait.Wait()
	close(parallelErrors)
	var reservationSuccesses, reservationConflicts int
	for err := range parallelErrors {
		if err == nil {
			reservationSuccesses++
			continue
		}
		switch domain.ErrorCodeOf(err) {
		case domain.CodeConflict:
			reservationConflicts++
		default:
			t.Fatalf("parallel reservation error=%v", err)
		}
	}
	var llmAvailable, llmAllocated, parallelToolAvailable, parallelToolAllocated int64
	if err := pool.QueryRow(ctx, `SELECT llm.available_value,llm.allocated_open_value,tool.available_value,tool.allocated_open_value FROM resource_balances llm JOIN resource_balances tool ON tool.tenant_id=llm.tenant_id AND tool.envelope_id=llm.envelope_id WHERE llm.tenant_id=$1 AND llm.envelope_id=$2 AND llm.dimension_id=$3 AND tool.dimension_id=$4`, tenant.String(), admission.EnvelopeID.String(), llmDimension.String(), toolDimension.String()).Scan(&llmAvailable, &llmAllocated, &parallelToolAvailable, &parallelToolAllocated); err != nil {
		t.Fatal(err)
	}
	if reservationSuccesses != 1 || reservationConflicts != 1 || llmAvailable != 20 || llmAllocated != 80 || parallelToolAvailable != 10 || parallelToolAllocated != 40 {
		t.Fatalf("parallel successes=%d conflicts=%d llm=%d/%d tool=%d/%d", reservationSuccesses, reservationConflicts, llmAvailable, llmAllocated, parallelToolAvailable, parallelToolAllocated)
	}

	// Governed child admission composes the authorized aggregate and reservation
	// atomically. The parent envelope lock makes competing structural checks see
	// a serial order even when consumable capacity would allow both children.
	structuralRoot, structuralResolution, structuralEvidence := makeAdmission()
	structuralRoot.Constraints = map[string]any{"max_llm_tokens": float64(10), "max_active_children": float64(1), "max_total_children": float64(2), "max_delegation_depth": float64(1)}
	structuralResolution.ResolvedConstraints = structuralRoot.Constraints
	if err := repository.AdmitRun(ctx, structuralRoot, structuralResolution, structuralEvidence); err != nil {
		t.Fatal(err)
	}
	makeGovernedChild := func(offset time.Duration) (domain.RunAdmission, domain.RunVersionResolution, ports.RunAdmissionEvidence, domain.ResourceReservation) {
		childAdmission, childResolution, childEvidence := makeAdmission()
		childAdmission.DeadlineAt = nil
		childAdmission.Constraints = map[string]any{"max_active_children": float64(1), "max_total_children": float64(2), "max_delegation_depth": float64(1)}
		childResolution.ResolvedConstraints = childAdmission.Constraints
		reservation := domain.ResourceReservation{ID: mustNewRepositoryID(t), TenantID: tenant, ParentEnvelopeID: structuralRoot.EnvelopeID, ChildEnvelopeID: childAdmission.EnvelopeID, ChildRunID: childAdmission.RunID, CreatedAt: now.Add(offset), Amounts: []domain.ResourceReservationAmount{{DimensionID: llmDimension, Coefficient: 1}}}
		return childAdmission, childResolution, childEvidence, reservation
	}
	expanded, expandedResolution, expandedEvidence, expandedReservation := makeGovernedChild(19 * time.Second)
	expanded.Constraints["max_active_children"] = float64(2)
	if _, err := repository.AdmitChildRun(ctx, expanded, expandedResolution, expandedEvidence, expandedReservation); domain.ErrorCodeOf(err) != domain.CodeConflict {
		t.Fatalf("expanded child authority error=%v", err)
	}
	var rolledBackExpanded int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM runs WHERE tenant_id=$1 AND id=$2`, tenant.String(), expanded.RunID.String()).Scan(&rolledBackExpanded); err != nil || rolledBackExpanded != 0 {
		t.Fatalf("expanded child remained durable count=%d error=%v", rolledBackExpanded, err)
	}
	governed := make([]struct {
		admission   domain.RunAdmission
		resolution  domain.RunVersionResolution
		evidence    ports.RunAdmissionEvidence
		reservation domain.ResourceReservation
	}, 2)
	for index := range governed {
		governed[index].admission, governed[index].resolution, governed[index].evidence, governed[index].reservation = makeGovernedChild(time.Duration(20+index) * time.Second)
	}
	governedErrors := make(chan error, 2)
	for index := range governed {
		go func(candidate struct {
			admission   domain.RunAdmission
			resolution  domain.RunVersionResolution
			evidence    ports.RunAdmissionEvidence
			reservation domain.ResourceReservation
		}) {
			_, err := repository.AdmitChildRun(ctx, candidate.admission, candidate.resolution, candidate.evidence, candidate.reservation)
			governedErrors <- err
		}(governed[index])
	}
	var admittedChildren, rejectedChildren int
	for range governed {
		if err := <-governedErrors; err == nil {
			admittedChildren++
		} else if domain.ErrorCodeOf(err) == domain.CodeConflict {
			rejectedChildren++
		} else {
			t.Fatalf("governed child admission error=%v", err)
		}
	}
	var durableChildren, structuralReservations int
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM resource_envelopes WHERE tenant_id=$1 AND parent_envelope_id=$2),(SELECT count(*) FROM resource_reservations WHERE tenant_id=$1 AND parent_envelope_id=$2)`, tenant.String(), structuralRoot.EnvelopeID.String()).Scan(&durableChildren, &structuralReservations); err != nil {
		t.Fatal(err)
	}
	if admittedChildren != 1 || rejectedChildren != 1 || durableChildren != 1 || structuralReservations != 1 {
		t.Fatalf("governed admissions=%d/%d durable=%d reservations=%d", admittedChildren, rejectedChildren, durableChildren, structuralReservations)
	}

	// Closing the first reservation simulates the RES-009 settlement state for
	// the structural counters: active capacity reopens, lifetime capacity does
	// not. A second child is admitted; a third conflicts on total children.
	if _, err := pool.Exec(ctx, `UPDATE resource_reservations SET state='SETTLED',settled_at=$3 WHERE tenant_id=$1 AND parent_envelope_id=$2 AND state='OPEN'`, tenant.String(), structuralRoot.EnvelopeID.String(), now.Add(30*time.Second)); err != nil {
		t.Fatal(err)
	}
	second, secondResolution, secondEvidence, secondReservation := makeGovernedChild(31 * time.Second)
	if _, err := repository.AdmitChildRun(ctx, second, secondResolution, secondEvidence, secondReservation); err != nil {
		t.Fatalf("second lifetime child admission error=%v", err)
	}
	third, thirdResolution, thirdEvidence, thirdReservation := makeGovernedChild(32 * time.Second)
	if _, err := repository.AdmitChildRun(ctx, third, thirdResolution, thirdEvidence, thirdReservation); domain.ErrorCodeOf(err) != domain.CodeConflict {
		t.Fatalf("total-child ceiling error=%v", err)
	}
	var rolledBackThird int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM runs WHERE tenant_id=$1 AND id=$2`, tenant.String(), third.RunID.String()).Scan(&rolledBackThird); err != nil || rolledBackThird != 0 {
		t.Fatalf("rejected child remained durable count=%d error=%v", rolledBackThird, err)
	}

	// A grandchild would be depth two while the inherited absolute ceiling is
	// one, so its entire admission is rolled back.
	grandchild, grandchildResolution, grandchildEvidence, grandchildReservation := makeGovernedChild(33 * time.Second)
	grandchildReservation.ParentEnvelopeID = second.EnvelopeID
	if _, err := repository.AdmitChildRun(ctx, grandchild, grandchildResolution, grandchildEvidence, grandchildReservation); domain.ErrorCodeOf(err) != domain.CodeConflict {
		t.Fatalf("delegation-depth ceiling error=%v", err)
	}
	var rolledBackGrandchild int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM runs WHERE tenant_id=$1 AND id=$2`, tenant.String(), grandchild.RunID.String()).Scan(&rolledBackGrandchild); err != nil || rolledBackGrandchild != 0 {
		t.Fatalf("rejected grandchild remained durable count=%d error=%v", rolledBackGrandchild, err)
	}
	outOfBoundsAdmission, outOfBoundsResolution, outOfBoundsEvidence := makeAdmission()
	outOfBoundsAdmission.Constraints["max_llm_tokens"] = float64(1001)
	outOfBoundsResolution.ResolvedConstraints["max_llm_tokens"] = float64(1001)
	if err := repository.AdmitRun(ctx, outOfBoundsAdmission, outOfBoundsResolution, outOfBoundsEvidence); domain.ErrorCodeOf(err) != domain.CodeUnavailable {
		t.Fatalf("out-of-bounds grant error=%v", err)
	}
	var rolledBackRuns, rolledBackEnvelopes int
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM runs WHERE tenant_id=$1 AND id=$2),(SELECT count(*) FROM resource_envelopes WHERE tenant_id=$1 AND id=$3)`, tenant.String(), outOfBoundsAdmission.RunID.String(), outOfBoundsAdmission.EnvelopeID.String()).Scan(&rolledBackRuns, &rolledBackEnvelopes); err != nil {
		t.Fatal(err)
	}
	if rolledBackRuns != 0 || rolledBackEnvelopes != 0 {
		t.Fatalf("invalid grant left runs=%d envelopes=%d", rolledBackRuns, rolledBackEnvelopes)
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
	if _, err := pool.Exec(ctx, `DELETE FROM resource_dimensions WHERE tenant_id=$1 AND id=$2`, tenant.String(), llmDimension.String()); err == nil {
		t.Fatal("dimension with grants unexpectedly deleted")
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
	var workerOutbox, matchingWorkerOutbox int
	if err := pool.QueryRow(ctx, `SELECT count(*),count(o.id)
	FROM run_events e LEFT JOIN outbox_messages o ON o.id=e.id AND o.tenant_id=e.tenant_id AND o.aggregate_type='run' AND o.aggregate_id=e.run_id::text AND o.event_type=e.event_type AND o.payload=e.payload
	WHERE e.tenant_id=$1 AND e.run_id=$2 AND e.event_type IN ('run.claimed','run.heartbeat','run.started','run.completed')`, tenant.String(), workerAdmission.RunID.String()).Scan(&workerOutbox, &matchingWorkerOutbox); err != nil {
		t.Fatal(err)
	}
	if workerOutbox != 5 || matchingWorkerOutbox != workerOutbox {
		t.Fatalf("worker outbox=%d matching events=%d", workerOutbox, matchingWorkerOutbox)
	}

	// Simulate a process crash after the sink accepted a worker event but before
	// its outbox row was acknowledged. Lease expiry must redeliver the same ID.
	crashSink := &recordingSink{}
	crashPublisher, err := NewOutboxPublisher(pool, crashSink, OutboxConfig{WorkerID: "run009-crash-worker", BatchSize: 1000, MaxAttempts: 3, Lease: time.Minute, BaseRetry: time.Second, MaxRetry: time.Minute, Jitter: func(time.Duration) time.Duration { return 0 }})
	if err != nil {
		t.Fatal(err)
	}
	crashAt := claimAt.Add(10 * time.Minute)
	claimed, err := crashPublisher.claim(ctx, crashAt)
	if err != nil {
		t.Fatal(err)
	}
	var crashedMessage *ClaimedMessage
	for index := range claimed {
		if claimed[index].AggregateID == workerAdmission.RunID.String() && claimed[index].EventType == "run.completed" {
			crashedMessage = &claimed[index]
			break
		}
	}
	if crashedMessage == nil {
		t.Fatal("completed worker outbox message was not claimed")
	}
	if err := crashSink.Send(ctx, crashedMessage.OutboxMessage); err != nil {
		t.Fatal(err)
	}
	if _, err := crashPublisher.PublishBatch(ctx, crashAt.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if crashSink.count(crashedMessage.ID) != 2 {
		t.Fatalf("crash replay deliveries=%d want 2", crashSink.count(crashedMessage.ID))
	}
	var replayPublished *time.Time
	if err := pool.QueryRow(ctx, `SELECT published_at FROM outbox_messages WHERE id=$1`, crashedMessage.ID.String()).Scan(&replayPublished); err != nil || replayPublished == nil {
		t.Fatalf("replayed publication=%v error=%v", replayPublished, err)
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
