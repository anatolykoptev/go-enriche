package search

import (
	"context"
	"fmt"
	"io"
	"testing"
)

// mockStartpageBrowser implements BrowserDoer for testing.
// Named distinctly to avoid conflicts with other test files.
type mockStartpageBrowser struct {
	handler func(method, url string, headers map[string]string, body io.Reader) ([]byte, map[string]string, int, error)
}

func (m *mockStartpageBrowser) Do(method, url string, headers map[string]string, body io.Reader) ([]byte, map[string]string, int, error) {
	return m.handler(method, url, headers, body)
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

	mock := &mockStartpageBrowser{
		handler: func(method, url string, headers map[string]string, body io.Reader) ([]byte, map[string]string, int, error) {
			if method != "POST" {
				t.Errorf("expected POST method, got %s", method)
			}
			return []byte(html), nil, 200, nil
		},
	}

	sp := NewStartpage(mock)
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

	mock := &mockStartpageBrowser{
		handler: func(method, url string, headers map[string]string, body io.Reader) ([]byte, map[string]string, int, error) {
			return nil, nil, 403, nil
		},
	}

	sp := NewStartpage(mock)
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

	mock := &mockStartpageBrowser{
		handler: func(method, url string, headers map[string]string, body io.Reader) ([]byte, map[string]string, int, error) {
			return htmlParts, nil, 200, nil
		},
	}

	sp := NewStartpage(mock, WithStartpageMaxResults(2))
	result, err := sp.Search(context.Background(), "test query", "")
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}
	if len(result.Sources) > 2 {
		t.Errorf("expected at most 2 sources, got %d", len(result.Sources))
	}
}
