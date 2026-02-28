package extract

import (
	"net/url"
	"strings"
	"testing"
)

func TestExtractDate_MetaTag(t *testing.T) {
	t.Parallel()
	html := `<!DOCTYPE html>
<html><head>
<meta property="article:published_time" content="2026-02-28T10:00:00Z">
<meta property="og:type" content="article">
<title>Test Article With Date</title>
</head><body>
<article>
<h1>Important Technology News</h1>
<p>This is a substantial article about technology trends in the modern world.
It contains multiple sentences that discuss various aspects of the topic.
The article provides detailed analysis of recent developments in the industry.
Many experts have commented on the significance of these changes for the future.
Furthermore, the impact on society has been profound and far-reaching.</p>
<p>New innovations continue to emerge at an unprecedented pace, transforming
how we live and work. The implications for education, healthcare, and
transportation are particularly noteworthy and deserve careful examination.
Several companies have already begun implementing these technologies in their
products and services, with promising results across multiple sectors.</p>
</article>
</body></html>`

	pageURL, _ := url.Parse("https://example.com/article")
	date := ExtractDate(strings.NewReader(html), pageURL)
	if date == nil {
		t.Fatal("expected date from article:published_time meta tag, got nil")
	}
	if date.Year() != 2026 || date.Month() != 2 || date.Day() != 28 {
		t.Errorf("expected 2026-02-28, got %v", date)
	}
}

func TestExtractDate_NoDate(t *testing.T) {
	t.Parallel()
	html := `<html><body><p>No date anywhere in this page at all</p></body></html>`
	pageURL, _ := url.Parse("https://example.com")
	date := ExtractDate(strings.NewReader(html), pageURL)
	if date != nil {
		t.Errorf("expected nil date for page without date metadata, got %v", date)
	}
}
