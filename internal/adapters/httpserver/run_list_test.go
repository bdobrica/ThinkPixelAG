package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/bdobrica/ThinkPixelAG/internal/adapters/oidc"
	"github.com/bdobrica/ThinkPixelAG/internal/application"
	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/policy"
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type listPolicy struct{ allowed bool }

func (p *listPolicy) Decide(_ context.Context, in policy.Input) (policy.Result, error) {
	return policy.Result{Decision: policy.Decision{DecisionID: in.DecisionID, Allow: p.allowed}, Metadata: policy.Metadata{PolicyDigest: "sha256:" + strings.Repeat("a", 64), PolicyVersion: 1}}, nil
}

type listStore struct {
	ports.PolicyAdministrationStore
	ids   []domain.ID
	calls int
}

func (s *listStore) ListRunIDs(_ context.Context, _ domain.ID, _ int) ([]domain.ID, error) {
	s.calls++
	return s.ids, nil
}

type scopedListQuery struct {
	hidden domain.ID
	seen   int
	run    domain.Run
}

func (s *scopedListQuery) Get(_ context.Context, c application.GetRun) (domain.Run, error) {
	s.seen++
	if c.RunID == s.hidden {
		return domain.Run{}, domain.NewError(domain.CodeNotFound, "run not found")
	}
	r := s.run
	r.ID = c.RunID
	return r, nil
}
func TestRunListPerObjectDenialAndCursorCallerBinding(t *testing.T) {
	tenant, actor := mustHTTPID(t), mustHTTPID(t)
	ids := []domain.ID{mustHTTPID(t), mustHTTPID(t), mustHTTPID(t)}
	p := &listPolicy{allowed: true}
	store := &listStore{ids: ids}
	svc := &application.PolicyAdministration{Store: store, Evaluator: p, Clock: domain.SystemClock{}}
	q := &scopedListQuery{hidden: ids[0], run: domain.Run{AgentID: mustHTTPID(t), VersionDigest: "sha256:" + strings.Repeat("b", 64), CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}}
	v := &fakeVerifier{principal: oidc.Principal{ID: actor.String(), TenantID: tenant.String()}}
	codec, _ := domain.NewCursorCodec(bytes.Repeat([]byte{5}, 32))
	var logs bytes.Buffer
	deps := testDependencies(t, &logs, false)
	deps.RunList = RunListHandler(v, svc, store, q, codec)
	h := newHandler(testConfig(), deps, &Readiness{})
	call := func(path string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", path, nil)
		r.Header.Set("Authorization", "Bearer valid")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	w := call("/v1/runs?limit=2")
	if w.Code != 200 || q.seen != 2 || strings.Contains(w.Body.String(), ids[0].String()) || !strings.Contains(w.Body.String(), ids[1].String()) {
		t.Fatal(w.Code, w.Body, q.seen)
	}
	var page struct {
		Next string `json:"next_cursor"`
	}
	if e := json.Unmarshal(w.Body.Bytes(), &page); e != nil || page.Next == "" {
		t.Fatal(e)
	}
	v.principal.ID = mustHTTPID(t).String()
	if w = call("/v1/runs?cursor=" + page.Next); w.Code != 400 {
		t.Fatal(w.Code, w.Body)
	}
	p.allowed = false
	before := store.calls
	if w = call("/v1/runs"); w.Code != 403 || store.calls != before {
		t.Fatal("denial queried list", w.Code)
	}
}
