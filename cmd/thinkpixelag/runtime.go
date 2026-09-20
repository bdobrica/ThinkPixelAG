package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/adapters/httpserver"
	"github.com/bdobrica/ThinkPixelAG/internal/adapters/integrationconfig"
	"github.com/bdobrica/ThinkPixelAG/internal/adapters/localapprovals"
	"github.com/bdobrica/ThinkPixelAG/internal/adapters/localkeys"
	"github.com/bdobrica/ThinkPixelAG/internal/adapters/mtls"
	"github.com/bdobrica/ThinkPixelAG/internal/adapters/oidc"
	"github.com/bdobrica/ThinkPixelAG/internal/adapters/opa"
	"github.com/bdobrica/ThinkPixelAG/internal/adapters/postgres"
	"github.com/bdobrica/ThinkPixelAG/internal/application"
	"github.com/bdobrica/ThinkPixelAG/internal/config"
	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/observability/metrics"
	"github.com/bdobrica/ThinkPixelAG/internal/policy"
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
)

// runtimeSettings is deployment configuration, never a request-supplied grant.
// Secrets are supplied separately through Secret or mounted TLS files.
type runtimeSettings struct {
	IntegrationsMode     string            `json:"integrations_mode,omitempty"`
	OPAAllowedOrigins    []string          `json:"opa_allowed_origins,omitempty"`
	OPASecretFiles       map[string]string `json:"opa_secret_files,omitempty"`
	RoleMappingsMode     string            `json:"role_mappings_mode,omitempty"`
	LocalPolicyKey       string            `json:"local_policy_key,omitempty"`
	PolicyChannel        string            `json:"policy_channel"`
	AuthorityConstraints map[string]any    `json:"authority_constraints"`
	TrustedAddress       string            `json:"trusted_address"`
	TLSCertificate       string            `json:"tls_certificate"`
	TLSKey               string            `json:"tls_key"`
	ClientCA             string            `json:"client_ca"`
	WorkloadBindings     string            `json:"workload_bindings"`
}

func readRuntimeSettings(path string) (runtimeSettings, error) {
	var c runtimeSettings
	if err := readRuntimeJSON(path, &c); err != nil {
		return c, err
	}
	if c.IntegrationsMode == "" {
		c.IntegrationsMode = "file"
	}
	if c.IntegrationsMode != "file" && c.IntegrationsMode != "api" {
		return c, errors.New("invalid integrations mode")
	}
	if c.IntegrationsMode == "api" && (c.LocalPolicyKey == "" || len(c.OPAAllowedOrigins) == 0) {
		return c, errors.New("API integrations require signed administration and destination allowlist")
	}
	if c.RoleMappingsMode == "" {
		c.RoleMappingsMode = "file"
	}
	if c.RoleMappingsMode != "file" && c.RoleMappingsMode != "api" {
		return c, errors.New("invalid role mappings mode")
	}
	if c.RoleMappingsMode == "api" && c.LocalPolicyKey == "" {
		return c, errors.New("API role mappings require signed administration profile")
	}
	if c.PolicyChannel == "" || len(c.PolicyChannel) > 128 || len(c.AuthorityConstraints) == 0 {
		return c, errors.New("runtime policy channel and admission ceilings are required")
	}
	allowed := map[string]bool{"max_execution_time_seconds": true, "max_budget_usd_microunits": true, "max_llm_tokens": true, "max_tool_calls": true, "max_tool_calls_per_minute": true, "max_active_children": true, "max_total_children": true, "max_delegation_depth": true}
	for k, v := range c.AuthorityConstraints {
		n, ok := v.(json.Number)
		i, err := n.Int64()
		if !ok || err != nil || !allowed[k] || i < 0 || i > 1<<53 || (k == "max_execution_time_seconds" && (i < 1 || i > 604800)) {
			return c, errors.New("runtime admission ceiling is invalid")
		}
	}
	trusted := c.TrustedAddress != "" || c.TLSCertificate != "" || c.TLSKey != "" || c.ClientCA != "" || c.WorkloadBindings != ""
	if trusted && (c.TrustedAddress == "" || c.TLSCertificate == "" || c.TLSKey == "" || c.ClientCA == "" || c.WorkloadBindings == "") {
		return c, errors.New("trusted runtime requires address, certificate, key, client CA and workload bindings")
	}
	if trusted {
		if _, _, err := net.SplitHostPort(c.TrustedAddress); err != nil {
			return c, errors.New("trusted runtime address is invalid")
		}
	}
	return c, nil
}

