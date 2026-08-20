package opa

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/policy"
)

var ErrUnavailable = errors.New("policy unavailable")

type Client struct {
	endpoint        string
	token           string
	timeout, maxTTL time.Duration
	http            *http.Client
	metadata        func() (string, int64, bool)
}

func New(base, path string, timeout, maxTTL time.Duration, token string, h *http.Client, metadata func() (string, int64, bool)) (*Client, error) {
	u, e := url.Parse(base)
	if e != nil || u.Host == "" {
		return nil, errors.New("OPA base URL is invalid")
	}
	p, e := url.Parse(path)
	if e != nil || !strings.HasPrefix(path, "/") || p.RawQuery != "" || p.Fragment != "" {
		return nil, errors.New("OPA decision path is invalid")
	}
	u.Path = strings.TrimRight(u.Path, "/") + p.Path
	if timeout <= 0 || maxTTL < 0 || metadata == nil {
		return nil, errors.New("OPA client bounds and metadata source are required")
	}
	if h == nil {
		h = &http.Client{}
	}
	return &Client{u.String(), token, timeout, maxTTL, h, metadata}, nil
}
func (c *Client) Decide(ctx context.Context, in policy.Input) (policy.Result, error) {
	if err := in.Validate(); err != nil {
		return policy.Result{}, fmt.Errorf("%w: input: %v", ErrUnavailable, err)
	}
	digest, version, fresh := c.metadata()
	if !fresh || digest == "" || version < 1 {
		return policy.Result{}, fmt.Errorf("%w: no fresh active policy", ErrUnavailable)
	}
	body, _ := json.Marshal(struct {
		Input policy.Input `json:"input"`
	}{in})
	inputDigest, _ := policy.AuthorizationDigest(in)
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(callCtx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return policy.Result{}, ErrUnavailable
	}
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	start := time.Now()
	resp, err := c.http.Do(req)
	duration := time.Since(start)
	if err != nil {
		return policy.Result{}, fmt.Errorf("%w: evaluation failed", ErrUnavailable)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return policy.Result{}, fmt.Errorf("%w: OPA status %d", ErrUnavailable, resp.StatusCode)
	}
	limited := io.LimitReader(resp.Body, (64<<10)+1)
	dec := json.NewDecoder(limited)
	dec.DisallowUnknownFields()
	var envelope struct {
		Result policy.Decision `json:"result"`
	}
	if err := dec.Decode(&envelope); err != nil {
		return policy.Result{}, fmt.Errorf("%w: malformed response", ErrUnavailable)
	}
	var extra any
	if dec.Decode(&extra) != io.EOF {
		return policy.Result{}, fmt.Errorf("%w: trailing response", ErrUnavailable)
	}
	if err := policy.ValidateDecision(envelope.Result, in, c.maxTTL); err != nil {
		return policy.Result{}, fmt.Errorf("%w: invalid decision", ErrUnavailable)
	}
	return policy.Result{Decision: envelope.Result, Metadata: policy.Metadata{PolicyDigest: digest, PolicyVersion: version, InputDigest: inputDigest, Duration: duration, CacheStatus: "miss"}}, nil
}
