//go:build integration

package postgres

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/artifact"
	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/policy"
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPolicyActivationAndRollbackAppendVersions(t *testing.T) {
	databaseURL := os.Getenv("THINKPIXELAG_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("THINKPIXELAG_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	m, err := NewMigrator(ctx, conn, os.DirFS(projectMigrationsDir(t)))
	if err != nil {
		t.Fatal(err)
	}
	if err = m.Up(ctx); err != nil {
		t.Fatal(err)
	}
	conn.Close(ctx)
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tx, _ := NewTransactor(pool)
	now := time.Now().UTC().Truncate(time.Microsecond)
	fresh, _ := policy.NewFreshness(time.Minute, func() time.Time { return now })
	store, _ := NewPolicyStore(pool, tx, fresh)
	tenant, actor, b1, b2 := mustNewRepositoryID(t), mustNewRepositoryID(t), mustNewRepositoryID(t), mustNewRepositoryID(t)
	var invalidated []string
	store.SetCacheInvalidator(func(got string) { invalidated = append(invalidated, got) })
	slug := "pol002-" + tenant.String()
	_, err = pool.Exec(ctx, `INSERT INTO tenants(id,slug,display_name,created_at,updated_at) VALUES($1,$2,$2,$3,$3)`, tenant.String(), slug, now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `INSERT INTO principals(id,tenant_id,external_issuer,external_subject,principal_type,created_at) VALUES($1,$2,'https://policy.test',$3,'HUMAN',$4)`, actor.String(), tenant.String(), actor.String(), now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM policy_activations WHERE tenant_id=$1`, tenant.String())
		_, _ = pool.Exec(context.Background(), `DELETE FROM policy_bundles WHERE tenant_id=$1`, tenant.String())
		_, _ = pool.Exec(context.Background(), `DELETE FROM principals WHERE tenant_id=$1`, tenant.String())
		_, _ = pool.Exec(context.Background(), `DELETE FROM tenants WHERE id=$1`, tenant.String())
	})
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	verifier := integrationSignatureVerifier{key: publicKey}
	for index, id := range []domain.ID{b1, b2} {
		body := []byte(id.String())
		revision := uint64(index + 1)
		signingDigest, digest, _ := artifact.SigningDigest(artifact.PolicyBundle, policy.ContractVersion, revision, body)
		signature := ed25519.Sign(privateKey, signingDigest.Value)
		if err := store.VerifyAndPersist(ctx, PolicyBundle{ID: id, TenantID: tenant, CreatedBy: actor, Channel: "stable", Digest: digest, ContractVersion: policy.ContractVersion, ArtifactRevision: revision, Bundle: body, Signature: signature, SignerKeyID: "test-key", SignerKeyVersion: "1", SignatureAlgorithm: ports.SignatureEd25519, CreatedAt: now}, verifier, integrationBundleValidator{}); err != nil {
			t.Fatal(err)
		}
	}
	a1, err := store.Activate(ctx, tenant, b1, actor, "stable", "policy.activate", now)
	if err != nil {
		t.Fatal(err)
	}
	a2, err := store.Activate(ctx, tenant, b2, actor, "stable", "policy.activate", now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	a3, err := store.Rollback(ctx, tenant, b1, actor, "stable", "policy.rollback", now.Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if a1.Version != 1 || a2.Version != 2 || a3.Version != 3 {
		t.Fatalf("versions: %d %d %d", a1.Version, a2.Version, a3.Version)
	}
	if len(invalidated) != 3 || invalidated[0] != tenant.String() || invalidated[1] != tenant.String() || invalidated[2] != tenant.String() {
		t.Fatalf("cache invalidations=%v", invalidated)
	}
	repositories, _ := NewRepositories(pool)
	runtimeState, err := repositories.LoadRuntimeSecurityState(ctx, now.Add(3*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	foundRuntimeState := false
	for _, state := range runtimeState {
		if state.Policy.TenantID == tenant.String() && state.Policy.Channel == "stable" {
			foundRuntimeState = state.Policy.BundleID == b1.String() && state.Policy.Version == 3 && state.Sequence >= 0 && state.Epochs.Security >= 0
		}
	}
	if !foundRuntimeState {
		t.Fatalf("active policy and revocation head were not loaded: %+v", runtimeState)
	}
	operationalSnapshot, err := repositories.LoadOperationalMetrics(ctx, now.Add(3*time.Second))
	if err != nil || operationalSnapshot.OutboxPending < 0 || operationalSnapshot.OutboxOldestAge < 0 || operationalSnapshot.OldestSettlementLag < 0 {
		t.Fatalf("operational metrics snapshot=%+v err=%v", operationalSnapshot, err)
	}
	var activeCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM policy_activations WHERE tenant_id=$1 AND channel='stable' AND deactivated_at IS NULL`, tenant.String()).Scan(&activeCount); err != nil || activeCount != 1 {
		t.Fatalf("active count=%d err=%v", activeCount, err)
	}
}

type integrationBundleValidator struct{}

func (integrationBundleValidator) Validate(context.Context, []byte) error { return nil }

type integrationSignatureVerifier struct{ key ed25519.PublicKey }

func (v integrationSignatureVerifier) Verify(_ context.Context, signature ports.Signature, digest ports.SigningDigest) error {
	if signature.KeyID != "test-key" || signature.KeyVersion != "1" || signature.Algorithm != ports.SignatureEd25519 || digest.Hash != "SHA-256" || !ed25519.Verify(v.key, digest.Value, signature.Value) {
		return errors.New("invalid test signature")
	}
	return nil
}
