package extract

import (
	"net/url"
	"strings"
	"testing"
)

func TestExtractText_Article(t *testing.T) {
	t.Parallel()
	html := `<html><head><title>Test Article</title></head>
	<body>
	<nav>Navigation menu items here</nav>
	<article>
	<h1>Important News About Technology</h1>
	<p>This is a substantial article about technology trends in the modern world.
	It contains multiple sentences that discuss various aspects of the topic.
	The article provides detailed analysis of recent developments in the industry.
	Many experts have commented on the significance of these changes for the future.</p>
	<p>Furthermore, the impact on society has been profound and far-reaching.
	New innovations continue to emerge at an unprecedented pace, transforming
	how we live and work. The implications for education, healthcare, and
	transportation are particularly noteworthy and deserve careful examination.</p>
	</article>
	<footer>Copyright 2026</footer>
	</body></html>`

	pageURL, _ := url.Parse("https://example.com/article")
	result, err := ExtractText(strings.NewReader(html), pageURL)
	if err != nil {
		t.Fatalf("ExtractText error: %v", err)
	}
	if result == nil {
		t.Fatal("expected result, got nil")
	}
	if result.Content == "" {
		t.Error("expected non-empty content")
	}
	if !strings.Contains(result.Content, "technology") {
		t.Errorf("content should contain 'technology', got: %s", result.Content)
	}
}

func TestExtractText_EmptyHTML(t *testing.T) {
	t.Parallel()
	pageURL, _ := url.Parse("https://example.com")
	result, err := ExtractText(strings.NewReader(""), pageURL)
	if err != nil {
		return // error is acceptable for empty input
	}
	if result != nil && result.Content != "" {
		t.Errorf("expected nil result or empty content for empty HTML, got content: %q", result.Content)
	}
}

func TestExtractText_Metadata(t *testing.T) {
	t.Parallel()
	html := `<html><head>
	<title>My Great Article</title>
	<meta name="author" content="Jane Smith">
	<meta name="description" content="An article about Go programming">
	<meta property="og:site_name" content="TechBlog">
	</head>
	<body>
	<article>
	<p>This is a substantial article about Go programming language.
	It discusses many features and patterns that make Go unique.
	The language has grown significantly in popularity over the years.
	Developers appreciate its simplicity and performance characteristics.</p>
	<p>Go's concurrency model based on goroutines and channels is one of its
	most distinctive features. This model makes it easier to write concurrent
	programs that are both efficient and easy to reason about.</p>
	</article>
	</body></html>`

	pageURL, _ := url.Parse("https://techblog.example.com/go-article")
	result, err := ExtractText(strings.NewReader(html), pageURL)
	if err != nil {
		t.Fatalf("ExtractText error: %v", err)
	}
	if result == nil {
		t.Fatal("expected result, got nil")
	}
	if result.Title == "" {
		t.Error("expected non-empty title")
	}
}

func TestExtractText_FormatMarkdown(t *testing.T) {
	t.Parallel()
	rawHTML := `<html><head><title>Markdown Test</title></head>
	<body>
	<article>
	<h1>Go Programming</h1>
	<p>Learn more at <a href="https://go.dev">the Go website</a>.
	This is a substantial article about Go programming language features.
	It discusses many aspects including concurrency, error handling, and more.
	The language has gained significant adoption in cloud infrastructure.</p>
	<p>Go's standard library is comprehensive and well-documented.
	The testing framework is built-in and encourages good practices.
	Many companies use Go for their backend services and tooling.</p>
	</article>
	</body></html>`

	pageURL, _ := url.Parse("https://example.com/go")
	result, err := ExtractText(strings.NewReader(rawHTML), pageURL, WithFormat(FormatMarkdown))
	if err != nil {
		t.Fatalf("ExtractText error: %v", err)
	}
	if result == nil {
		t.Fatal("expected result, got nil")
	}
	if result.Content == "" {
		t.Error("expected non-empty content")
	}
	// Markdown should preserve links (if ContentNode is available).
	// At minimum, content should contain the text.
	if !strings.Contains(result.Content, "Go") {
		t.Errorf("content should contain 'Go', got: %s", result.Content)
	}
}

func TestExtractText_DefaultFormat(t *testing.T) {
	t.Parallel()
	rawHTML := `<html><head><title>Default</title></head>
	<body><article>
	<p>Simple text content for testing default format behavior.
	This needs to be substantial enough for trafilatura to extract.
	Multiple sentences help ensure the extraction works correctly.
	The default format should return plain text without markup.</p>
	</article></body></html>`

	pageURL, _ := url.Parse("https://example.com")
	// No opts = default FormatText.
	result, err := ExtractText(strings.NewReader(rawHTML), pageURL)
	if err != nil {
		t.Fatalf("ExtractText error: %v", err)
	}
	if result == nil {
		t.Fatal("expected result, got nil")
	}
	if result.Content == "" {
		t.Error("expected non-empty content")
	}
}
