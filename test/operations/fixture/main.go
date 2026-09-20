// Command fixture supplies isolated test identities and a durable evidence sink.
// It is never included in the production image. All key material is generated
// into an operator-selected private directory, never embedded in source.
package main

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/adapters/postgres"
	"github.com/bdobrica/ThinkPixelAG/internal/artifact"
	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/evidence"
	"github.com/bdobrica/ThinkPixelAG/internal/policy"
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
	"github.com/jackc/pgx/v5/pgxpool"
)

type identity struct {
	Tenant, ForeignTenant, Principal, ForeignPrincipal, Admin, Agent, Version, Gateway string
	Issuer                                                                             string
	Audience                                                                           string
	JWKS                                                                               any
	Gateways                                                                           []string
}

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: fixture prepare|seed|serve|token|health DIRECTORY")
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "prepare":
		err = prepare(os.Args[2])
	case "seed":
		err = seed(os.Args[2])
	case "serve":
		err = serve(os.Args[2])
	case "health":
		err = health(os.Args[2])
	case "token":
		err = refreshCallerToken(os.Args[2], os.Stdout)
	default:
		err = errors.New("unknown mode")
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "fixture operation failed:", err)
		os.Exit(1)
	}
}
func newID() string {
	id, err := domain.NewID()
	if err != nil {
		panic("ID generation failed")
	}
	return id.String()
}
func write(dir, name string, b []byte) error { return os.WriteFile(filepath.Join(dir, name), b, 0600) }
func load(dir string) (identity, error) {
	var i identity
	b, e := os.ReadFile(filepath.Join(dir, "identity.json"))
	if e == nil {
		e = json.Unmarshal(b, &i)
	}
	return i, e
}
func prepare(dir string) error {
	count, err := gatewayCount(os.Getenv("OPS_GATEWAY_COUNT"))
	if err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(dir, "identity.json")); err == nil {
		return errors.New("fixture already exists; reuse it instead of replacing keys")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	issuer := os.Getenv("OPS_ISSUER")
	if issuer == "" {
		return errors.New("OPS_ISSUER is required")
	}
	i := identity{Tenant: newID(), ForeignTenant: newID(), Principal: newID(), ForeignPrincipal: newID(), Admin: newID(), Agent: newID(), Version: newID(), Gateway: newID(), Issuer: issuer}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return err
	}
	i.JWKS = map[string]any{"keys": []any{map[string]any{"kty": "RSA", "kid": "ops010", "alg": "RS256", "use": "sig", "n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()), "e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes())}}}
	i.Audience = os.Getenv("OPS_AUDIENCE")
	if i.Audience == "" {
		i.Audience = "ops010"
	}
	if err = writeTokens(dir, i, key, 23*time.Hour); err != nil {
		return err
	}
	if os.Getenv("OPS_RETAIN_SIGNING_KEY") == "1" {
		der, e := x509.MarshalPKCS8PrivateKey(key)
		if e != nil {
			return e
		}
		if e = write(dir, "issuer.key", pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})); e != nil {
			return e
		}
	}
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "OPS010 isolated test CA"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(14 * 24 * time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature}
	caDER, err := x509.CreateCertificate(rand.Reader, ca, ca, &caKey.PublicKey, caKey)
	if err != nil {
		return err
	}
	if err = write(dir, "ca.crt", pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})); err != nil {
		return err
	}
	issue := func(name, uri string, hosts []string, serial int64) error {
		k, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if e != nil {
			return e
		}
		c := &x509.Certificate{SerialNumber: big.NewInt(serial), Subject: pkix.Name{CommonName: name}, NotBefore: ca.NotBefore, NotAfter: ca.NotAfter, KeyUsage: x509.KeyUsageDigitalSignature, DNSNames: hosts, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
		if uri != "" {
			u, e := url.Parse(uri)
			if e != nil {
				return e
			}
			c.URIs = []*url.URL{u}
			c.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}
		}
		b, e := x509.CreateCertificate(rand.Reader, c, ca, &k.PublicKey, caKey)
		if e != nil {
			return e
		}
		if e = write(dir, name+".crt", pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: b})); e != nil {
			return e
		}
		der, e := x509.MarshalPKCS8PrivateKey(k)
		if e != nil {
			return e
		}
		return write(dir, name+".key", pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
	}
	u, e := url.Parse(issuer)
	if e != nil {
		return e
	}
	if e = issue("server", "", []string{u.Hostname(), "ops-api", "ops-api.thinkpixelag-ops.svc", "localhost"}, 2); e != nil {
		return e
	}
	bindings := []any{}
	for n := 0; n < count; n++ {
		id := newID()
		if n == 0 {
			id = i.Gateway
		}
		i.Gateways = append(i.Gateways, id)
		uri := "spiffe://ops010/gateway/" + strconv.Itoa(n)
		if e = issue("gateway-"+strconv.Itoa(n), uri, nil, int64(n+3)); e != nil {
			return e
		}
		bindings = append(bindings, map[string]any{"uri_san": uri, "principal_id": id, "tenant_id": i.Tenant, "roles": []string{"trusted-gateway"}})
	}
	b, _ := json.Marshal(map[string]any{"contract_version": "thinkpixelag.workload-identity/v1", "bindings": bindings})
	if e = write(dir, "bindings.json", b); e != nil {
		return e
	}
	b, _ = json.Marshal(i)
	if e = write(dir, "identity.json", b); e != nil {
		return e
	}
	fmt.Printf("Prepared isolated identities, TLS certificates, and %d gateway bindings; key material retained locally\n", count)
	return nil
}

