package search

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"
)

type mockBrowser struct {
	handler func(method, url string, headers map[string]string, body io.Reader) ([]byte, map[string]string, int, error)
}

func (m *mockBrowser) Do(method, url string, headers map[string]string, body io.Reader) ([]byte, map[string]string, int, error) {
	return m.handler(method, url, headers, body)
}

// newTestDDG creates a DDG provider with a mock BrowserDoer for testing.
func newTestDDG(mock BrowserDoer, opts ...DDGOption) *DDG {
	allOpts := append([]DDGOption{WithDDGDoer(mock)}, opts...)
	// proxyURL is unused because WithDDGDoer overrides the stealth client.
	d, _ := NewDDG("socks5://test:1080", allOpts...) //nolint:errcheck // test helper
	return d
}

func TestDDG_SearchHTML(t *testing.T) {
	t.Parallel()

	html := `<html><body>
		<div class="result">
			<a class="result__a" href="https://example.com/one">First Title</a>
			<span class="result__snippet">First snippet content</span>
		</div>
		<div class="result">
			<a class="result__a" href="https://example.com/two">Second Title</a>
			<span class="result__snippet">Second snippet content</span>
		</div>
	</body></html>`

	mock := &mockBrowser{
		handler: func(method, url string, headers map[string]string, body io.Reader) ([]byte, map[string]string, int, error) {
			if method != "POST" {
				t.Errorf("expected POST, got %s", method)
			}
			if url != "https://html.duckduckgo.com/html/" {
				t.Errorf("unexpected URL: %s", url)
			}
			return []byte(html), nil, 200, nil
		},
	}

	ddg := newTestDDG(mock, WithDDGMaxResults(5))
	result, err := ddg.Search(context.Background(), "test query", "")
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}
	if len(result.Sources) != 2 {
		t.Errorf("expected 2 sources, got %d", len(result.Sources))
	}
	if result.Context == "" {
		t.Error("expected non-empty context")
	}
	if !strings.Contains(result.Context, "First Title") {
		t.Error("context missing First Title")
	}
	if !strings.Contains(result.Context, "Second snippet content") {
		t.Error("context missing Second snippet content")
	}
}

func TestDDG_UnwrapURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		href string
		want string
	}{
		{
			name: "ddg redirect with uddg param",
			href: "//duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.com%2Fpage&rut=abc",
			want: "https://example.com/page",
		},
		{
			name: "direct http url",
			href: "https://example.com/direct",
			want: "https://example.com/direct",
		},
		{
			name: "relative path",
			href: "/relative/path",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ddgUnwrapURL(tt.href)
			if got != tt.want {
				t.Errorf("ddgUnwrapURL(%q) = %q, want %q", tt.href, got, tt.want)
			}
		})
	}
}

func TestDDG_ErrorStatus(t *testing.T) {
	t.Parallel()

	mock := &mockBrowser{
		handler: func(_, _ string, _ map[string]string, _ io.Reader) ([]byte, map[string]string, int, error) {
			return nil, nil, 403, nil
		},
	}

	ddg := newTestDDG(mock)
	_, err := ddg.Search(context.Background(), "test", "")
	if err == nil {
		t.Error("expected error on 403")
	}
	if !strings.Contains(err.Error(), "403") {
		t.Errorf("error should mention 403, got: %v", err)
	}
}

func TestDDG_MaxResults(t *testing.T) {
	t.Parallel()

	// Build HTML with 10 results.
	var htmlParts []string
	htmlParts = append(htmlParts, `<html><body>`)
	for i := range 10 {
		htmlParts = append(htmlParts, fmt.Sprintf(
			`<div class="result"><a class="result__a" href="https://example.com/%d">Title %d</a><span class="result__snippet">Content %d</span></div>`,
			i, i, i,
		))
	}
	htmlParts = append(htmlParts, `</body></html>`)
	html := strings.Join(htmlParts, "\n")

	mock := &mockBrowser{
		handler: func(_, _ string, _ map[string]string, _ io.Reader) ([]byte, map[string]string, int, error) {
			return []byte(html), nil, 200, nil
		},
	}

	ddg := newTestDDG(mock, WithDDGMaxResults(2))
	result, err := ddg.Search(context.Background(), "test", "")
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}
	if len(result.Sources) > 2 {
		t.Errorf("expected max 2 sources, got %d", len(result.Sources))
	}
}

func TestDDG_RequiresProxy(t *testing.T) {
	t.Parallel()

	// Empty proxy URL should fail at stealth client creation.
	_, err := NewDDG("")
	if err == nil {
		t.Error("expected error with empty proxy URL")
	}
}

// staticPool is a test mock implementing ProxyPoolProvider.
type staticPool struct{ url string }

func (p *staticPool) Next() string { return p.url }

func TestDDG_WithProxyPool(t *testing.T) {
	t.Parallel()

	html := `<html><body>
		<div class="result">
			<a class="result__a" href="https://example.com/pool">Pool Result</a>
			<span class="result__snippet">Pool snippet</span>
		</div>
	</body></html>`

	mock := &mockBrowser{
		handler: func(_, _ string, _ map[string]string, _ io.Reader) ([]byte, map[string]string, int, error) {
			return []byte(html), nil, 200, nil
		},
	}

	// ProxyPool set → proxyURL can be empty.
	ddg, err := NewDDG("", WithDDGDoer(mock), WithDDGProxyPool(&staticPool{url: "socks5://tor:9050"}))
	if err != nil {
		t.Fatalf("NewDDG with pool should succeed: %v", err)
	}

	result, err := ddg.Search(context.Background(), "test", "")
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}
	if len(result.Sources) != 1 {
		t.Errorf("expected 1 source, got %d", len(result.Sources))
	}
}
