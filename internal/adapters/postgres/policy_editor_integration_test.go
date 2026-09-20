//go:build integration

package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/bdobrica/ThinkPixelAG/internal/application"
	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPolicyEditorRevisionPromotionAndIndependentRollback(t *testing.T) {
	f := newAdministrationFixture(t)
	ctx := context.Background()
	approver := mustNewRepositoryID(t)
	if _, err := f.pool.Exec(ctx, `INSERT INTO principals(id,tenant_id,external_issuer,external_subject,principal_type,created_at)VALUES($1,$2,'https://admin.test',$3,'HUMAN',$4)`, approver.String(), f.tenant.String(), approver.String(), time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	handler := administrationHTTPWithApprover(t, f, approver)
	call := func(method, path, key, token string, body any) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(body)
		r := httptest.NewRequest(method, path, bytes.NewReader(raw))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Authorization", "Bearer "+token)
		if key != "" {
			r.Header.Set("Idempotency-Key", key)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	require := func(w *httptest.ResponseRecorder, status int) {
		t.Helper()
		if w.Code != status {
			t.Fatalf("HTTP=%d want=%d body=%s", w.Code, status, w.Body)
		}
	}
	source := strings.Replace(administrationPolicySource, "governance.operation.allowed", "agent.invoke.allowed", 1)
	saved := call("POST", "/v1/admin/policy-drafts", "draft-create-test-key", "admin", application.SavePolicyDraft{Source: source})
	require(saved, 201)
	var draft ports.PolicyDraft
	if err := json.Unmarshal(saved.Body.Bytes(), &draft); err != nil {
		t.Fatal(err)
	}
	path := "/v1/admin/policy-drafts/" + draft.ID.String()
	require(call("PUT", path, "draft-stale-edit-key", "admin", application.SavePolicyDraft{Source: source, ExpectedRevision: 0}), 409)
	require(call("GET", path, "", "invoker", nil), 403)
	got := call("GET", path, "", "admin", nil)
	require(got, 200)
	if !strings.Contains(got.Body.String(), "source") {
		t.Fatal("source missing")
	}
	require(call("POST", path+"/validation", "draft-validate-test-key", "admin", map[string]any{"revision": 1}), 200)
	require(call("POST", path+"/promotions", "draft-promote-wrong-key", "admin", application.PromotePolicyDraft{Revision: 1, Digest: f.initial.Digest, ArtifactRevision: 2}), 409)
	promoted := call("POST", path+"/promotions", "draft-promote-test-key", "admin", application.PromotePolicyDraft{Revision: 1, Digest: draft.Digest, ArtifactRevision: 2})
	require(promoted, 201)
	activePath := "/v1/admin/policies/" + draft.Digest + "/activations"
	require(call("POST", activePath, "draft-activate-test-key", "admin", application.PolicyActivate{Channel: "stable", Reason: "policy.activate", Approval: "not-required"}), 201)
	req := call("POST", "/v1/admin/policies/"+f.initial.Digest+"/rollback-approvals", "rollback-request-test-key", "admin", application.RequestRollbackApproval{ExpectedPolicyEpoch: 2, LifetimeSeconds: 300, Reason: "policy.rollback"})
	require(req, 201)
	var approval ports.ApprovalView
	if err := json.Unmarshal(req.Body.Bytes(), &approval); err != nil {
		t.Fatal(err)
	}
	if _, err := f.repo.GovernanceApproval(ctx, approval.ID); err != nil {
		t.Fatalf("stored approval: %v", err)
	}
	decisionPath := "/v1/admin/approvals/" + approval.ID.String() + "/decisions"
	require(call("POST", decisionPath, "rollback-self-test-key", "admin", map[string]bool{"approved": true}), 409)
	require(call("POST", decisionPath, "rollback-approve-test-key", "approver", map[string]bool{"approved": true}), 201)
	rollbackPath := "/v1/admin/policies/" + f.initial.Digest + "/activations"
	body := application.PolicyActivate{Channel: "stable", Reason: "policy.rollback", Approval: approval.ID.String()}
	require(call("POST", rollbackPath, "rollback-consume-test-key", "admin", body), 201)
	replay := call("POST", rollbackPath, "rollback-repeat-test-key", "admin", body)
	if replay.Code == 201 {
		t.Fatal("reused consumed approval")
	}
	current := call("GET", "/v1/admin/policy-activations/current", "", "admin", nil)
	require(current, 200)
	var active ports.PolicyActivation
	json.Unmarshal(current.Body.Bytes(), &active)
	if active.Version != 3 || active.Digest != f.initial.Digest {
		t.Fatalf("rollback=%+v", active)
	}
	view := call("GET", "/v1/admin/approvals/"+approval.ID.String(), "", "admin", nil)
	require(view, 200)
	if !strings.Contains(view.Body.String(), "CONSUMED") {
		t.Fatal("approval not consumed")
	}
	require(call("PUT", path, "draft-invalid-save-key", "admin", application.SavePolicyDraft{ExpectedRevision: 1, Source: "invalid rego"}), 201)
	require(call("POST", path+"/validation", "draft-invalid-check-key", "admin", map[string]any{"revision": 2}), 400)
	_, still, err := f.repo.CurrentPolicy(ctx, "stable")
	if err != nil || still.Version != 3 {
		t.Fatal("invalid draft changed active policy")
	}
	page := call("GET", "/v1/admin/policies?limit=1", "", "admin", nil)
	require(page, 200)
	var list struct {
		Next string `json:"next_cursor"`
	}
	json.Unmarshal(page.Body.Bytes(), &list)
	if list.Next == "" {
		t.Fatal("pagination cursor missing")
	}
	require(call("GET", "/v1/admin/policy-drafts?cursor="+list.Next, "", "admin", nil), 400)
	require(call("GET", "/v1/admin/policies?limit=1&cursor="+list.Next, "", "admin", nil), 200)
	foreign := &TenantRepository{db: f.pool, tenantID: mustNewRepositoryID(t)}
	if _, err := foreign.PolicyDraft(ctx, draft.ID, 0); domain.ErrorCodeOf(err) != domain.CodeNotFound {
		t.Fatalf("cross-tenant draft exposed: %v", err)
	}
	f.service.Clock = editorTestClock{time.Now().UTC()}
	expiryRequest := call("POST", "/v1/admin/policies/"+draft.Digest+"/rollback-approvals", "rollback-expiry-request", "admin", application.RequestRollbackApproval{ExpectedPolicyEpoch: 3, LifetimeSeconds: 1, Reason: "policy.rollback"})
	require(expiryRequest, 201)
	var expiring ports.ApprovalView
	json.Unmarshal(expiryRequest.Body.Bytes(), &expiring)
	f.service.Clock = editorTestClock{f.service.Clock.Now().Add(2 * time.Second)}
	require(call("POST", "/v1/admin/approvals/"+expiring.ID.String()+"/decisions", "rollback-expired-vote", "approver", map[string]bool{"approved": true}), 409)
	expiryView := call("GET", "/v1/admin/approvals/"+expiring.ID.String(), "", "admin", nil)
	require(expiryView, 200)
	if !strings.Contains(expiryView.Body.String(), "EXPIRED") {
		t.Fatal("expired approval not reported")
	}
	if _, err := f.pool.Exec(ctx, `UPDATE local_approval_receipts SET approved=false WHERE tenant_id=$1`, f.tenant.String()); err == nil {
		t.Fatal("mutated authenticated receipt")
	}
}

type editorTestClock struct{ at time.Time }

func (c editorTestClock) Now() time.Time { return c.at }
