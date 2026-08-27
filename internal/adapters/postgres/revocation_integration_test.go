//go:build integration

package postgres

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRevocationAtomicEvidenceAndConcurrentMonotonicEpochs(t *testing.T) {
	url := os.Getenv("THINKPIXELAG_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("THINKPIXELAG_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	m, _ := NewMigrator(ctx, conn, os.DirFS(projectMigrationsDir(t)))
	if err = m.Up(ctx); err != nil {
		t.Fatal(err)
	}
	_ = conn.Close(ctx)
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	// The shared integration database may contain rows from an interrupted prior
	// run. Keep revocation publisher evidence from entering unrelated outbox claims.
	if _, err = pool.Exec(ctx, `DELETE FROM outbox_messages WHERE aggregate_type='revocation'`); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM outbox_messages WHERE aggregate_type='revocation'`)
	}()
	tenant, actor := mustNewRepositoryID(t), mustNewRepositoryID(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	if _, err = pool.Exec(ctx, `INSERT INTO tenants(id,slug,display_name,created_at,updated_at) VALUES($1,$2,$2,$3,$3)`, tenant.String(), "rev-"+tenant.String(), now); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO principals(id,tenant_id,external_issuer,external_subject,principal_type,created_at) VALUES($1,$2,'https://rev.test',$4,'HUMAN',$3)`, actor.String(), tenant.String(), now, actor.String()); err != nil {
		t.Fatal(err)
	}
	repos, _ := NewRepositories(pool)
	repo, _ := repos.ForTenant(tenant)
	var baseline int64
	if err = pool.QueryRow(ctx, `SELECT security_epoch FROM security_epochs`).Scan(&baseline); err != nil {
		t.Fatal(err)
	}
	const n = 16
	results := make(chan domain.RevocationResult, n)
	errs := make(chan error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := mustNewRepositoryID(t)
			result, e := repo.CreateRevocation(ctx, domain.Revocation{ID: id, TenantID: &tenant, ActorPrincipalID: actor, Scope: domain.RevocationToolID, Target: fmt.Sprintf("tool-%02d", i), ReasonCode: "security.compromise", EffectiveAt: now, CreatedAt: now}, revEvidence(t))
			if e != nil {
				errs <- e
				return
			}
			results <- result
		}(i)
	}
	wg.Wait()
	close(results)
	close(errs)
	for e := range errs {
		t.Fatal(e)
	}
	security := make([]int64, 0, n)
	sequences := make([]int64, 0, n)
	var chosen domain.ID
	for result := range results {
		security = append(security, result.Epochs.Security)
		sequences = append(sequences, result.Sequence)
		chosen = result.RevocationID
	}
	sort.Slice(security, func(i, j int) bool { return security[i] < security[j] })
	sort.Slice(sequences, func(i, j int) bool { return sequences[i] < sequences[j] })
	for i, v := range security {
		if v != baseline+int64(i)+1 {
			t.Fatalf("security epochs=%v", security)
		}
	}
	for i := 1; i < n; i++ {
		if sequences[i] <= sequences[i-1] {
			t.Fatalf("duplicate/nonmonotonic sequences=%v", sequences)
		}
	}
	lift, err := repo.LiftRevocation(ctx, domain.RevocationLift{RevocationID: chosen, TenantID: &tenant, ActorPrincipalID: actor, ReasonCode: "security.restored", ApprovalReference: "approval:incident-1", ChangedAt: now.Add(time.Second)}, revEvidence(t))
	if err != nil {
		t.Fatal(err)
	}
	if lift.Epochs.Security != baseline+n+1 || lift.State != domain.RevocationLifted {
		t.Fatalf("lift=%+v", lift)
	}
	if _, err = repo.LiftRevocation(ctx, domain.RevocationLift{RevocationID: chosen, TenantID: &tenant, ActorPrincipalID: actor, ReasonCode: "security.restored", ApprovalReference: "approval:incident-2", ChangedAt: now.Add(2 * time.Second)}, revEvidence(t)); domain.ErrorCodeOf(err) != domain.CodeConflict {
		t.Fatalf("second lift error=%v", err)
	}
	var changes, logs, audits, outbox int
	if err = pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM revocation_changes WHERE tenant_id=$1),(SELECT count(*) FROM revocation_log WHERE tenant_id=$1),(SELECT count(*) FROM audit_events WHERE tenant_id=$1 AND resource_type='revocation'),(SELECT count(*) FROM outbox_messages WHERE tenant_id=$1 AND aggregate_type='revocation')`, tenant.String()).Scan(&changes, &logs, &audits, &outbox); err != nil {
		t.Fatal(err)
	}
	if changes != n+1 || logs != n+1 || audits != n+1 || outbox != n+1 {
		t.Fatalf("atomic evidence changes=%d logs=%d audits=%d outbox=%d", changes, logs, audits, outbox)
	}
	distributed, err := repos.RevocationChanges(ctx, tenant, 0, 100, now.Add(-time.Hour))
	if err != nil || len(distributed) != n+1 {
		t.Fatalf("distributed changes=%d err=%v", len(distributed), err)
	}
	for i := 1; i < len(distributed); i++ {
		if distributed[i].Sequence <= distributed[i-1].Sequence {
			t.Fatalf("distribution is not ordered: %+v", distributed)
		}
	}
	snapshot, err := repos.RevocationSnapshot(ctx, tenant, now.Add(3*time.Second))
	if err != nil || snapshot.Sequence != distributed[len(distributed)-1].Sequence || len(snapshot.Active) != n-1 || snapshot.Epochs.Security != baseline+n+1 {
		t.Fatalf("snapshot=%+v err=%v", snapshot, err)
	}
	reconciledAt := now.Add(4 * time.Second)
	checkpoint := ports.GatewayCheckpoint{TenantID: tenant, GatewayPrincipalID: actor, LastSequence: snapshot.Sequence, Epochs: snapshot.Epochs, LastReconciledAt: &reconciledAt, UpdatedAt: reconciledAt}
	if err = repos.SaveGatewayCheckpoint(ctx, checkpoint); err != nil {
		t.Fatal(err)
	}
	checkpoint.LastSequence--
	checkpoint.UpdatedAt = checkpoint.UpdatedAt.Add(time.Second)
	if err = repos.SaveGatewayCheckpoint(ctx, checkpoint); domain.ErrorCodeOf(err) != domain.CodeConflict {
		t.Fatalf("regressing checkpoint error=%v", err)
	}
	if _, err = pool.Exec(ctx, `UPDATE revocation_log SET committed_at=$1 WHERE sequence=$2`, now.Add(-2*time.Hour), distributed[0].Sequence); err != nil {
		t.Fatal(err)
	}
	if _, err = repos.RevocationChanges(ctx, tenant, 0, 100, now.Add(-time.Hour)); !errors.Is(err, ports.ErrRevocationCursorGone) {
		t.Fatalf("expired cursor error=%v", err)
	}
}
func revEvidence(t *testing.T) ports.RevocationEvidence {
	t.Helper()
	return ports.RevocationEvidence{ChangeID: mustNewRepositoryID(t), EventID: mustNewRepositoryID(t), AuditID: mustNewRepositoryID(t), OutboxID: mustNewRepositoryID(t), RequestID: mustNewRepositoryID(t), PolicyDecisionID: mustNewRepositoryID(t), ReasonCodes: []string{"revocation.allowed"}}
}
