package fetch

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	oxBrowserTimeout  = 30 * time.Second
	maxResponseBytes  = 2 << 20 // 2 MB
	errorPreviewBytes = 200
)

// OxBrowserClient calls ox-browser's /read endpoint for content extraction.
type OxBrowserClient struct {
	baseURL string
	client  *http.Client
}

// NewOxBrowserClient creates a client for the ox-browser read API.
func NewOxBrowserClient(baseURL string) *OxBrowserClient {
	return &OxBrowserClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  &http.Client{Timeout: oxBrowserTimeout},
	}
}

// ReadabilityResult is the response from ox-browser /read.
type ReadabilityResult struct {
	Title     string            `json:"title"`
	Content   string            `json:"content"`
	Author    string            `json:"author"`
	Excerpt   string            `json:"excerpt"`
	OGImage   string            `json:"og_image,omitempty"`
	Length    int               `json:"length"`
	ElapsedMs int               `json:"elapsed_ms"`
	JsonLD    []json.RawMessage `json:"json_ld,omitempty"`
	Error     string            `json:"error,omitempty"`
}

// Extract calls ox-browser /read and returns extracted content.
func (c *OxBrowserClient) Extract(ctx context.Context, pageURL string) (*ReadabilityResult, error) {
	body := fmt.Sprintf(`{"url":%q}`, pageURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/read", strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req) //nolint:gosec // URL is caller-provided internal service
	if err != nil {
		return nil, fmt.Errorf("ox-browser readability: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("ox-browser read body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ox-browser HTTP %d: %s", resp.StatusCode, string(data[:min(len(data), errorPreviewBytes)]))
	}

	var result ReadabilityResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("ox-browser unmarshal: %w", err)
	}
	if result.Error != "" {
		return nil, fmt.Errorf("ox-browser: %s", result.Error)
	}
	return &result, nil
}
