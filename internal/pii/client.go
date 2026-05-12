package pii

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Analyzer sends text to a PII detection backend and returns detected entities.
type Analyzer interface {
	Analyze(ctx context.Context, text, language string) ([]PresidioEntity, error)
}

// PresidioClient is an HTTP client for the Microsoft Presidio API.
type PresidioClient struct {
	BaseURL    string
	HTTPClient *http.Client
}

// NewPresidioClient creates a client with sensible defaults.
func NewPresidioClient(baseURL string) *PresidioClient {
	return &PresidioClient{
		BaseURL: baseURL,
		HTTPClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// Analyze sends a text sample to the Presidio /analyze endpoint.
func (c *PresidioClient) Analyze(ctx context.Context, text, language string) ([]PresidioEntity, error) {
	if language == "" {
		language = "en"
	}

	body, err := json.Marshal(PresidioRequest{Text: text, Language: language})
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := c.BaseURL + "/analyze"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http post: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("presidio returned %d: %s", resp.StatusCode, string(msg))
	}

	var entities []PresidioEntity
	if err := json.NewDecoder(resp.Body).Decode(&entities); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return entities, nil
}