func readRuntimeJSON(path string, out any) error {
	f, err := os.Open(path)
	if err != nil {
		return errors.New("runtime configuration cannot be opened")
	}
	defer f.Close()
	body, err := io.ReadAll(io.LimitReader(f, (4<<20)+1))
	if err != nil || len(body) > 4<<20 {
		return errors.New("runtime configuration exceeds bounds")
	}
	d := json.NewDecoder(bytes.NewReader(body))
	d.DisallowUnknownFields()
	d.UseNumber()
	if d.Decode(out) != nil {
		return errors.New("runtime configuration is invalid")
	}
	if d.Decode(new(any)) != io.EOF {
		return errors.New("runtime configuration has trailing data")
	}
	return nil
}

func trustedTransport(c runtimeSettings) (*tls.Config, *mtls.Verifier, error) {
	var doc struct {
		Version  string `json:"contract_version"`
		Bindings []struct {
			URI       string   `json:"uri_san"`
			Principal string   `json:"principal_id"`
			Tenant    string   `json:"tenant_id"`
			Roles     []string `json:"roles"`
		} `json:"bindings"`
	}
	if err := readRuntimeJSON(c.WorkloadBindings, &doc); err != nil {
		return nil, nil, err
	}
	if doc.Version != "thinkpixelag.workload-identity/v1" || len(doc.Bindings) < 1 || len(doc.Bindings) > 10000 {
		return nil, nil, errors.New("workload identity contract is invalid")
	}
	bindings := make([]mtls.Binding, 0, len(doc.Bindings))
	for _, b := range doc.Bindings {
		if _, err := domain.ParseID(b.Principal); err != nil {
			return nil, nil, errors.New("workload principal is invalid")
		}
		if _, err := domain.ParseID(b.Tenant); err != nil {
			return nil, nil, errors.New("workload tenant is invalid")
		}
		bindings = append(bindings, mtls.Binding{URISAN: b.URI, PrincipalID: b.Principal, TenantID: b.Tenant, Roles: b.Roles})
	}
	verifier, err := mtls.New(bindings)
	if err != nil {
		return nil, nil, err
	}
	cert, err := tls.LoadX509KeyPair(c.TLSCertificate, c.TLSKey)
	if err != nil {
		return nil, nil, errors.New("trusted listener certificate could not be loaded")
	}
	ca, err := os.ReadFile(c.ClientCA)
	if err != nil {
		return nil, nil, errors.New("trusted listener CA could not be loaded")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(ca) {
		return nil, nil, errors.New("trusted listener CA is invalid")
	}
	return &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{cert}, ClientCAs: roots, ClientAuth: tls.RequireAndVerifyClientCert}, verifier, nil
}

// Each request binds its repository only after authentication. There is no
// unbounded per-tenant handler cache and no tenant hint taken from a body/header.
type runtimeRoutes struct {
	readiness    interface{ Ready(context.Context) error }
	settings     config.Config
	runtime      runtimeSettings
	repositories *postgres.Repositories
	policies     *policy.Freshness
	verifier     oidc.Verifier
	workload     httpserver.WorkloadVerifier
	clock        domain.Clock
	metrics      *metrics.Metrics
	client       *http.Client
	accelerator  ports.ThroughputAccelerator
	localKey     *localkeys.Key
}

func (r *runtimeRoutes) mount(d *httpserver.Dependencies, trusted bool) {
	if trusted {
		d.TrustedUsage = r.route("usage", true)
		d.ResourceSettlement = r.route("settlement", true)
		d.RevocationDistribution = r.route("distribution", true)
	} else {
		if r.localKey != nil {
			d.PolicyAdministration = r.route("policy-admin", false)
			d.RegistryAdministration = r.route("registry-admin", false)
			d.RunList = r.route("run-list", false)
			d.Integrations = r.route("integrations", false)
			d.RoleMappings = r.route("role-mappings", false)
			d.PolicyEditor = r.route("policy-editor", false)
		}
		d.HarnessGuidance = r.route("harness-guidance", false)
		d.AgentDiscovery = r.route("discovery", false)
		d.AgentApprovals = r.route("approval", false)
		d.RunAdmission = r.route("admission", false)
		d.RunQuery = r.route("query", false)
		d.RunSignal = r.route("signal", false)
		d.RunCancellation = r.route("cancel", false)
		d.RunEvents = r.route("events", false)
		d.ResourceExtension = r.route("extension", false)
		d.Revocations = r.route("revocation", false)
	}
}

