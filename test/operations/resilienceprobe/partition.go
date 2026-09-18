package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/adapters/opa"
	"github.com/bdobrica/ThinkPixelAG/internal/application"
	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/policy"
)

// The probe consumes the real mTLS endpoint and exercises the existing
// freshness boundary. It is not a durable, production gateway implementation.
func partition(dir string) error {
	var identity struct{ Tenant, Principal string }
	raw, err := os.ReadFile(filepath.Join(dir, "identity.json"))
	if err != nil {
		return err
	}
	if err = json.Unmarshal(raw, &identity); err != nil {
		return err
	}
	tenant, err := domain.ParseID(identity.Tenant)
	if err != nil {
		return err
	}
	cert, err := tls.LoadX509KeyPair(filepath.Join(dir, "gateway-0.crt"), filepath.Join(dir, "gateway-0.key"))
	if err != nil {
		return err
	}
	pem, err := os.ReadFile(filepath.Join(dir, "ca.crt"))
	if err != nil {
		return err
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(pem) {
		return errors.New("invalid CA")
	}
	transport := &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, ServerName: "ops-api", RootCAs: roots, Certificates: []tls.Certificate{cert}}, ResponseHeaderTimeout: 20 * time.Second}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	tracker, _ := application.NewRevocationFreshnessTracker(domain.SystemClock{})
	type head struct {
		Sequence int64              `json:"authoritative_sequence"`
		Epochs   domain.EpochVector `json:"epochs"`
	}
	reconcile := func() (head, error) {
		req, err := http.NewRequestWithContext(ctx, "POST", os.Getenv("OPS_TRUSTED_URL")+"/v1/trusted/revocations/reconcile", bytes.NewBufferString(`{"last_sequence":0,"epochs":{"security_epoch":0,"tenant_policy_epoch":0,"tenant_revocation_epoch":0,"agent_revocation_epoch":0}}`))
		if err != nil {
			return head{}, err
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			return head{}, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			return head{}, errors.New("reconciliation failed")
		}
		var value head
		if err = json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&value); err != nil {
			return head{}, err
		}
		return value, tracker.RecordReconciliation(tenant, value.Sequence, value.Epochs)
	}
	before, err := reconcile()
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, "GET", os.Getenv("OPS_TRUSTED_URL")+"/v1/trusted/revocations/events?after_sequence="+strconv.FormatInt(before.Sequence, 10), nil)
	if err != nil {
		return err
	}
	response, err := client.Do(req)
	if err != nil {
		return err
	}
	if response.StatusCode != 200 {
		_ = response.Body.Close()
		return errors.New("stream did not connect")
	}
	_ = response.Body.Close() // The consumer now has no stream or reconcile refresh.
	active := func() (string, int64, bool) { return "sha256:ops011-fixture", 1, true }
	next, err := opa.New(os.Getenv("OPS_OPA_URL"), "/v1/data/thinkpixelag/authorization/decision", time.Second, 30*time.Second, "", nil, active)
	if err != nil {
		return err
	}
	guarded, err := application.NewFreshnessEnforcingEvaluator(next, tracker, application.DefaultRevocationFreshnessPolicy(30*time.Second))
	if err != nil {
		return err
	}
	in := policy.Input{ContractVersion: policy.ContractVersion, DecisionID: "ops011-partition", RequestTime: time.Now().UTC(), Subject: policy.Subject{PrincipalID: identity.Principal, TenantID: identity.Tenant, PrincipalType: "human", Roles: []string{"agent-invoker"}}, Action: "runs.signal", Resource: policy.Resource{Type: "run", ID: "ops011", TenantID: identity.Tenant, Attributes: map[string]any{}}, RequestedConstraints: map[string]any{}, AuthorityConstraints: map[string]any{}, Context: policy.RequestContext{RequestID: "ops011-partition"}}
	first, err := guarded.Decide(ctx, in)
	if err != nil || !first.Decision.Allow {
		return errors.New("fresh probe denied")
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(31 * time.Second):
	}
	stale, err := guarded.Decide(ctx, in)
	if err != nil || stale.Decision.Allow {
		return errors.New("partition did not fail closed")
	}
	after, err := reconcile()
	if err != nil {
		return err
	}
	if after.Sequence < before.Sequence || after.Epochs.Security < before.Epochs.Security {
		return errors.New("reconciliation regressed")
	}
	recovered, err := guarded.Decide(ctx, in)
	if err != nil || !recovered.Decision.Allow {
		return errors.New("reconciliation did not restore bounded service")
	}
	emit(map[string]any{"scenario": "stream_partition", "stream_connected": true, "partition_seconds": 31, "normal_write_denied_after_bound": true, "reconciliation_monotonic": true, "authorized_after_reconciliation": true, "scope": "test consumer with real mTLS endpoint and freshness boundary; not a production gateway"})
	return nil
}
