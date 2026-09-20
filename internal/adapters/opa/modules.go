package opa

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/bdobrica/ThinkPixelAG/internal/artifact"
	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/policy"
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// Modules keeps multiple immutable artifact namespaces side by side. Loading a
// module is compilation, not activation: PostgreSQL chooses the evaluated one.
type Modules struct {
	Base, Token string
	Client      *http.Client
	Timeout     time.Duration
}

var packageLine = regexp.MustCompile(`(?m)^package thinkpixelag\.authorization[ \t]*\r?$`)
var forbiddenSource = regexp.MustCompile(`\b(data|http|net|opa)\b|__ag_envelope`)

func moduleSource(digest string, source []byte) ([]byte, error) {
	if !domain.ValidDigest(digest) || len(source) == 0 || len(source) > 1<<20 || !utf8.Valid(source) || len(packageLine.FindAllIndex(source, -1)) != 1 || forbiddenSource.Match(source) {
		return nil, errors.New("policy must be a bounded self-contained authorization Rego module")
	}
	got, err := policy.Digest(source)
	if err != nil || got != digest {
		return nil, errors.New("policy digest mismatch")
	}
	compiled := packageLine.ReplaceAll(source, []byte("package thinkpixelag.artifacts.d"+digest[7:]))
	return append(compiled, []byte("\n__ag_envelope := {\"decision\": decision, \"artifact_digest\": \""+digest+"\"}\n")...), nil
}
func (m *Modules) request(ctx context.Context, method, path, contentType string, body []byte) (int, []byte, error) {
	if m.Client == nil || m.Timeout <= 0 {
		return 0, nil, ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, m.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(m.Base, "/")+path, bytes.NewReader(body))
	if err != nil {
		return 0, nil, ErrUnavailable
	}
	req.Header.Set("Content-Type", contentType)
	if m.Token != "" {
		req.Header.Set("Authorization", "Bearer "+m.Token)
	}
	response, err := m.Client.Do(req)
	if err != nil {
		return 0, nil, ErrUnavailable
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 8<<20))
	if err != nil || len(raw) >= 8<<20 {
		return 0, nil, ErrUnavailable
	}
	return response.StatusCode, raw, nil
}
func (m *Modules) Validate(ctx context.Context, source []byte) error {
	digest, err := policy.Digest(source)
	if err != nil {
		return err
	}
	return m.Ensure(ctx, digest, source)
}
func (m *Modules) Ensure(ctx context.Context, digest string, source []byte) error {
	compiled, err := moduleSource(digest, source)
	if err != nil {
		return err
	}
	path := "/v1/policies/thinkpixelag-" + digest[7:]
	status, raw, err := m.request(ctx, http.MethodGet, path, "application/json", nil)
	if err != nil {
		return err
	}
	if status == http.StatusOK {
		var result struct {
			Result struct {
				Raw string `json:"raw"`
			} `json:"result"`
		}
		if json.Unmarshal(raw, &result) != nil || result.Result.Raw != string(compiled) {
			return errors.New("loaded policy module does not match artifact")
		}
		return nil
	}
	if status != http.StatusNotFound {
		return ErrUnavailable
	}
	status, _, err = m.request(ctx, http.MethodPut, path, "text/plain", compiled)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return errors.New("OPA rejected policy compilation")
	}
	return nil
}

type ArtifactEvaluator struct {
	Store    ports.PolicyAdministrationStore
	Modules  *Modules
	Verifier ports.Verifier
	Channel  string
	MaxTTL   time.Duration
}

func (e *ArtifactEvaluator) Decide(ctx context.Context, in policy.Input) (policy.Result, error) {
	if err := in.Validate(); err != nil {
		return policy.Result{}, err
	}
	b, active, err := e.Store.CurrentPolicy(ctx, e.Channel)
	if err != nil {
		return policy.Result{}, err
	}
	if e.Verifier == nil {
		return policy.Result{}, ErrUnavailable
	}
	err = artifact.Verify(ctx, artifact.Envelope{Kind: artifact.PolicyBundle, FormatVersion: b.ContractVersion, Revision: b.Revision, Digest: b.Digest, Payload: b.Source, Signature: b.Signature}, e.Verifier, artifact.Versions{artifact.PolicyBundle: {policy.ContractVersion: {}}})
	if err != nil {
		return policy.Result{}, ErrUnavailable
	}
	if err = e.Modules.Ensure(ctx, b.Digest, b.Source); err != nil {
		return policy.Result{}, err
	}
	body, _ := json.Marshal(map[string]any{"input": in})
	started := time.Now()
	status, raw, err := e.Modules.request(ctx, http.MethodPost, "/v1/data/thinkpixelag/artifacts/d"+b.Digest[7:]+"/__ag_envelope", "application/json", body)
	if err != nil || status != 200 || len(raw) > 64<<10 {
		return policy.Result{}, ErrUnavailable
	}
	var response struct {
		Result struct {
			Decision policy.Decision `json:"decision"`
			Digest   string          `json:"artifact_digest"`
		} `json:"result"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	decoder.DisallowUnknownFields()
	if decoder.Decode(&response) != nil || decoder.Decode(new(any)) != io.EOF || response.Result.Digest != b.Digest {
		return policy.Result{}, ErrUnavailable
	}
	if err = policy.ValidateDecision(response.Result.Decision, in, e.MaxTTL); err != nil {
		return policy.Result{}, ErrUnavailable
	}
	if response.Result.Decision.Allow && in.Action == "runs.create" {
		response.Result.Decision.ResolvedConstraints, err = policy.ResolveConstraints(in.AuthorityConstraints, in.RequestedConstraints, response.Result.Decision.ResolvedConstraints)
		if err != nil {
			return policy.Result{}, ErrUnavailable
		}
	}
	_, after, err := e.Store.CurrentPolicy(ctx, e.Channel)
	if err != nil || after.Version != active.Version || after.Digest != active.Digest {
		return policy.Result{}, ErrUnavailable
	}
	inputDigest, _ := policy.AuthorizationDigest(in)
	return policy.Result{Decision: response.Result.Decision, Metadata: policy.Metadata{PolicyDigest: b.Digest, PolicyVersion: active.Version, InputDigest: inputDigest, Duration: time.Since(started), CacheStatus: "bypass"}}, nil
}
