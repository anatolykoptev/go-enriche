package extract

import (
	"regexp"
	"strings"

	htmltomarkdown "github.com/JohannesKaufmann/html-to-markdown/v2"
	"github.com/PuerkitoBio/goquery"
)

// reWhitespace collapses runs of whitespace into a single space.
var reWhitespace = regexp.MustCompile(`\s+`)

// removeSelectors are HTML elements stripped before text extraction.
var removeSelectors = strings.Join([]string{
	// Standard boilerplate.
	"script", "style", "noscript", "iframe", "svg",
	"header", "footer", "nav", "aside",
	// Ads and non-content.
	".advertisement", ".ad", ".sidebar", ".comments",
	".cookie-banner", ".popup", ".modal", ".newsletter-signup",
	".social-share", ".share-buttons",
	// Navigation and metadata.
	".breadcrumbs", ".breadcrumb", ".tags", ".tag-list",
	".related", ".related-articles", ".related-news",
	".author-info", ".author-bio", ".author-card",
	".subscribe", ".subscription", ".newsletter",
	// Common CMS widget patterns.
	".widget", ".incut", ".banner",
	// ARIA and HTML5 hidden.
	"[role=navigation]", "[role=banner]", "[role=contentinfo]",
	"[aria-hidden=true]", "[hidden]",
}, ", ")

// contentSelectors are tried in order to find the main content element.
const contentSelectors = "article, main, .content, .post-content, .article-content, #content"

// ExtractGoquery uses goquery CSS selectors to extract main content.
// Returns content in the requested format and the page title.
func ExtractGoquery(rawHTML string, format Format) (content string, title string) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(rawHTML))
	if err != nil {
		return "", ""
	}

	// Extract title.
	title = doc.Find("title").First().Text()
	if title == "" {
		doc.Find(`meta[property="og:title"]`).Each(func(_ int, s *goquery.Selection) {
			if title == "" {
				title, _ = s.Attr("content")
			}
		})
	}

	// Remove boilerplate elements.
	doc.Find(removeSelectors).Each(func(_ int, s *goquery.Selection) {
		s.Remove()
	})

	// Find main content container.
	contentSel := doc.Find(contentSelectors).First()
	if contentSel.Length() == 0 {
		contentSel = doc.Find("body")
	}

	switch format {
	case FormatMarkdown:
		rawContent, _ := contentSel.Html()
		if md, mdErr := htmltomarkdown.ConvertString(rawContent); mdErr == nil {
			content = strings.TrimSpace(md)
		}
	default: // FormatText
		content = contentSel.Text()
		content = strings.TrimSpace(content)
		content = reWhitespace.ReplaceAllString(content, " ")
		content = cleanLines(content)
	}

	return content, title
}

// cleanLines removes empty lines and trims each line.
func cleanLines(s string) string {
	lines := strings.Split(s, "\n")
	clean := lines[:0]
	for _, l := range lines {
		if l = strings.TrimSpace(l); l != "" {
			clean = append(clean, l)
		}
	}
	return strings.Join(clean, "\n")
}
