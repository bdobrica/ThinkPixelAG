package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLoadRejectsDuplicateRunsAndDoesNotCountErrorsAsSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(201)
		_, _ = w.Write([]byte(`{"id":"same-run"}`))
	}))
	defer server.Close()
	r := &runner{base: server.URL, client: server.Client(), seen: map[string]bool{}, report: report{Statuses: map[int]int64{}}}
	r.operation("admission")
	r.operation("admission")
	if r.report.Completed != 2 || r.report.Success != 2 || r.report.InvariantFailures != 1 {
		t.Fatalf("duplicate detection: %+v", r.report)
	}
	denied := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }))
	defer denied.Close()
	r.base = denied.URL
	r.operation("admission")
	if r.report.Completed != 3 || r.report.Success != 2 || r.report.Statuses[503] != 1 {
		t.Fatalf("failure accounting: %+v", r.report)
	}
}