func gatewayCount(value string) (int, error) {
	if value == "" {
		return 5000, nil
	}
	n, err := strconv.Atoi(value)
	if err != nil || n < 1 || n > 5000 {
		return 0, errors.New("OPS_GATEWAY_COUNT must be between 1 and 5000")
	}
	return n, nil
}

func writeTokens(dir string, i identity, key *rsa.PrivateKey, lifetime time.Duration) error {
	for name, claims := range map[string]map[string]any{"caller.token": {"sub": i.Principal, "tenant_id": i.Tenant, "roles": []string{"invoker"}}, "foreign.token": {"sub": i.ForeignPrincipal, "tenant_id": i.ForeignTenant, "roles": []string{"invoker"}}, "admin.token": {"sub": i.Admin, "tenant_id": i.Tenant, "roles": []string{"revoker"}}} {
		claims["iss"] = i.Issuer
		claims["aud"] = i.Audience
		claims["iat"] = time.Now().Unix()
		claims["exp"] = time.Now().Add(lifetime).Unix()
		head, _ := json.Marshal(map[string]string{"alg": "RS256", "kid": "ops010"})
		body, _ := json.Marshal(claims)
		msg := base64.RawURLEncoding.EncodeToString(head) + "." + base64.RawURLEncoding.EncodeToString(body)
		hash := sha256.Sum256([]byte(msg))
		sig, e := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, hash[:])
		if e != nil {
			return e
		}
		if e = write(dir, name, []byte(msg+"."+base64.RawURLEncoding.EncodeToString(sig))); e != nil {
			return e
		}
	}
	return nil
}

// Token issuance is an operator-only CLI operation, never an HTTP endpoint.
// Persisting the sample issuer key is opt-in and confined to the private volume.
func refreshCallerToken(dir string, out io.Writer) error {
	i, err := load(dir)
	if err != nil {
		return errors.New("load sample identity")
	}
	data, err := os.ReadFile(filepath.Join(dir, "issuer.key"))
	if err != nil {
		return errors.New("sample issuer key unavailable; token refresh requires opt-in key retention at setup")
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return errors.New("invalid sample issuer key")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return errors.New("invalid sample issuer key")
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return errors.New("invalid sample issuer key type")
	}
	if i.Audience == "" {
		i.Audience = "ops010"
	}
	if err := writeTokens(dir, i, key, 15*time.Minute); err != nil {
		return err
	}
	token, err := os.ReadFile(filepath.Join(dir, "caller.token"))
	if err != nil {
		return errors.New("read sample caller token")
	}
	_, err = fmt.Fprintln(out, string(token))
	return err
}

type signatureVerifier struct{ key ed25519.PublicKey }

func (v signatureVerifier) Verify(_ context.Context, s ports.Signature, d ports.SigningDigest) error {
	if s.KeyID != "ops010-software-test-key" || s.KeyVersion != "1" || s.Algorithm != ports.SignatureEd25519 || !ed25519.Verify(v.key, d.Value, s.Value) {
		return errors.New("invalid fixture policy signature")
	}
	return nil
}

type bundleValidator struct{ path string }

