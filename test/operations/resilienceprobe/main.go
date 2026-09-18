// Command resilienceprobe exercises real adapters in an isolated operations
// fixture. It is not a production worker or gateway and never executes a harness.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/adapters/opa"
	"github.com/bdobrica/ThinkPixelAG/internal/adapters/postgres"
	"github.com/bdobrica/ThinkPixelAG/internal/adapters/valkey"
	"github.com/bdobrica/ThinkPixelAG/internal/application"
	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/policy"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: resilienceprobe worker|hold|cache-healthy|cache-down|partition PRIVATE_PATH")
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "partition":
		err = partition(os.Args[2])
	case "worker":
		err = worker(os.Args[2], false)
	case "hold":
		err = worker(os.Args[2], true)
	case "cache-healthy":
		err = cache(false)
	case "cache-down":
		err = cache(true)
	default:
		err = errors.New("unknown scenario")
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "resilience probe failed (details withheld)")
		os.Exit(1)
	}
}
func emit(v any) { _ = json.NewEncoder(os.Stdout).Encode(v) }
func worker(path string, hold bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	pool, err := pgxpool.New(ctx, os.Getenv("OPS_DATABASE_URL"))
	if err != nil {
		return err
	}
	defer pool.Close()
	repo, err := postgres.NewRepositories(pool)
	if err != nil {
		return err
	}
	tenant, err := domain.ParseID(os.Getenv("OPS_TENANT"))
	if err != nil {
		return err
	}
	principal, err := domain.ParseID(os.Getenv("OPS_WORKER"))
	if err != nil {
		return err
	}
	service, err := application.NewGovernedRunWorkerService(repo, repo, domain.SystemClock{}, 45*time.Second)
	if err != nil {
		return err
	}
	if hold {
		lease, err := service.Claim(ctx, tenant, principal)
		if err != nil {
			return err
		}
		raw, err := json.Marshal(lease)
		if err != nil {
			return err
		}
		// O_EXCL avoids consuming an old checkpoint from another probe.
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		if _, err = f.Write(raw); err != nil {
			_ = f.Close()
			return err
		}
		if err = f.Sync(); err != nil {
			_ = f.Close()
			return err
		}
		if err = f.Close(); err != nil {
			return err
		}
		<-ctx.Done()
		return ctx.Err()
	}
	if _, err = os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		return errors.New("checkpoint must be new")
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	child := exec.CommandContext(ctx, executable, "hold", path)
	if err = child.Start(); err != nil {
		return err
	}
	defer func() { _ = child.Process.Kill(); _ = child.Wait() }()
	var previous domain.RunLease
	for {
		raw, readErr := os.ReadFile(path)
		if readErr == nil && json.Unmarshal(raw, &previous) == nil && previous.Validate() == nil {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
	if err = child.Process.Kill(); err != nil {
		return err
	}
	_ = child.Wait()
	if wait := time.Until(previous.ExpiresAt.Add(time.Second)); wait > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
	current, err := service.Claim(ctx, tenant, principal)
	if err != nil {
		return err
	}
	if current.RunID != previous.RunID || current.FencingToken <= previous.FencingToken || current.LeaseID == previous.LeaseID {
		return errors.New("lease did not recover monotonically")
	}
	// Spoofing only the local expiry bypasses the clock check, forcing the
	// authoritative repository to reject the old lease ID/fence itself.
	previous.ExpiresAt = time.Now().UTC().Add(5 * time.Second)
	_, heartbeatErr := service.Heartbeat(ctx, previous)
	_, mutationErr := service.Operate(ctx, previous, domain.WorkerRunStart)
	if !conflict(heartbeatErr) || !conflict(mutationErr) {
		return errors.New("stale worker was not fenced")
	}
	if _, err = service.Heartbeat(ctx, current); err != nil {
		return err
	}
	emit(map[string]any{"scenario": "worker_process_kill", "same_run_reclaimed": true, "fence_increased": true, "stale_heartbeat_rejected": true, "stale_mutation_rejected": true, "new_heartbeat_accepted": true, "scope": "real application and PostgreSQL adapters; test process, not production worker composition"})
	return nil
}
func cache(expectDown bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c, err := valkey.New(os.Getenv("OPS_VALKEY_URL"), 500*time.Millisecond, []byte(os.Getenv("OPS_CACHE_KEY")))
	if err != nil {
		return err
	}
	active := func() (string, int64, bool) { return "sha256:ops011-fixture", 1, true }
	client, err := opa.New(os.Getenv("OPS_OPA_URL"), "/v1/data/thinkpixelag/authorization/decision", time.Second, 30*time.Second, "", nil, active)
	if err != nil {
		return err
	}
	in := policy.Input{ContractVersion: policy.ContractVersion, DecisionID: "ops011", RequestTime: time.Now().UTC(), Subject: policy.Subject{PrincipalID: "ops011", TenantID: "ops011", PrincipalType: "human", Roles: []string{"agent-invoker"}}, Action: "agents.list", Resource: policy.Resource{Type: "agent", ID: "ops011", TenantID: "ops011", Attributes: map[string]any{}}, RequestedConstraints: map[string]any{}, AuthorityConstraints: map[string]any{}, SecurityState: policy.SecurityState{FreshnessMaxAgeSeconds: 30}, Context: policy.RequestContext{RequestID: "ops011"}}
	evaluator, err := policy.NewCachedEvaluator(client, c, active, 30*time.Second, time.Now)
	if err != nil {
		return err
	}
	first, err := evaluator.Decide(ctx, in)
	if err != nil || !first.Decision.Allow {
		return errors.New("cache loss did not fall back to real OPA")
	}
	// A new evaluator has no local entries: a healthy hit must come from Valkey.
	evaluator, err = policy.NewCachedEvaluator(client, c, active, 30*time.Second, time.Now)
	if err != nil {
		return err
	}
	second, err := evaluator.Decide(ctx, in)
	if err != nil || !second.Decision.Allow {
		return errors.New("second decision failed")
	}
	if expectDown && second.Metadata.CacheStatus != "miss" {
		return errors.New("cache-down probe was not a miss")
	}
	if !expectDown && second.Metadata.CacheStatus != "hit" {
		return errors.New("remote cache was not exercised")
	}
	// An authority failure must never be hidden by a cached ALLOW.
	dead, err := opa.New("http://127.0.0.1:1", "/decision", 100*time.Millisecond, 30*time.Second, "", nil, active)
	if err != nil {
		return err
	}
	isolated, err := policy.NewCachedEvaluator(dead, c, active, 30*time.Second, time.Now)
	if err != nil {
		return err
	}
	in.SecurityState.Authoritative = true
	if _, err = isolated.Decide(ctx, in); err == nil {
		return errors.New("authoritative request reused ALLOW")
	}
	emit(map[string]any{"scenario": "real_valkey_and_opa", "cache_down": expectDown, "cache_status": second.Metadata.CacheStatus, "allow_from_real_opa": true, "authoritative_cache_bypass": true, "scope": "test composition; production API cache remains disabled"})
	return nil
}

func conflict(err error) bool {
	var typed *domain.Error
	return errors.As(err, &typed) && typed.Code() == domain.CodeConflict
}