func (r *runtimeRoutes) route(name string, trusted bool) http.Handler {
	next := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if r.readiness != nil {
			if err := r.readiness.Ready(req.Context()); err != nil {
				httpserver.WriteError(w, req, domain.NewError(domain.CodeUnavailable, "security state is not ready").WithRetryable())
				return
			}
		}
		p, ok := httpserver.PrincipalFromContext(req.Context())
		tenant, err := domain.ParseID(p.TenantID)
		if !ok || err != nil {
			httpserver.WriteError(w, req, domain.NewError(domain.CodeUnauthenticated, "authenticated tenant identifier is invalid"))
			return
		}
		repository, err := r.repositories.ForTenant(tenant)
		if err != nil {
			httpserver.WriteError(w, req, err)
			return
		}
		handler, err := r.handler(req.Context(), name, tenant, repository)
		if err != nil {
			httpserver.WriteError(w, req, err)
			return
		}
		handler.ServeHTTP(w, req)
	})
	if trusted {
		return httpserver.AuthenticateWorkload(r.workload, next)
	}
	return httpserver.AuthenticateBearer(r.verifier, next)
}

func (r *runtimeRoutes) handler(ctx context.Context, name string, tenant domain.ID, repo *postgres.TenantRepository) (http.Handler, error) {
	idempotency, err := postgres.NewIdempotencyStore(r.repositories)
	if err != nil {
		return nil, err
	}
	active := func() (string, int64, bool) {
		a, ok := r.policies.Get(tenant.String(), r.runtime.PolicyChannel)
		return a.Digest, a.Version, ok
	}
	client, err := opa.New(r.settings.OPA.URL, r.settings.OPA.DecisionPath, r.settings.OPA.Timeout, r.settings.OPA.DecisionMaxTTL, r.settings.OPA.BearerToken.Value(), r.client, active)
	if err != nil {
		return nil, err
	}
	var base policy.Evaluator = client
	modules := &opa.Modules{Base: r.settings.OPA.URL, Token: r.settings.OPA.BearerToken.Value(), Client: r.client, Timeout: r.settings.OPA.Timeout}
	if r.runtime.IntegrationsMode == "api" {
		config, e := repo.OPAIntegration(ctx)
		if e != nil {
			return nil, e
		}
		modules, e = r.integrationAdapter(tenant, repo).Modules(config.Connection)
		if e != nil {
			return nil, e
		}
	}
	if r.localKey != nil {
		base = &opa.ArtifactEvaluator{Store: repo, Modules: modules, Verifier: r.localKey, Channel: r.runtime.PolicyChannel, MaxTTL: r.settings.OPA.DecisionMaxTTL}
	}
	evaluator, err := policy.NewRevocationEvaluator(&measuredPolicy{base, r.metrics}, r.repositories, r.clock.Now)
	if err != nil {
		return nil, err
	}
	// This initial composition deliberately uses the live authority on every
	// operation. Optional decision caching must not bypass this check.
	live := policy.SecurityState{Authoritative: true}
	key := func(purpose string) []byte {
		m := hmac.New(sha256.New, []byte(r.settings.CursorKey.Value()))
		_, _ = m.Write([]byte(purpose))
		return m.Sum(nil)
	}
	switch name {
	case "harness-guidance":
		q, e := application.NewRunQuery(repo, evaluator, r.clock)
		if e != nil {
			return nil, e
		}
		return httpserver.HarnessGuidanceHandler(r.verifier, &application.HarnessGuidance{State: &harnessState{r, repo}, Evaluator: evaluator, Runs: q, Clock: r.clock, RevisionKey: key("harness-guidance")}), nil
	case "policy-admin", "policy-editor", "role-mappings", "integrations", "registry-admin", "run-list":
		s := &application.PolicyAdministration{Store: repo, Evaluator: evaluator, Modules: modules, Verifier: r.localKey, Signer: r.localKey, SigningKeyID: r.localKey.ID(), ApprovalProvider: &localapprovals.Provider{Store: repo}, Channel: r.runtime.PolicyChannel, Clock: r.clock}
		if name == "registry-admin" {
			return httpserver.RegistryAdministrationHandler(r.verifier, &application.RegistryAdministration{Policy: s, Store: repo}), nil
		}
		if name == "run-list" {
			q, e := application.NewRunQuery(repo, evaluator, r.clock)
			if e != nil {
				return nil, e
			}
			codec, e := domain.NewCursorCodec(key("run-pages"))
			if e != nil {
				return nil, e
			}
			return httpserver.RunListHandler(r.verifier, s, repo, q, codec), nil
		}
		if name == "integrations" {
			mode := r.runtime.IntegrationsMode
			if mode == "" {
				mode = "file"
			}
			return httpserver.IntegrationsHandler(r.verifier, &application.IntegrationAdministration{Policy: s, Store: repo, Mode: mode, File: ports.OPAConnection{Endpoint: r.settings.OPA.URL}, Checker: r.integrationAdapter(tenant, repo)}), nil
		}
		if name == "role-mappings" {
			mode := r.runtime.RoleMappingsMode
			if mode == "" {
				mode = "file"
			}
			return httpserver.RoleMappingsHandler(r.verifier, &application.RoleMappingAdministration{Policy: s, Store: repo, Mode: mode, Issuer: strings.TrimSuffix(r.settings.OIDC.IssuerURL, "/"), File: oidc.FileMappings(r.settings.OIDC.RoleMappings)}), nil
		}
		if name == "policy-admin" {
			return httpserver.PolicyAdministrationHandler(r.verifier, s), nil
		}
		codec, e := domain.NewCursorCodec(key("policy-administration-pages"))
		if e != nil {
			return nil, e
		}
		return httpserver.PolicyEditorHandler(r.verifier, s, codec), nil
	case "discovery":
		s, e := application.NewAgentDiscovery(repo, evaluator, r.clock)
		if e != nil {
			return nil, e
		}
		codec, e := domain.NewCursorCodec(key("agent-pages"))
		if e != nil {
			return nil, e
		}
		return httpserver.AgentDiscoveryHandler(r.verifier, &liveDiscovery{s}, codec)
	case "approval":
		a, e := application.NewPolicyAgentApprovalAuthorizer(evaluator, r.clock.Now)
		if e != nil {
			return nil, e
		}
		s, e := application.NewAgentApprovalRegistry(repo, a, r.clock)
		if e != nil {
			return nil, e
		}
		return httpserver.AgentApprovalHandler(r.verifier, s, func() (string, error) { id, e := domain.NewID(); return id.String(), e })
	case "admission":
		resolver, e := application.NewVersionResolver(repo, evaluator, r.clock)
		if e != nil {
			return nil, e
		}
		s, e := application.NewRunAdmissionService(resolver, repo, r.clock)
		if e != nil {
			return nil, e
		}
		return httpserver.RunAdmissionHandler(r.verifier, &measuredAdmission{s, r.metrics}, idempotency, r.clock, httpserver.RunAdmissionHTTPConfig{AuthorityConstraints: r.runtime.AuthorityConstraints, SecurityState: live, Lease: time.Minute, TTL: 24 * time.Hour})
	case "query", "events":
		s, e := application.NewRunQuery(repo, evaluator, r.clock)
		if e != nil {
			return nil, e
		}
		if name == "query" {
			return httpserver.RunQueryHandler(r.verifier, s, live)
		}
		stream, e := application.NewRunEventStream(s, repo, 100000)
		if e != nil {
			return nil, e
		}
		codec, e := domain.NewRunEventCursorCodec(key("run-events"))
		if e != nil {
			return nil, e
		}
		return httpserver.RunEventStreamHandler(r.verifier, stream, codec, live, httpserver.RunEventStreamOptions{HeartbeatInterval: 15 * time.Second, PollInterval: time.Second, WriteTimeout: 5 * time.Second})
	case "signal":
		s, e := application.NewRunSignalService(repo, evaluator, r.clock)
		if e != nil {
			return nil, e
		}
		return httpserver.RunSignalHandler(r.verifier, s, idempotency, r.clock, httpserver.RunSignalHTTPConfig{SecurityState: live, Lease: time.Minute, TTL: 24 * time.Hour})
	case "cancel":
		s, e := application.NewRunCancellationService(repo, evaluator, r.clock)
		if e != nil {
			return nil, e
		}
		return httpserver.RunCancellationHandler(r.verifier, s, idempotency, r.clock, httpserver.RunCancellationHTTPConfig{SecurityState: live, Lease: time.Minute, TTL: 24 * time.Hour})
	case "extension":
		s, e := application.NewResourceExtensionService(repo, evaluator, r.clock)
		if e != nil {
			return nil, e
		}
		return httpserver.ResourceExtensionHandler(r.verifier, s, httpserver.ResourceExtensionHTTPConfig{SecurityState: live})
	case "revocation":
		s, e := application.NewRevocationService(repo, evaluator, r.clock)
		if e != nil {
			return nil, e
		}
		return httpserver.RevocationHandler(r.verifier, s, httpserver.RevocationHTTPConfig{SecurityState: live})
	case "usage":
		s, e := application.NewTrustedUsageService(repo, evaluator, r.clock, r.accelerator)
		if e != nil {
			return nil, e
		}
		return httpserver.TrustedUsageHandler(r.workload, s, httpserver.TrustedUsageHTTPConfig{SecurityState: live})
	case "settlement":
		s, e := application.NewResourceSettlementService(repo, evaluator, r.clock)
		if e != nil {
			return nil, e
		}
		return httpserver.ResourceSettlementHandler(r.workload, s, httpserver.ResourceSettlementHTTPConfig{SecurityState: live})
	case "distribution":
		s, e := application.NewRevocationDistribution(r.repositories, evaluator, r.clock, 24*time.Hour)
		if e != nil {
			return nil, e
		}
		codec, e := domain.NewRevocationCursorCodec(key("revocations"))
		if e != nil {
			return nil, e
		}
		return httpserver.RevocationDistributionHandler(r.workload, s, codec, httpserver.RevocationStreamOptions{HeartbeatInterval: 15 * time.Second, PollInterval: time.Second, WriteTimeout: 5 * time.Second})
	}
	return nil, errors.New("unknown runtime route")
}

