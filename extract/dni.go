package extract

import (
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// dniVendorSignatures maps a canonical DNI/call-tracking vendor name to the
// lowercase substrings that identify it in an already-fetched page. A match is
// sought in:
//   - every <script src> URL (the vendor loader), and
//   - inline <script> text (a global config token, e.g. window.roistatProjectId).
//
// These are dynamic-number-insertion (DNI) vendors: at runtime they rewrite the
// page's displayed tel:/microdata phone with a rotating tracking proxy. A static
// snapshot sees one slot; a later fetch sees another. So when any of these is
// present, an injected tel:/microdata phone is NOT the venue's own line and must
// not be asserted as authoritative — only a DNI-immune source (a hard-coded
// social-link phone) can be trusted. Signature match over already-fetched HTML
// only: no cross-fetch rotation detection, no screenshot.
//
// Substrings are deliberately vendor-specific (host/product tokens), not generic
// words like "widget" or "phone", so a normal WordPress/Bitrix wrapper or a
// gtag/analytics loader never trips detection.
var dniVendorSignatures = []struct {
	vendor string
	tokens []string
}{
	{"roistat", []string{"roistat"}},
	{"calltouch", []string{"calltouch"}},
	{"comagic", []string{"comagic"}},
	{"mango", []string{"mango-office", "mango.office", "mangooffice", "mango.js"}},
	{"callibri", []string{"callibri"}},
	{"uis", []string{"uiscom", "uis-comagic", "uiscall"}},
}

// detectDNIVendor reports whether the page carries a known DNI/call-tracking
// vendor signature, and which vendor. Pure DOM read, no network I/O. Returns
// ("", false) for a clean page.
func detectDNIVendor(doc *goquery.Document) (string, bool) {
	if doc == nil {
		return "", false
	}

	// Collect the haystacks once: all script-src URLs + inline script bodies.
	var hay strings.Builder
	doc.Find("script").Each(func(_ int, s *goquery.Selection) {
		if src, ok := s.Attr("src"); ok {
			hay.WriteString(strings.ToLower(src))
			hay.WriteByte('\n')
		}
		hay.WriteString(strings.ToLower(s.Text()))
		hay.WriteByte('\n')
	})
	h := hay.String()
	if h == "" {
		return "", false
	}

	for _, sig := range dniVendorSignatures {
		for _, tok := range sig.tokens {
			if strings.Contains(h, tok) {
				return sig.vendor, true
			}
		}
	}
	return "", false
}
