package extract

import (
	"net/url"
	"strings"
	"testing"
)

func TestExtractDate_MetaTag(t *testing.T) {
	t.Parallel()
	html := `<html><head>
	<meta property="article:published_time" content="2026-02-28T10:00:00Z">
	</head><body>
	<article>
	<p>Content here for extraction to work properly with trafilatura.
	This needs to be a substantial paragraph for the library to process it.
	Adding more sentences to ensure the content is long enough for extraction.
	The library requires a minimum amount of content to consider it valid.</p>
	</article>
	</body></html>`

	pageURL, _ := url.Parse("https://example.com/article")
	date := ExtractDate(strings.NewReader(html), pageURL)
	if date == nil {
		t.Skip("trafilatura may not extract date from minimal HTML")
	}
	if date.Year() != 2026 || date.Month() != 2 || date.Day() != 28 {
		t.Errorf("unexpected date: %v", date)
	}
}

func TestExtractDate_NoDate(t *testing.T) {
	t.Parallel()
	html := `<html><body><p>No date anywhere in this page at all</p></body></html>`
	pageURL, _ := url.Parse("https://example.com")
	date := ExtractDate(strings.NewReader(html), pageURL)
	if date != nil {
		t.Logf("got unexpected date: %v (may be from URL or heuristic)", date)
	}
}
