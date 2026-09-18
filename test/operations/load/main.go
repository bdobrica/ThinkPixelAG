// Command load drives governed HTTP APIs from outside the cluster. Reports
// contain aggregate measurements only; credentials and response bodies are not
// logged. Client latency includes transport and is separate from server SLOs.
package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
)

type identity struct{ Tenant, Principal, Admin, Agent string }
type report struct {
	LateDropped, BusyDropped                                                 int64
	CompleteStreams                                                          int64
	StreamSetupSeconds                                                       float64
	Mode                                                                     string    `json:"mode"`
	Started                                                                  time.Time `json:"started"`
	DurationSeconds                                                          float64   `json:"duration_seconds"`
	Offered, Completed, Success, Dropped, TransportErrors, InvariantFailures int64
	Statuses                                                                 map[int]int64
	Throughput, P50MS, P95MS, P99MS                                          float64
	StreamClients, PeakStreams, StreamEvents, Reconnects, StreamGaps         int64
	LagP99MS                                                                 float64
}
type runner struct {
	afterSequence                                  int64
	received                                       []int64
	base, trusted, dir, token, admin, foreign, run string
	id                                             identity
	client                                         *http.Client
	mu                                             sync.Mutex
	report                                         report
	latencies                                      []float64
	seen                                           map[string]bool
	lag                                            [6002]int64
	active                                         atomic.Int64
}

