package enriche

import (
	"context"

	"github.com/anatolykoptev/go-enriche/extract"
)

// minRenderShellBytes is the floor below which a render result is treated as a
// bot-protection ERROR SHELL, not a real page. Some SPAs (observed: lenta.com,
// ~245 bytes) return a tiny stub when a headless browser is detected; adopting
// it would drop every contact the raw fetch already had. A render below this
// size is degraded to the raw fetch, never adopted, and counts as a render
// failure for the hit-rate metric.
const minRenderShellBytes = 512

// resolveContactsPage discovers the venue's canonical contacts subpage from the
// already-fetched homepage DOM, fetches it (raw first, render only when the raw
// contacts page is itself thin/contactless), extracts its facts, and merges
// them at sourceOfficialSite — AFTER the homepage facts, so a contacts-page
// value wins on conflict (the resolver takes an equal-or-higher source). The
// contacts page is the canonical, per-venue contact source and is exactly the
// thin/JS page the homepage render trigger misses; it is therefore rendered
// MORE aggressively than the homepage (rendered whenever its raw fetch yields
// no more contact facts than the homepage already had).
//
// homeHTML is the homepage HTML used for link discovery: pass the rendered DOM
// when a homepage render happened (some homepages are JS shells whose nav links
// only exist post-render), else the raw homepage HTML.
//
// All merges flow through the same DNI-aware extractor and the same resolver,
// so the contacts page inherits every anti-fab guarantee (DNI-poison omit,
// social-link-over-rotating-tel:, operator-seed supremacy) for free. A
// discovered page that yields nothing new never displaces the homepage facts.
func (e *Enricher) resolveContactsPage(ctx context.Context, item Item, result *Result, r *resolver, homeHTML string, homeFacts extract.Facts) {
	if homeHTML == "" {
		return
	}
	// Gate the (network-cost) contacts-page discovery+fetch on the homepage
	// actually MISSING a RICH field that lives on a /contacts page — hours, email,
	// or address. A homepage that already carries all three has nothing to gain
	// from the second fetch, so skip it and spare the round-trip. The gate is
	// deliberately NOT a blunt "homepage has no contact fact": a phone-only
	// homepage still fetches /contacts (that is exactly where the «часы»/email we
	// are after live), it just no longer fetches when the homepage is complete.
	if !homepageMissingRichField(homeFacts) {
		return
	}
	contactsURL, ok := extract.DiscoverContactsPage(homeHTML, result.URL)
	if !ok || contactsURL == item.URL || contactsURL == result.URL {
		return
	}
	e.metrics.contactsPageDiscovered()
	e.logger.DebugContext(ctx, "enriche: contacts page discovered", "name", item.Name, "url", contactsURL)

	contactsHTML, rawPoisoned := e.fetchContactsHTML(ctx, contactsURL, item)
	if contactsHTML == "" {
		return
	}

	contactsFacts := extract.ExtractFactsForCity(contactsHTML, contactsURL, item.City)
	// Poison-OR (same invariant as the homepage render path): if the RAW contacts
	// page carried a DNI/call-tracking widget, the page is a DNI site no matter
	// what its post-render DOM looks like (the widget can rewrite/remove itself at
	// runtime). When a render replaced the raw fact-set with a "clean"-looking one,
	// carry the raw poison verdict forward so a rendered contacts page can never
	// LAUNDER a rotating proxy by hiding the widget.
	if rawPoisoned && !contactsFacts.PhonePoisoned {
		contactsFacts.PhonePoisoned = true
		contactsFacts.Phone = nil
	}
	// Adopt the contacts page only when it is STRICTLY richer than the homepage
	// in structured contact facts — a contacts page that surfaced nothing new
	// must not re-merge (and a PhonePoisoned contacts page must still be able to
	// drop a homepage phone, so route a poison verdict through regardless).
	if contactFactCount(contactsFacts) <= contactFactCount(homeFacts) && !contactsFacts.PhonePoisoned {
		e.logger.DebugContext(ctx, "enriche: contacts page no richer than homepage", "name", item.Name)
		return
	}
	if siteHasAnyFact(contactsFacts) {
		e.metrics.siteResolved()
	}
	e.metrics.contactsPageResolved()
	// Merge at sourceOfficialSite AFTER the homepage merge: the resolver takes an
	// equal-or-higher source, so the contacts-page value wins on conflict while
	// poison-lock / operator-seed still outrank it.
	r.mergeSite(contactsFacts)
}

