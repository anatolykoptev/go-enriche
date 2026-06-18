package extract

import (
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// ContactSlug classifies a known contacts-page URL slug by priority. The
// discovery rule is SLUG-FIRST and MULTILINGUAL: sites span RU (piter.now) and
// EN (hully.day) and future locales, where the visible label («Контакты» /
// Contacts / Kontakt) is language-dependent and synonym-heavy, but the URL path
// slug is stable. A contact-family slug outranks an about-family slug.
type contactSlugClass int

const (
	slugNone        contactSlugClass = iota
	slugAboutFamily                  // about / about-us / o-nas — secondary, lower priority
	slugContact                      // contact / contacts / kontakty — the canonical contacts page
)

// contactSlugs is the curated multilingual contacts-page slug set, matched
// against a URL path's last SEGMENT (normalized lowercase, trailing slash
// stripped) — NOT a substring. Segment matching is deliberate: a substring
// rule would match "/contact-lenses-shop" (a product page), shipping the wrong
// page as the contacts source. New locales extend this table.
//
// The about-family is kept at a LOWER priority (slugAboutFamily): an "О нас" /
// "About" page sometimes carries contacts when a site has no dedicated contacts
// page, but a real contacts page always wins when both exist.
var contactSlugs = map[string]contactSlugClass{
	// English
	"contact":      slugContact,
	"contacts":     slugContact,
	"contact-us":   slugContact,
	"contactus":    slugContact,
	"get-in-touch": slugContact,
	// Russian (Cyrillic + translit)
	"контакты": slugContact,
	"контакт":  slugContact,
	"kontakty": slugContact,
	"kontakt":  slugContact,
	// German
	"kontakte": slugContact,
	// French
	"nous-contacter": slugContact,
	// Spanish
	"contacto": slugContact,
	// Italian
	"contatti": slugContact,
	// Portuguese
	"contato": slugContact,
	// about-family (lower priority)
	"about":      slugAboutFamily,
	"about-us":   slugAboutFamily,
	"o-nas":      slugAboutFamily,
	"o-kompanii": slugAboutFamily,
}

// contactLinkText holds multilingual visible-text labels used as the SECONDARY
// discovery signal (a tiebreaker, and the only signal for CMS-numeric slugs
// such as /page/5 or /node/12 where the path segment is meaningless). Compared
// case-insensitively against the trimmed anchor text. Kept short and
// unambiguous — generic words ("info", "о") would over-match.
var contactLinkText = []string{
	"контакты", "контакт", "contacts", "contact us", "contact",
	"kontakt", "kontakte", "kontakty", "nous contacter", "contacto",
	"contatti", "contato",
}

// contactPageCandidate is one discovered contacts-page link with its ranking
// inputs: the slug class (primary) and whether the visible text matched
// (secondary tiebreaker).
type contactPageCandidate struct {
	absURL    string
	slugClass contactSlugClass
	textMatch bool
}

// DiscoverContactsPage scans an already-fetched homepage's links for the
// venue's canonical contacts page and returns its absolute URL.
//
// It performs ZERO network I/O — a deterministic read over the homepage HTML
// the enricher already fetched. Discovery is slug-first (URL path last segment
// matched against the multilingual contactSlugs table), with visible link text
// as a secondary tiebreaker / fallback for CMS-numeric slugs.
//
// Returns (absoluteURL, true) for the best same-origin contacts-page link, or
// ("", false) when none is found — in which case the caller stays on the
// homepage (never worse than today). The discovered URL is guaranteed
// same-origin as baseURL and distinct from it (a self-link to the homepage is
// not a contacts page).
func DiscoverContactsPage(homeHTML, baseURL string) (string, bool) {
	doc, err := documentFromHTML(homeHTML)
	if err != nil || doc == nil {
		return "", false
	}
	base, err := url.Parse(baseURL)
	if err != nil || base.Host == "" {
		return "", false
	}

	var best *contactPageCandidate
	doc.Find("a[href]").Each(func(_ int, s *goquery.Selection) {
		href, ok := s.Attr("href")
		if !ok {
			return
		}
		abs, ok := resolveSameOrigin(base, href)
		if !ok {
			return
		}
		slugClass := classifyContactSlug(abs)
		textMatch := matchesContactText(s.Text())
		if slugClass == slugNone && !textMatch {
			return
		}
		cand := contactPageCandidate{absURL: abs, slugClass: slugClass, textMatch: textMatch}
		if best == nil || betterContactCandidate(cand, *best) {
			c := cand
			best = &c
		}
	})

	if best == nil {
		return "", false
	}
	return best.absURL, true
}

// resolveSameOrigin resolves href against base and returns the absolute URL
// only when it is on the same host as base AND distinct from the homepage
// itself (a link back to "/" is not a contacts page). Fragment-only and
// non-http(s) links (mailto:, tel:, javascript:) are rejected.
func resolveSameOrigin(base *url.URL, href string) (string, bool) {
	href = strings.TrimSpace(href)
	if href == "" || strings.HasPrefix(href, "#") {
		return "", false
	}
	ref, err := url.Parse(href)
	if err != nil {
		return "", false
	}
	abs := base.ResolveReference(ref)
	if abs.Scheme != "http" && abs.Scheme != "https" {
		return "", false
	}
	if abs.Host != base.Host {
		return "", false // off-origin link — never follow
	}
	// Drop fragment/query for the self-link comparison; a "/#contacts" or
	// "/?x=1" that resolves to the homepage path is not a separate page.
	abs.Fragment = ""
	if normalizePath(abs.Path) == normalizePath(base.Path) {
		return "", false // self-link to the homepage
	}
	return abs.String(), true
}

// classifyContactSlug returns the slug class of a URL's last path segment,
// matched against contactSlugs by exact segment (NOT substring). Returns
// slugNone when the last segment is not a known contacts slug.
func classifyContactSlug(rawURL string) contactSlugClass {
	u, err := url.Parse(rawURL)
	if err != nil {
		return slugNone
	}
	seg := lastPathSegment(u.Path)
	if seg == "" {
		return slugNone
	}
	return contactSlugs[seg]
}

// lastPathSegment returns the final non-empty path segment, lowercased, with
// any trailing slash already stripped by the split. "/contacts/" -> "contacts",
// "/about/team/" -> "team", "/" -> "".
func lastPathSegment(path string) string {
	path = normalizePath(path)
	if path == "" {
		return ""
	}
	segs := strings.Split(path, "/")
	return strings.ToLower(segs[len(segs)-1])
}

// normalizePath lowercases nothing (callers lowercase the segment) but strips a
// single trailing slash and a leading slash run so two equivalent paths compare
// equal. "/" -> "", "/contacts/" -> "contacts", "/contacts" -> "contacts".
func normalizePath(path string) string {
	path = strings.Trim(path, "/")
	return path
}

// matchesContactText reports whether an anchor's visible text is a multilingual
// contacts label (secondary discovery signal). Matched case-insensitively
// against the whole trimmed text being exactly one of the known labels — an
// exact-ish match avoids "контактная информация о доставке" style false hits
// while still catching the short nav labels that are the real signal.
func matchesContactText(text string) bool {
	t := strings.ToLower(strings.TrimSpace(text))
	if t == "" {
		return false
	}
	for _, label := range contactLinkText {
		if t == label {
			return true
		}
	}
	return false
}

// betterContactCandidate reports whether a should rank above b. Primary key:
// slug class (slugContact > slugAboutFamily > slugNone). Tiebreaker: a visible
// text match. A slug-class win is decisive — a real /contacts URL beats an
// about-page even if the about-page's link text also matched.
func betterContactCandidate(a, b contactPageCandidate) bool {
	if a.slugClass != b.slugClass {
		return a.slugClass > b.slugClass
	}
	return a.textMatch && !b.textMatch
}