func main() {
	var base, trusted, dir, mode string
	var rate float64
	var workers, clients int
	var duration time.Duration
	var afterSequence int64
	flag.StringVar(&base, "base", "", "public API URL")
	flag.StringVar(&trusted, "trusted", "", "trusted API URL")
	flag.StringVar(&dir, "identities", "", "private fixture directory")
	flag.StringVar(&mode, "mode", "smoke", "smoke, admission, read, signal, revocation, streams")
	flag.Float64Var(&rate, "rate", 10, "scheduled requests/second (revocations during streams)")
	flag.IntVar(&workers, "workers", 64, "bounded HTTP concurrency")
	flag.IntVar(&clients, "clients", 100, "distinct mTLS stream clients")
	flag.DurationVar(&duration, "duration", time.Minute, "measurement duration")
	flag.Int64Var(&afterSequence, "after-sequence", 0, "initial revocation sequence; reconcile before a live fanout measurement")
	flag.Parse()
	if afterSequence < 0 || base == "" || dir == "" || math.IsNaN(rate) || rate < 0.01 || rate > 10000 || rate*duration.Seconds() < 1 || workers < 1 || workers > 1024 || clients < 1 || clients > 5000 || duration <= 0 || duration > 10*time.Minute {
		fatal(errors.New("invalid load configuration"))
	}
	r := &runner{base: strings.TrimRight(base, "/"), trusted: strings.TrimRight(trusted, "/"), dir: dir, seen: map[string]bool{}, report: report{Mode: mode, Started: time.Now().UTC(), Statuses: map[int]int64{}}}
	b, e := os.ReadFile(filepath.Join(dir, "identity.json"))
	if e != nil {
		fatal(e)
	}
	if e = json.Unmarshal(b, &r.id); e != nil {
		fatal(e)
	}
	for name, target := range map[string]*string{"caller.token": &r.token, "admin.token": &r.admin, "foreign.token": &r.foreign} {
		b, e = os.ReadFile(filepath.Join(dir, name))
		if e != nil {
			fatal(e)
		}
		*target = string(b)
	}
	r.afterSequence = afterSequence
	r.client = &http.Client{Timeout: 20 * time.Second, Transport: &http.Transport{MaxIdleConns: workers * 2, MaxIdleConnsPerHost: workers, MaxConnsPerHost: workers, ResponseHeaderTimeout: 15 * time.Second}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	if e = r.smoke(); e != nil {
		r.report.InvariantFailures++
		_ = json.NewEncoder(os.Stdout).Encode(r.report)
		fatal(e)
	}
	if mode == "smoke" {
		fmt.Println(`{"mode":"smoke","result":"passed","checks":["admission","idempotent_replay","cross_tenant_denial","forged_token_denial","run_read"]}`)
		return
	}
	r.report.Started = time.Now().UTC()
	switch mode {
	case "admission", "read", "signal", "revocation":
		r.load(mode, rate, workers, duration)
	case "streams":
		if e = r.streams(clients, rate, workers, duration); e != nil {
			fatal(e)
		}
	default:
		fatal(errors.New("unknown mode"))
	}
	r.mu.Lock()
	sort.Float64s(r.latencies)
	r.report.P50MS = percentile(r.latencies, .5)
	r.report.P95MS = percentile(r.latencies, .95)
	r.report.P99MS = percentile(r.latencies, .99)
	r.report.Throughput = float64(r.report.Success) / r.report.DurationSeconds
	var total int64
	for _, n := range r.lag {
		total += n
	}
	if total > 0 {
		threshold := int64(math.Ceil(float64(total) * .99))
		var n int64
		for j, v := range r.lag {
			n += v
			if n >= threshold {
				r.report.LagP99MS = float64(j * 10)
				break
			}
		}
	}
	_ = json.NewEncoder(os.Stdout).Encode(r.report)
	r.mu.Unlock()
	if r.report.InvariantFailures > 0 {
		os.Exit(2)
	}
	if r.report.Success != r.report.Offered || r.report.Dropped > 0 || r.report.TransportErrors > 0 || r.report.StreamGaps > 0 || (mode == "streams" && (r.report.PeakStreams < int64(clients) || r.report.Reconnects < int64(clients/4) || r.report.CompleteStreams < int64(clients))) {
		os.Exit(1)
	}
}
func fatal(e error) { fmt.Fprintln(os.Stderr, "qualification failed:", e); os.Exit(1) }
func percentile(v []float64, p float64) float64 {
	if len(v) == 0 {
		return 0
	}
	return v[min(len(v)-1, int(float64(len(v))*p))]
}
func id() string {
	v, e := domain.NewID()
	if e != nil {
		panic("ID generation failed")
	}
	return v.String()
}
func (r *runner) request(ctx context.Context, method, path, token, key string, body []byte) (int, []byte, error) {
	req, e := http.NewRequestWithContext(ctx, method, r.base+path, bytes.NewReader(body))
	if e != nil {
		return 0, nil, e
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	resp, e := r.client.Do(req)
	if e != nil {
		return 0, nil, errors.New("HTTP transport failed")
	}
	defer resp.Body.Close()
	b, e := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode, b, e
}
func (r *runner) admissionBody() []byte {
	b, _ := json.Marshal(map[string]any{"objective": "OPS010 synthetic workload", "input": map[string]string{"synthetic": strings.Repeat("x", 1024)}, "constraints": map[string]int{"max_execution_time_seconds": 3600, "max_llm_tokens": 1000, "max_tool_calls": 100, "max_active_children": 10, "max_total_children": 100, "max_delegation_depth": 1}})
	return b
}
func (r *runner) smoke() error {
	ctx := context.Background()
	key := "ops010-" + id()
	status, b, e := r.request(ctx, "POST", "/v1/agents/"+r.id.Agent+"/runs", r.token, key, r.admissionBody())
	if e != nil || status != 201 {
		return fmt.Errorf("admission smoke status=%d", status)
	}
	var v struct{ ID string }
	if json.Unmarshal(b, &v) != nil || v.ID == "" {
		return errors.New("invalid admission response")
	}
	r.run = v.ID
	status, replay, e := r.request(ctx, "POST", "/v1/agents/"+r.id.Agent+"/runs", r.token, key, r.admissionBody())
	if e != nil || status != 201 || !bytes.Equal(b, replay) {
		return errors.New("idempotent replay mismatch")
	}
	for _, check := range []struct {
		token  string
		status int
	}{{r.token, 200}, {r.foreign, 404}, {"forged", 401}} {
		status, _, e = r.request(ctx, "GET", "/v1/runs/"+r.run, check.token, "", nil)
		if e != nil || status != check.status {
			return fmt.Errorf("read/isolation smoke expected=%d actual=%d", check.status, status)
		}
	}
	return nil
}
func (r *runner) operation(mode string) {
	start := time.Now()
	method, path, token, key, expected := "GET", "/v1/runs/"+r.run, r.token, "", 200
	var body []byte
	switch mode {
	case "admission":
		method, path, key, expected = "POST", "/v1/agents/"+r.id.Agent+"/runs", "ops010-"+id(), 201
		body = r.admissionBody()
	case "signal":
		method, path, key, expected = "POST", "/v1/runs/"+r.run+"/signals", "ops010-"+id(), 202
		body = []byte(`{"type":"CUSTOM","payload":{"name":"ops010.sample","data":{}}}`)
	case "revocation":
		method, path, token, expected = "POST", "/v1/admin/revocations", r.admin, 201
		now := time.Now().UTC()
		body, _ = json.Marshal(map[string]any{"scope": "PRINCIPAL_ID", "target": id(), "reason_code": "security.compromise", "effective_at": now, "expires_at": now.Add(10 * time.Second), "approval_reference": "ops010-synthetic"})
	}
	status, b, e := r.request(context.Background(), method, path, token, key, body)
	latency := float64(time.Since(start)) / float64(time.Millisecond)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.report.Completed++
	r.latencies = append(r.latencies, latency)
	if e != nil {
		r.report.TransportErrors++
		return
	}
	r.report.Statuses[status]++
	if status != expected {
		return
	}
	r.report.Success++
	if mode == "admission" {
		var response struct{ ID string }
		if json.Unmarshal(b, &response) != nil || response.ID == "" || r.seen[response.ID] {
			r.report.InvariantFailures++
		}
		r.seen[response.ID] = true
	}
}
func (r *runner) load(mode string, rate float64, workers int, duration time.Duration) {
	start := time.Now()
	slots := make(chan struct{}, workers)
	var wg sync.WaitGroup
	total := int64(duration.Seconds() * float64(rate))
	r.mu.Lock()
	r.report.Offered = total
	r.mu.Unlock()
	for n := int64(0); n < total; n++ {
		due := start.Add(time.Duration(float64(n) * float64(time.Second) / rate))
		if wait := time.Until(due); wait > 0 {
			time.Sleep(wait)
		}
		if time.Since(due) > max(100*time.Millisecond, time.Duration(float64(time.Second)/rate)) {
			r.mu.Lock()
			r.report.Dropped++
			r.report.LateDropped++
			r.mu.Unlock()
			continue
		}
		select {
		case slots <- struct{}{}:
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer func() { <-slots }()
				r.operation(mode)
			}()
		default:
			r.mu.Lock()
			r.report.Dropped++
			r.report.BusyDropped++
			r.mu.Unlock()
		}
	}
	wg.Wait()
	if remaining := time.Until(start.Add(duration)); remaining > 0 {
		time.Sleep(remaining)
	}
	r.mu.Lock()
	r.report.DurationSeconds = time.Since(start).Seconds()
	r.mu.Unlock()
}
func (r *runner) streams(clients int, rate float64, workers int, duration time.Duration) error {
	setupStarted := time.Now()
	r.received = make([]int64, clients)
	if r.trusted == "" {
		return errors.New("trusted URL required")
	}
	roots := x509.NewCertPool()
	ca, e := os.ReadFile(filepath.Join(r.dir, "ca.crt"))
	if e != nil || !roots.AppendCertsFromPEM(ca) {
		return errors.New("test CA unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), duration+5*time.Minute)
	defer cancel()
	var wg sync.WaitGroup
	connected := make(chan struct{}, clients)
	sem := make(chan struct{}, 32)
	reconnect := make(chan struct{})
	var errorsCount atomic.Int64
	for n := 0; n < clients; n++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			sem <- struct{}{}

			cert, e := tls.LoadX509KeyPair(filepath.Join(r.dir, "gateway-"+strconv.Itoa(n)+".crt"), filepath.Join(r.dir, "gateway-"+strconv.Itoa(n)+".key"))
			if e != nil {
				<-sem
				errorsCount.Add(1)
				connected <- struct{}{}
				return
			}
			transport := &http.Transport{DialContext: (&net.Dialer{Timeout: 5 * time.Second}).DialContext, TLSHandshakeTimeout: 5 * time.Second, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots, Certificates: []tls.Certificate{cert}, ServerName: "ops-api"}, ResponseHeaderTimeout: 20 * time.Second}
			defer transport.CloseIdleConnections()
			client := &http.Client{Transport: transport}
			cursor := ""
			last := r.afterSequence
			var lastEpoch domain.EpochVector
			streamCtx, stop := context.WithCancel(ctx)
			defer stop()
			if n < clients/4 {
				go func() {
					select {
					case <-reconnect:
						stop()
					case <-ctx.Done():
					}
				}()
			}
			first := true
			for {
				req, _ := http.NewRequestWithContext(streamCtx, "GET", r.trusted+"/v1/trusted/revocations/events?after_sequence="+strconv.FormatInt(r.afterSequence, 10), nil)
				if cursor != "" {
					req.Header.Set("Last-Event-ID", cursor)
				}
				resp, e := client.Do(req)
				if first {
					<-sem
					first = false
					connected <- struct{}{}
				}
				if e != nil || resp.StatusCode != 200 {
					if resp != nil {
						resp.Body.Close()
					}
					errorsCount.Add(1)
					return
				}
				active := r.active.Add(1)
				r.mu.Lock()
				r.report.PeakStreams = max(r.report.PeakStreams, active)
				r.mu.Unlock()
				scanner := bufio.NewScanner(resp.Body)
				scanner.Buffer(make([]byte, 4096), 1<<20)
				eventCursor := ""
				eventType := ""
				eventData := ""
				for scanner.Scan() {
					line := scanner.Text()
					if strings.HasPrefix(line, "event: ") {
						eventType = strings.TrimPrefix(line, "event: ")
					}
					if strings.HasPrefix(line, "id: ") {
						eventCursor = strings.TrimPrefix(line, "id: ")
					}
					if strings.HasPrefix(line, "data: ") {
						eventData = strings.TrimPrefix(line, "data: ")
					}
					if line == "" && eventData != "" {
						line = "data: " + eventData
						eventData = ""
						if eventType == "gap" {
							r.mu.Lock()
							r.report.StreamGaps++
							r.mu.Unlock()
							break
						}
						var event struct {
							Sequence int64              `json:"sequence"`
							Epochs   domain.EpochVector `json:"epochs"`
							Occurred time.Time          `json:"occurred_at"`
						}
						if json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &event) != nil || event.Sequence <= last || event.Epochs.Security < lastEpoch.Security || event.Epochs.TenantRevocation < lastEpoch.TenantRevocation || event.Epochs.TenantPolicy < lastEpoch.TenantPolicy || event.Epochs.AgentRevocation < lastEpoch.AgentRevocation {
							r.mu.Lock()
							r.report.InvariantFailures++
							r.mu.Unlock()
							break
						}
						last = event.Sequence
						lastEpoch = event.Epochs
						cursor = eventCursor
						lag := time.Since(event.Occurred).Milliseconds() / 10
						if lag < 0 {
							lag = 0
						}
						lag = min(lag, 6001)
						r.mu.Lock()
						r.report.StreamEvents++
						r.received[n]++
						r.lag[lag]++
						r.mu.Unlock()
					}
				}
				resp.Body.Close()
				r.active.Add(-1)
				if ctx.Err() != nil {
					return
				}
				select {
				case <-reconnect:
					if n < clients/4 && streamCtx.Err() != nil {
						streamCtx = ctx
						r.mu.Lock()
						r.report.Reconnects++
						r.mu.Unlock()
						continue
					}
				default:
				}
				errorsCount.Add(1)
				return
			}
		}(n)
	}
	for n := 0; n < clients; n++ {
		<-connected
	}
	r.mu.Lock()
	r.report.StreamSetupSeconds = time.Since(setupStarted).Seconds()
	r.report.StreamClients = int64(clients)
	r.mu.Unlock()
	go func() {
		select {
		case <-time.After(duration / 2):
			close(reconnect)
		case <-ctx.Done():
		}
	}()
	r.load("revocation", rate, workers, duration)
	// Allow the final committed change to reach each receiver before disconnect.
	for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline) && ctx.Err() == nil; {
		r.mu.Lock()
		complete := int64(0)
		for _, count := range r.received {
			if r.report.Success > 0 && count >= r.report.Success {
				complete++
			}
		}
		r.report.CompleteStreams = complete
		r.mu.Unlock()
		if complete == int64(clients) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	cancel()
	wg.Wait()
	r.mu.Lock()
	r.report.TransportErrors += errorsCount.Load()
	r.mu.Unlock()
	return nil
}
