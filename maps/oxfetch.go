package maps

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// maxResponseBytes is the size limit for HTTP response bodies (512 KB).
const maxResponseBytes = 512 * 1024

// oxFetchResponse is the JSON response from ox-browser /fetch endpoint.
type oxFetchResponse struct {
	Body   string `json:"body"`
	Status int    `json:"status"`
	Error  string `json:"error,omitempty"`
}

// oxFetch sends a URL to ox-browser /fetch and returns the response body.
func oxFetch(ctx context.Context, client *http.Client, baseURL, targetURL string) (string, error) {
	body, err := json.Marshal(map[string]string{"url": targetURL})
	if err != nil {
		return "", fmt.Errorf("ox fetch: marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		baseURL+"/fetch", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("ox fetch: request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req) //nolint:gosec
	if err != nil {
		return "", fmt.Errorf("ox fetch: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ox fetch: HTTP %d", resp.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return "", fmt.Errorf("ox fetch: read: %w", err)
	}

	var fr oxFetchResponse
	if err := json.Unmarshal(data, &fr); err != nil {
		return "", fmt.Errorf("ox fetch: parse: %w", err)
	}
	if fr.Error != "" {
		return "", fmt.Errorf("ox fetch: %s", fr.Error)
	}
	if fr.Status != http.StatusOK {
		return "", fmt.Errorf("ox fetch: upstream HTTP %d", fr.Status)
	}
	return fr.Body, nil
}
