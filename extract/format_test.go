package extract

import (
	"strings"
	"testing"

	trafilatura "github.com/markusmobius/go-trafilatura"
	"golang.org/x/net/html"
)

func TestRenderContentNodeAsMarkdown(t *testing.T) {
	t.Parallel()

	rawHTML := `<div><h1>Title</h1><p>Hello <a href="https://example.com">world</a></p></div>`
	node, err := html.Parse(strings.NewReader(rawHTML))
	if err != nil {
		t.Fatal(err)
	}

	result := &trafilatura.ExtractResult{ContentNode: node}
	md := renderContentNodeAsMarkdown(result)
	if md == "" {
		t.Fatal("expected non-empty markdown")
	}
	if !strings.Contains(md, "[world](https://example.com)") {
		t.Errorf("expected markdown link, got: %s", md)
	}
	if !strings.Contains(md, "# Title") {
		t.Errorf("expected markdown heading, got: %s", md)
	}
}

func TestRenderContentNode_NilNode(t *testing.T) {
	t.Parallel()
	result := &trafilatura.ExtractResult{ContentNode: nil}
	if got := renderContentNode(result); got != "" {
		t.Errorf("expected empty string for nil node, got: %q", got)
	}
}

func TestRenderContentNodeAsMarkdown_NilNode(t *testing.T) {
	t.Parallel()
	result := &trafilatura.ExtractResult{ContentNode: nil}
	if got := renderContentNodeAsMarkdown(result); got != "" {
		t.Errorf("expected empty string for nil node, got: %q", got)
	}
}