func (v bundleValidator) Validate(ctx context.Context, b []byte) error {
	p, err := os.ReadFile(v.path)
	if err != nil || string(p) != string(b) {
		return errors.New("policy bytes changed")
	}
	cmd := exec.CommandContext(ctx, "opa", "check", "--strict", v.path)
	if err = cmd.Run(); err != nil {
		return errors.New("OPA compile validation failed")
	}
	return nil
}
func seed(dir string) error {
	i, err := load(dir)
	if err != nil {
		return err
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, os.Getenv("OPS_DATABASE_URL"))
	if err != nil {
		return errors.New("database configuration failed")
	}
	defer pool.Close()
	now := time.Now().UTC()
	tenants := []string{i.Tenant, i.ForeignTenant}
	for _, tenant := range tenants {
		if _, err = pool.Exec(ctx, `INSERT INTO tenants(id,slug,display_name,created_at,updated_at) VALUES($1,$2,$2,$3,$3) ON CONFLICT(id) DO NOTHING`, tenant, "ops010-"+tenant, now); err != nil {
			return err
		}
	}
	ids := []string{i.Principal, i.ForeignPrincipal, i.Admin}
	ids = append(ids, i.Gateways...)
	for _, id := range ids {
		tenant := i.Tenant
		kind := "WORKLOAD"
		if id == i.ForeignPrincipal {
			tenant = i.ForeignTenant
		}
		if id == i.Principal || id == i.ForeignPrincipal || id == i.Admin {
			kind = "HUMAN"
		}
		if _, err = pool.Exec(ctx, `INSERT INTO principals(id,tenant_id,external_issuer,external_subject,principal_type,created_at) VALUES($1,$2,$3,$6,$4,$5) ON CONFLICT(id) DO NOTHING`, id, tenant, i.Issuer, kind, now, id); err != nil {
			return err
		}
	}
	if _, err = pool.Exec(ctx, `INSERT INTO agents(id,tenant_id,name,owner_principal_id,sponsor_principal_id,risk_class,created_at,updated_at) VALUES($1,$2,'ops010-agent',$3,$4,'MEDIUM',$5,$5) ON CONFLICT(id) DO NOTHING`, i.Agent, i.Tenant, i.Principal, i.Admin, now); err != nil {
		return err
	}
	repos, err := postgres.NewRepositories(pool)
	if err != nil {
		return err
	}
	tenant, _ := domain.ParseID(i.Tenant)
	repo, err := repos.ForTenant(tenant)
	if err != nil {
		return err
	}
	manifest, err := domain.NewAgentManifest("registry.example/ops010@sha256:"+strings.Repeat("a", 64), nil, nil, nil, nil, domain.AgentLimits{})
	if err != nil {
		return err
	}
	digest, _ := manifest.ContentDigest()
	id, _ := domain.ParseID(i.Version)
	agent, _ := domain.ParseID(i.Agent)
	principal, _ := domain.ParseID(i.Principal)
	var exists bool
	if err = pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agent_versions WHERE id=$1)`, i.Version).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		if err = repo.RegisterAgentVersion(ctx, domain.AgentVersion{ID: id, TenantID: tenant, AgentID: agent, ContentDigest: digest, ImageDigest: "sha256:" + strings.Repeat("a", 64), Manifest: manifest, CreatedBy: principal, CreatedAt: now}, nil); err != nil {
			return err
		}
		if _, err = pool.Exec(ctx, `INSERT INTO agent_version_approvals(id,tenant_id,agent_id,agent_version_id,decision,actor_principal_id,policy_decision_id,reason_code,created_at) VALUES($1,$2,$3,$4,'APPROVED',$5,$6,'registry.version.approved',$7)`, newID(), i.Tenant, i.Agent, i.Version, i.Admin, newID(), now); err != nil {
			return err
		}
	}
	for _, name := range []string{"llm_tokens", "tool_calls", "active_children", "total_children", "delegation_depth"} {
		class, unit, aggregation := "CONSUMABLE", name, "SUM"
		if strings.Contains(name, "children") || name == "delegation_depth" {
			class, unit, aggregation = "STRUCTURAL", "children", "MAX"
		}
		if _, err = pool.Exec(ctx, `INSERT INTO resource_dimensions(id,tenant_id,name,class,unit,scale,minimum_value,maximum_value,aggregation,created_at) VALUES($1,$2,$3,$4,$5,0,0,1000000000,$6,$7) ON CONFLICT DO NOTHING`, newID(), i.Tenant, name, class, unit, aggregation, now); err != nil {
			return err
		}
	}
	body, err := os.ReadFile("policies/authorization.rego")
	if err != nil {
		return err
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	tx, err := postgres.NewTransactor(pool)
	if err != nil {
		return err
	}
	fresh, _ := policy.NewFreshness(time.Minute, time.Now)
	store, err := postgres.NewPolicyStore(pool, tx, fresh)
	if err != nil {
		return err
	}
	for _, tid := range tenants {
		var revision int64
		if err = pool.QueryRow(ctx, `SELECT COALESCE(max(artifact_revision),0)+1 FROM policy_bundles WHERE tenant_id=$1`, tid).Scan(&revision); err != nil {
			return err
		}
		sign, digest, err := artifact.SigningDigest(artifact.PolicyBundle, policy.ContractVersion, uint64(revision), body)
		if err != nil {
			return err
		}
		t, _ := domain.ParseID(tid)
		actor := principal
		if tid == i.ForeignTenant {
			actor, _ = domain.ParseID(i.ForeignPrincipal)
		}
		bundleID, _ := domain.NewID()
		bundle := postgres.PolicyBundle{ID: bundleID, TenantID: t, CreatedBy: actor, Channel: "stable", Digest: digest, ContractVersion: policy.ContractVersion, ArtifactRevision: uint64(revision), Bundle: body, Signature: ed25519.Sign(priv, sign.Value), SignerKeyID: "ops010-software-test-key", SignerKeyVersion: "1", SignatureAlgorithm: ports.SignatureEd25519, CreatedAt: now}
		if err = store.VerifyAndPersist(ctx, bundle, signatureVerifier{pub}, bundleValidator{"policies/authorization.rego"}); err != nil {
			return err
		}
		if _, err = store.Activate(ctx, t, bundleID, actor, "stable", "ops010.qualification", now); err != nil {
			return err
		}
	}
	fmt.Println("Seeded isolated synthetic data and signature/compile-verified test policy; production KMS is not exercised")
	return nil
}

func serve(dir string) error {
	i, err := load(dir)
	if err != nil {
		return err
	}
	token := os.Getenv("OPS_SINK_TOKEN")
	if token == "" {
		return errors.New("sink token required")
	}
	path := os.Getenv("OPS_RECEIPTS_FILE")
	if path == "" {
		return errors.New("durable receipt path required")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	type stored struct {
		Delivery evidence.Delivery `json:"delivery"`
		Receipt  evidence.Receipt  `json:"receipt"`
	}
	receipts := map[string]evidence.Receipt{}
	heads := map[string]evidence.Receipt{}
	dec := json.NewDecoder(f)
	for {
		var record stored
		if e := dec.Decode(&record); e == io.EOF {
			break
		} else if e != nil {
			return e
		}
		if record.Receipt.ValidateFor(record.Delivery) != nil {
			return errors.New("persisted evidence integrity failed")
		}
		receipts[record.Receipt.EventID] = record.Receipt
		heads[record.Receipt.SinkID] = record.Receipt
	}
	var mu sync.Mutex
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"issuer": i.Issuer, "jwks_uri": i.Issuer + "/jwks"})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "max-age=3600")
		_ = json.NewEncoder(w).Encode(i.JWKS)
	})
	mux.HandleFunc("/evidence", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.Header.Get("Authorization") != "Bearer "+token {
			w.WriteHeader(401)
			return
		}
		var d evidence.Delivery
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&d) != nil || d.Validate() != nil {
			w.WriteHeader(400)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		receipt, exists := receipts[d.EventID]
		if exists {
			if receipt.ValidateFor(d) != nil {
				w.WriteHeader(409)
				return
			}
		} else {
			head := heads[d.SinkID]
			if d.Sequence != head.Sequence+1 || d.PreviousHash != head.EventHash {
				w.WriteHeader(409)
				return
			}
			receipt = evidence.Receipt{Version: evidence.DeliveryVersion, SinkID: d.SinkID, Sequence: d.Sequence, EventID: d.EventID, EventHash: d.EventHash, ReceiptID: newID(), Checkpoint: d.EventHash, AcceptedAt: time.Now().UTC()}
			if json.NewEncoder(f).Encode(stored{d, receipt}) != nil || f.Sync() != nil {
				w.WriteHeader(503)
				return
			}
			receipts[d.EventID] = receipt
			heads[d.SinkID] = receipt
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(receipt)
	})
	server := &http.Server{Addr: ":8443", Handler: mux, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: time.Minute, MaxHeaderBytes: 1 << 20}
	return server.ListenAndServeTLS(filepath.Join(dir, "server.crt"), filepath.Join(dir, "server.key"))
}

func health(dir string) error {
	i, err := load(dir)
	if err != nil {
		return errors.New("load sample issuer")
	}
	ca, err := os.ReadFile(filepath.Join(dir, "ca.crt"))
	if err != nil {
		return errors.New("load sample CA")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(ca) {
		return errors.New("invalid sample CA")
	}
	client := &http.Client{Timeout: 2 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}}}
	resp, err := client.Get(i.Issuer + "/.well-known/openid-configuration")
	if err != nil {
		return errors.New("sample issuer unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return errors.New("sample issuer not ready")
	}
	return nil
}
