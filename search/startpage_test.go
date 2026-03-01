package search

import (
	"context"
	"fmt"
	"io"
	"testing"
)

// newTestStartpage creates a Startpage provider with a mock BrowserDoer for testing.
func newTestStartpage(mock BrowserDoer, opts ...StartpageOption) *Startpage {
	allOpts := append([]StartpageOption{WithStartpageDoer(mock)}, opts...)
	// proxyURL is unused because WithStartpageDoer overrides the stealth client.
	sp, _ := NewStartpage("socks5://test:1080", allOpts...) //nolint:errcheck // test helper
	return sp
}

func TestStartpage_Search(t *testing.T) {
	t.Parallel()

	html := `<html><body>
		<div class="w-gl__result">
			<a class="w-gl__result-title" href="https://example.com/1">Result One</a>
			<p class="w-gl__description">First description</p>
		</div>
		<div class="w-gl__result">
			<a class="w-gl__result-title" href="https://example.com/2">Result Two</a>
			<p class="w-gl__description">Second description</p>
		</div>
	</body></html>`

	mock := &mockBrowser{
		handler: func(method, url string, headers map[string]string, body io.Reader) ([]byte, map[string]string, int, error) {
			if method != "POST" {
				t.Errorf("expected POST method, got %s", method)
			}
			return []byte(html), nil, 200, nil
		},
	}

	sp := newTestStartpage(mock)
	result, err := sp.Search(context.Background(), "test query", "")
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}
	if len(result.Sources) != 2 {
		t.Errorf("expected 2 sources, got %d", len(result.Sources))
	}
	if result.Context == "" {
		t.Error("expected non-empty context")
	}
}

func TestStartpage_ErrorStatus(t *testing.T) {
	t.Parallel()

	mock := &mockBrowser{
		handler: func(method, url string, headers map[string]string, body io.Reader) ([]byte, map[string]string, int, error) {
			return nil, nil, 403, nil
		},
	}

	sp := newTestStartpage(mock)
	_, err := sp.Search(context.Background(), "test query", "")
	if err == nil {
		t.Error("expected error on 403")
	}
}

func TestStartpage_MaxResults(t *testing.T) {
	t.Parallel()

	// Generate HTML with 10 results.
	var htmlParts []byte
	htmlParts = append(htmlParts, []byte(`<html><body>`)...)
	for i := range 10 {
		htmlParts = append(htmlParts, []byte(fmt.Sprintf(
			`<div class="w-gl__result">
				<a class="w-gl__result-title" href="https://example.com/%d">Result %d</a>
				<p class="w-gl__description">Description %d</p>
			</div>`, i, i, i))...)
	}
	htmlParts = append(htmlParts, []byte(`</body></html>`)...)

	mock := &mockBrowser{
		handler: func(method, url string, headers map[string]string, body io.Reader) ([]byte, map[string]string, int, error) {
			return htmlParts, nil, 200, nil
		},
	}

	sp := newTestStartpage(mock, WithStartpageMaxResults(2))
	result, err := sp.Search(context.Background(), "test query", "")
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}
	if len(result.Sources) > 2 {
		t.Errorf("expected at most 2 sources, got %d", len(result.Sources))
	}
}

func TestStartpage_RequiresProxy(t *testing.T) {
	t.Parallel()

	// Empty proxy URL should fail at stealth client creation.
	_, err := NewStartpage("")
	if err == nil {
		t.Error("expected error with empty proxy URL")
	}
}

func TestStartpage_WithProxyPool(t *testing.T) {
	t.Parallel()

	html := `<html><body>
		<div class="w-gl__result">
			<a class="w-gl__result-title" href="https://example.com/pool">Pool Result</a>
			<p class="w-gl__description">Pool description</p>
		</div>
	</body></html>`

	mock := &mockBrowser{
		handler: func(_, _ string, _ map[string]string, _ io.Reader) ([]byte, map[string]string, int, error) {
			return []byte(html), nil, 200, nil
		},
	}

	sp, err := NewStartpage("", WithStartpageDoer(mock), WithStartpageProxyPool(&staticPool{url: "socks5://tor:9050"}))
	if err != nil {
		t.Fatalf("NewStartpage with pool should succeed: %v", err)
	}

	result, err := sp.Search(context.Background(), "test", "")
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}
	if len(result.Sources) != 1 {
		t.Errorf("expected 1 source, got %d", len(result.Sources))
	}
}
