// Package evidencehttp exports minimized evidence to an independently
// configured HTTP sink. Authentication material is never placed in the body.
package evidencehttp

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

	"github.com/bdobrica/ThinkPixelAG/internal/evidence"
)

type Config struct {
	Endpoint, BearerToken string
	Timeout               time.Duration
	MaxResponseBytes      int64
}
type Sink struct {
	endpoint         string
	token            string
	client           *http.Client
	maxResponseBytes int64
}

func New(config Config, client *http.Client) (*Sink, error) {
	u, err := url.Parse(config.Endpoint)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || config.BearerToken == "" || strings.ContainsAny(config.BearerToken, "\r\n") || config.Timeout <= 0 || config.Timeout > time.Minute || config.MaxResponseBytes < 1 || config.MaxResponseBytes > 1<<20 {
		return nil, errors.New("invalid authenticated evidence sink configuration")
	}
	if client == nil {
		client = &http.Client{}
	}
	clone := *client
	clone.Timeout = config.Timeout
	clone.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Sink{config.Endpoint, config.BearerToken, &clone, config.MaxResponseBytes}, nil
}

func (s *Sink) Export(ctx context.Context, delivery evidence.Delivery) (evidence.Receipt, error) {
	if err := delivery.Validate(); err != nil {
		return evidence.Receipt{}, err
	}
	body, err := json.Marshal(delivery)
	if err != nil {
		return evidence.Receipt{}, fmt.Errorf("encode evidence delivery: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint, bytes.NewReader(body))
	if err != nil {
		return evidence.Receipt{}, fmt.Errorf("create evidence request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+s.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", delivery.EventID)
	response, err := s.client.Do(req)
	if err != nil {
		return evidence.Receipt{}, fmt.Errorf("export evidence: %w", err)
	}
	defer response.Body.Close()
	limited := io.LimitReader(response.Body, s.maxResponseBytes+1)
	responseBody, readErr := io.ReadAll(limited)
	if readErr != nil {
		return evidence.Receipt{}, fmt.Errorf("read evidence receipt: %w", readErr)
	}
	if int64(len(responseBody)) > s.maxResponseBytes {
		return evidence.Receipt{}, errors.New("evidence receipt exceeds configured bound")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return evidence.Receipt{}, fmt.Errorf("evidence sink returned status %d", response.StatusCode)
	}
	var receipt evidence.Receipt
	decoder := json.NewDecoder(bytes.NewReader(responseBody))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&receipt); err != nil {
		return evidence.Receipt{}, errors.New("evidence sink returned an invalid receipt")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return evidence.Receipt{}, errors.New("evidence sink returned trailing receipt data")
	}
	if err := receipt.ValidateFor(delivery); err != nil {
		return evidence.Receipt{}, err
	}
	return receipt, nil
}
