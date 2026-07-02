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

// stripBoilerplate removes non-content HTML elements (script/style/nav/ads/...
// — see removeSelectors) from doc in place. Used ONLY by ExtractGoquery's own
// main-content extraction below — NOT by applyRegexFallback (extract/facts.go),
// which needs the much narrower stripNoise (see its doc comment for why).
func stripBoilerplate(doc *goquery.Document) {
	removeElements(doc, removeSelectors)
}

// removeNoiseSelectors are non-textual elements that can never legitimately
// carry a phone/address/price value: script/style payloads, embedded
// iframes/SVG icons, and the document <head> (title/meta/inline <style>).
// Deliberately narrower than removeSelectors: applyRegexFallback's
// junk-avoidance scoping (extract/facts.go) must strip a CSS/script decimal
// before it can misread as a phone number (the Novoclinic bug — see
// stripNoiseHTML in facts.go) WITHOUT also stripping header/footer/nav/aside/
// .widget/.banner/[role=contentinfo] the way removeSelectors does. Those
// containers legitimately carry a RU-SMB venue's plain-text contact info —
// on a real Tilda-built page (novoclinicspb.ru) EVERY content block,
// including the venue's own phone number, carries a literal "widget" class
// token (Tilda's universal block-wrapper convention, not a sidebar widget),
// so removeSelectors' ".widget" selector would strip the phone itself.
var removeNoiseSelectors = strings.Join([]string{
	"script", "style", "noscript", "template", "svg", "head",
}, ", ")

// stripNoise removes only the non-textual, never-a-contact-value elements
// (see removeNoiseSelectors) from doc in place — the narrow scope
// applyRegexFallback's junk-avoidance needs, as opposed to stripBoilerplate's
// full main-content strip.
func stripNoise(doc *goquery.Document) {
	removeElements(doc, removeNoiseSelectors)
}

// removeElements removes every element doc matches against the given CSS
// selector list, in place. Shared control flow for stripBoilerplate and
// stripNoise — they differ ONLY by which selector list governs the strip;
// both named entry points (and their distinct selector consts) stay so
// callers keep a semantically clear name for which scope they want.
func removeElements(doc *goquery.Document, selectors string) {
	doc.Find(selectors).Each(func(_ int, s *goquery.Selection) {
		s.Remove()
	})
}

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
	stripBoilerplate(doc)

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
