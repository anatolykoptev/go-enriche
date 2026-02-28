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
	if err == nil && result != nil && result.Content != "" {
		t.Error("expected empty content for empty HTML")
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
