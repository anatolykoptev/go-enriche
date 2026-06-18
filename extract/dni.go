package extract

import (
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// dniVendorSignatures maps a canonical DNI/call-tracking vendor name to the
// lowercase tokens that identify it in an already-fetched page. Detection is
// split by where the token may appear, because the two surfaces carry very
// different false-positive risk:
//
//   - srcTokens match a <script src> URL (the vendor loader host/path). A loader
//     URL like cloud.roistat.com or app.comagic.ru is unambiguous: the page is
//     actively running the vendor. A plain substring match is safe here.
//
//   - configTokens match INLINE <script> text, but ONLY as a configuration /
//     initialization pattern (e.g. `window.roistat`, `new Comagic`, `Calltouch(`,
//     `mango-office`). A bare mention of the vendor name in inline text — a blog
//     post about call-tracking, a competitor reference, a code sample — must NOT
//     trip detection, because a false DNI verdict needlessly OMITS a real venue
//     phone. So configTokens are deliberately initialization-shaped, not the bare
//     vendor word.
//
// These are dynamic-number-insertion (DNI) vendors: at runtime they rewrite the
// page's displayed tel:/microdata phone with a rotating tracking proxy. A static
// snapshot sees one slot; a later fetch sees another. So when any is actively
// present, an injected tel:/microdata phone is NOT the venue's own line — only a
// DNI-immune source (a hard-coded social-link phone) can be trusted. Signature
// match over already-fetched HTML only: no cross-fetch rotation, no screenshot.
var dniVendorSignatures = []struct {
	vendor       string
	srcTokens    []string // matched in a <script src> URL (vendor loader)
	configTokens []string // matched in inline <script> text, init-shaped only
}{
	{
		vendor:       "roistat",
		srcTokens:    []string{"roistat.com", "roistat.js"},
		configTokens: []string{"window.roistat", "roistatprojectid", "roistat.init", "roistatcompanyid"},
	},
	{
		vendor:       "calltouch",
		srcTokens:    []string{"calltouch.ru", "calltouch.js", "mod.calltouch"},
		configTokens: []string{"window.ct(", "ct_data", "calltouch_session", "calltouchsiteid"},
	},
	{
		vendor:       "comagic",
		srcTokens:    []string{"comagic.ru", "comagic.io", "comagic.js"},
		configTokens: []string{"comagic.", "window.comagic", "__comagic"},
	},
	{
		vendor:       "mango",
		srcTokens:    []string{"mango-office.ru", "mango.office", "mangooffice", "mango.js"},
		configTokens: []string{"mango-office", "mangooffice", "mango_office", "window.mango", "mangoobject"},
	},
	{
		vendor:       "callibri",
		srcTokens:    []string{"callibri.ru", "callibri.js", "cdn.callibri"},
		configTokens: []string{"window.callibri", "callibri_", "__callibri"},
	},
	{
		vendor:       "uis",
		srcTokens:    []string{"uiscom.ru", "uis-comagic", "uiscall"},
		configTokens: []string{"window.uis", "uiscom.", "__uis"},
	},
}

// detectDNIVendor reports whether the page ACTIVELY runs a known DNI/call-
// tracking vendor, and which one. A vendor is "active" when its loader URL
// appears in a <script src> OR an init-shaped config token appears in inline
// <script> text. A page that merely mentions a vendor name in prose/inline body
// (a blog, a competitor reference) does NOT match — that would needlessly omit a
// real phone. Pure DOM read, no network I/O. Returns ("", false) for a clean
// page.
func detectDNIVendor(doc *goquery.Document) (string, bool) {
	if doc == nil {
		return "", false
	}

	// Collect the two surfaces separately so a config token is never matched
	// against a loose body mention and vice versa.
	var srcHay, cfgHay strings.Builder
	doc.Find("script").Each(func(_ int, s *goquery.Selection) {
		if src, ok := s.Attr("src"); ok {
			srcHay.WriteString(strings.ToLower(src))
			srcHay.WriteByte('\n')
			return // a <script src> has no meaningful inline body
		}
		cfgHay.WriteString(strings.ToLower(s.Text()))
		cfgHay.WriteByte('\n')
	})
	src := srcHay.String()
	cfg := cfgHay.String()
	if src == "" && cfg == "" {
		return "", false
	}

	for _, sig := range dniVendorSignatures {
		for _, tok := range sig.srcTokens {
			if src != "" && strings.Contains(src, tok) {
				return sig.vendor, true
			}
		}
		for _, tok := range sig.configTokens {
			if cfg != "" && strings.Contains(cfg, tok) {
				return sig.vendor, true
			}
		}
	}
	return "", false
}
