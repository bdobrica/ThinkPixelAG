package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
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

func TestLoadMeasuresTheEntireScheduledWindow(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	defer server.Close()
	r := &runner{base: server.URL, client: server.Client(), report: report{Statuses: map[int]int64{}}}
	// A single request at the beginning must not make reported throughput
	// depend only on that request's latency; fractional rates use the same path.
	r.load("read", 5, 1, 200*time.Millisecond)
	if r.report.Offered != 1 || r.report.Success != 1 || r.report.Dropped != 0 || r.report.DurationSeconds < .2 {
		t.Fatalf("measurement window truncated: %+v", r.report)
	}
}
