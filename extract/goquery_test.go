package extract

import (
	"strings"
	"testing"
)

func TestExtractGoquery_StripBoilerplate(t *testing.T) {
	t.Parallel()
	rawHTML := `<html><head><title>Test Page</title></head><body>
		<nav>Menu links</nav>
		<article><p>Main article content here with enough text to be meaningful.</p></article>
		<footer>Copyright info</footer>
	</body></html>`

	content, title := ExtractGoquery(rawHTML, FormatText)
	if title != "Test Page" {
		t.Errorf("expected title 'Test Page', got: %q", title)
	}
	if !strings.Contains(content, "Main article content") {
		t.Errorf("expected article content, got: %q", content)
	}
	if strings.Contains(content, "Menu links") {
		t.Error("expected nav to be stripped")
	}
	if strings.Contains(content, "Copyright info") {
		t.Error("expected footer to be stripped")
	}
}

func TestExtractGoquery_Markdown(t *testing.T) {
	t.Parallel()
	rawHTML := `<html><head><title>MD Test</title></head><body>
		<article>
			<h2>Section</h2>
			<p>Visit <a href="https://example.com">example</a> for details.</p>
		</article>
	</body></html>`

	content, _ := ExtractGoquery(rawHTML, FormatMarkdown)
	if !strings.Contains(content, "[example](https://example.com)") {
		t.Errorf("expected markdown link, got: %q", content)
	}
	if !strings.Contains(content, "## Section") {
		t.Errorf("expected markdown heading, got: %q", content)
	}
}

func TestExtractGoquery_OGTitle(t *testing.T) {
	t.Parallel()
	rawHTML := `<html><head><meta property="og:title" content="OG Title"></head>
		<body><p>Content</p></body></html>`

	_, title := ExtractGoquery(rawHTML, FormatText)
	if title != "OG Title" {
		t.Errorf("expected OG title, got: %q", title)
	}
}

func TestExtractGoquery_FallbackToBody(t *testing.T) {
	t.Parallel()
	rawHTML := `<html><head><title>No Article</title></head>
		<body><p>Body content without article tags.</p></body></html>`

	content, _ := ExtractGoquery(rawHTML, FormatText)
	if !strings.Contains(content, "Body content") {
		t.Errorf("expected body fallback content, got: %q", content)
	}
}

func TestExtractGoquery_InvalidHTML(t *testing.T) {
	t.Parallel()
	content, title := ExtractGoquery("", FormatText)
	// goquery handles empty input gracefully
	_ = content
	_ = title
}

func TestCleanLines(t *testing.T) {
	t.Parallel()
	input := "line1\n\n  \n  line2  \n\nline3"
	got := cleanLines(input)
	if got != "line1\nline2\nline3" {
		t.Errorf("cleanLines = %q", got)
	}
}
