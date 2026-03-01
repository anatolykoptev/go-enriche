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

	ddg := NewDDG(mock, WithDDGMaxResults(5))
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

	ddg := NewDDG(mock)
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

	ddg := NewDDG(mock, WithDDGMaxResults(2))
	result, err := ddg.Search(context.Background(), "test", "")
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}
	if len(result.Sources) > 2 {
		t.Errorf("expected max 2 sources, got %d", len(result.Sources))
	}
}