type liveDiscovery struct{ *application.AgentDiscovery }

func (s *liveDiscovery) List(ctx context.Context, c application.DiscoverAgents) (application.AgentPage, error) {
	c.SecurityState = policy.SecurityState{Authoritative: true}
	return s.AgentDiscovery.List(ctx, c)
}
func (s *liveDiscovery) Describe(ctx context.Context, c application.DiscoverAgents, id domain.ID) (domain.Agent, error) {
	c.SecurityState = policy.SecurityState{Authoritative: true}
	return s.AgentDiscovery.Describe(ctx, c, id)
}

type measuredPolicy struct {
	next    policy.Evaluator
	metrics *metrics.Metrics
}

func (m *measuredPolicy) Decide(ctx context.Context, in policy.Input) (policy.Result, error) {
	start := time.Now()
	v, e := m.next.Decide(ctx, in)
	m.metrics.ObservePolicyDuration(time.Since(start))
	outcome := "deny"
	if e != nil {
		outcome = "unavailable"
	} else if v.Decision.Allow {
		outcome = "allow"
	}
	m.metrics.ObservePolicyDecision(outcome)
	return v, e
}

type measuredAdmission struct {
	*application.RunAdmissionService
	metrics *metrics.Metrics
}

func (m *measuredAdmission) Admit(ctx context.Context, c application.AdmitRun) (domain.RunAdmission, error) {
	v, e := m.RunAdmissionService.Admit(ctx, c)
	outcome := "admitted"
	if e != nil {
		outcome = "error"
		var d *domain.Error
		if errors.As(e, &d) {
			if d.Code() == domain.CodeForbidden {
				outcome = "denied"
			} else if d.Code() == domain.CodeConflict {
				outcome = "conflict"
			}
		}
	}
	m.metrics.ObserveRunAdmission(outcome)
	return v, e
}