// homepageMissingRichField reports whether the homepage lacks at least one of
// the RICH contact fields that a dedicated /contacts page typically carries:
// hours, email, or address. When all three are already present the homepage is
// "complete" for contacts-page purposes and the second fetch is skipped (a perf
// gate — the contacts page could only re-supply what we already have). Phone is
// intentionally excluded: a phone-only homepage is still missing hours/email/
// address, so it must keep fetching /contacts. PhonePoisoned is irrelevant to
// the gate — it concerns the phone, which the gate does not consider.
func homepageMissingRichField(f extract.Facts) bool {
	return f.Hours == nil || f.Email == nil || f.Address == nil
}

// fetchContactsHTML returns the best available HTML for the contacts page plus
// the RAW fetch's DNI verdict (rawPoisoned). The HTML is the raw fast fetch,
// upgraded to the rendered DOM when the raw page is thin or carries no contact
// facts (the JS-injected-contacts case the contacts page is most likely to be).
// A render that fails or returns an error shell degrades to the raw HTML — a
// shell is never adopted. Returns ("", false) when the page is unreachable.
//
// rawPoisoned is the PhonePoisoned verdict of the RAW contacts HTML, returned
// separately so the caller can carry it forward even when a (clean-looking)
// render replaces the fact-set — a render must never launder a rotating proxy by
// hiding the call-tracking widget the raw markup exposed.
func (e *Enricher) fetchContactsHTML(ctx context.Context, contactsURL string, item Item) (html string, rawPoisoned bool) {
	fr := e.fetchWithRetry(ctx, contactsURL)
	var rawHTML string
	if fr != nil && fr.Status == StatusActive {
		rawHTML = fr.HTML
	}

	// Extract the raw facts once: they drive the render decision below AND carry
	// the DNI verdict the caller needs, so the poison signal survives even when a
	// clean render replaces the adopted fact-set.
	rawFacts := extract.ExtractFactsForCity(rawHTML, contactsURL, item.City)
	// rawPoisoned is true when the raw contacts page is a DNI site. Two cases:
	//   - rawFacts.PhonePoisoned — the raw markup had a tel: candidate the DNI
	//     detector refused (dniOmit);
	//   - HasDNIVendor(rawHTML) — the raw markup carries a DNI loader but the
	//     phone is JS-INJECTED (absent from the raw DOM, so PhonePoisoned is NOT
	//     set: dniOmit needs a candidate). This is exactly the contactless-raw
	//     case that TRIGGERS a render — without this signal a render would surface
	//     the rotating proxy from the post-render DOM and launder it.
	rawPoisoned = rawFacts.PhonePoisoned || extract.HasDNIVendor(rawHTML)

	// Decide whether to render: render when the raw contacts page is thin or
	// carries no contact facts. The contacts page is rendered more aggressively
	// than the homepage — it is the canonical contact source, so a contactless
	// raw contacts page is the strongest signal the contacts are JS-injected.
	if e.browserFetch == nil {
		return rawHTML, rawPoisoned
	}
	if rawHTML != "" && hasContactFacts(rawFacts) {
		return rawHTML, rawPoisoned // raw already carries contacts — no render needed
	}

	// This render fires whenever the raw contacts fetch was thin/contactless —
	// including when e.fetchWithRetry above refused the target outright (Guard A
	// on fetch.Fetcher's transport), leaving rawHTML == "". browserFetch is an
	// external delegate this package does not control the dial for, so that
	// refusal is NOT inherited here; gate contactsURL explicitly (SSRF guard,
	// see checkTarget) before handing it off.
	if err := e.checkTarget(ctx, contactsURL); err != nil {
		e.metrics.browserRenderError()
		e.logger.DebugContext(ctx, "enriche: contacts page render target blocked", "url", contactsURL, "err", err)
		return rawHTML, rawPoisoned
	}

	rendered, err := e.browserFetch(ctx, contactsURL)
	if err != nil || len(rendered) < minRenderShellBytes {
		// Render failed or returned a bot-protection error shell — degrade to the
		// raw fetch (which may itself be ""), never adopt the shell.
		e.metrics.browserRenderError()
		e.logger.DebugContext(ctx, "enriche: contacts page render failed/shell",
			"url", contactsURL, "rendered_bytes", len(rendered), "err", err)
		return rawHTML, rawPoisoned
	}
	e.metrics.browserRender("contacts_page")
	// Adopt the render only if it yields more contact facts than the raw page.
	renderedFacts := extract.ExtractFactsForCity(rendered, contactsURL, item.City)
	if contactFactCount(renderedFacts) > contactFactCount(rawFacts) {
		return rendered, rawPoisoned
	}
	return rawHTML, rawPoisoned
}
