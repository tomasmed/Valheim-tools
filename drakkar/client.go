package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"strings"
	"time"
)

// HTTPClient manages authenticated communication with the Mead Hall Dashboard
type HTTPClient struct {
	endpoint string
	secret   string
	client   *http.Client
}

// NewHTTPClient creates a new HTTPClient instance
func NewHTTPClient(dashboardURL, secret string) *HTTPClient {
	endpoint := strings.TrimRight(dashboardURL, "/") + "/api/telemetry"
	return &HTTPClient{
		endpoint: endpoint,
		secret:   secret,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// SendTelemetry serializes and posts telemetry with exponential backoff
func (c *HTTPClient) SendTelemetry(ctx context.Context, payload TelemetryPayload) error {
	payload.Source = "drakkar"
	if payload.Timestamp == "" {
		payload.Timestamp = time.Now().UTC().Format(time.RFC3339)
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal telemetry payload: %w", err)
	}

	maxRetries := 5
	backoff := 500 * time.Millisecond

	for attempt := 1; attempt <= maxRetries; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(bodyBytes))
		if err != nil {
			return fmt.Errorf("failed to create request: %w", err)
		}

		req.Header.Set("Content-Type", "application/json")
		if c.secret != "" {
			req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.secret))
		}

		resp, err := c.client.Do(req)
		if err == nil {
			defer resp.Body.Close()
			respBody, _ := io.ReadAll(resp.Body)

			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				// Success
				return nil
			}

			// Do not retry permanent client auth/validation errors
			if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusBadRequest {
				return fmt.Errorf("telemetry rejected with HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
			}

			log.Printf("[Drakkar] Mead Hall returned HTTP %d: %s (attempt %d/%d)", resp.StatusCode, strings.TrimSpace(string(respBody)), attempt, maxRetries)
		} else {
			log.Printf("[Drakkar] Network error reaching Mead Hall: %v (attempt %d/%d)", err, attempt, maxRetries)
		}

		if attempt == maxRetries {
			break
		}

		// Calculate exponential backoff with jitter
		jitter := time.Duration(rand.Int63n(int64(backoff / 2)))
		sleepDuration := backoff + jitter

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(sleepDuration):
		}

		backoff *= 2
	}

	return fmt.Errorf("failed to deliver telemetry after %d attempts", maxRetries)
}