// Bound dependency sockets independently of incoming request/stream counts.
func policyHTTPClient(timeout time.Duration) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxConnsPerHost = 32
	transport.MaxIdleConnsPerHost = 32
	transport.MaxIdleConns = 32
	transport.ResponseHeaderTimeout = timeout
	return &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func (m *measuredAdmission) AdmitIdempotent(ctx context.Context, c application.AdmitRun, acquisition ports.IdempotencyAcquisition, encode ports.RunAdmissionResponseEncoder) (ports.IdempotencyResponse, error) {
	response, err := m.RunAdmissionService.AdmitIdempotent(ctx, c, acquisition, encode)
	outcome := "admitted"
	if err != nil {
		outcome = "error"
		var d *domain.Error
		if errors.As(err, &d) {
			if d.Code() == domain.CodeForbidden {
				outcome = "denied"
			} else if d.Code() == domain.CodeConflict {
				outcome = "conflict"
			}
		}
	}
	m.metrics.ObserveRunAdmission(outcome)
	return response, err
}

func (r *runtimeRoutes) integrationAdapter(tenant domain.ID, repo *postgres.TenantRepository) *integrationconfig.OPA {
	origins := r.runtime.OPAAllowedOrigins
	token := ""
	if r.runtime.IntegrationsMode != "api" {
		origins = []string{r.settings.OPA.URL}
		token = r.settings.OPA.BearerToken.Value()
	}
	return &integrationconfig.OPA{AllowedOrigins: origins, DefaultToken: token, SecretFiles: r.runtime.OPASecretFiles, Client: r.client, Timeout: min(r.settings.OPA.Timeout, 5*time.Second), MaxTTL: r.settings.OPA.DecisionMaxTTL, Store: repo, Verifier: r.localKey, Channel: r.runtime.PolicyChannel, Tenant: tenant}
}
