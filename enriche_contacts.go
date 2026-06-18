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
	contactsURL, ok := extract.DiscoverContactsPage(homeHTML, result.URL)
	if !ok || contactsURL == item.URL || contactsURL == result.URL {
		return
	}
	e.metrics.contactsPageDiscovered()
	e.logger.DebugContext(ctx, "enriche: contacts page discovered", "name", item.Name, "url", contactsURL)

	contactsHTML := e.fetchContactsHTML(ctx, contactsURL, item)
	if contactsHTML == "" {
		return
	}

	contactsFacts := extract.ExtractFactsForCity(contactsHTML, contactsURL, item.City)
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

// fetchContactsHTML returns the best available HTML for the contacts page:
// the raw fast fetch, upgraded to the rendered DOM when the raw page is thin or
// carries no contact facts (the JS-injected-contacts case the contacts page is
// most likely to be). A render that fails or returns an error shell degrades to
// the raw HTML — a shell is never adopted. Returns "" when the page is
// unreachable.
func (e *Enricher) fetchContactsHTML(ctx context.Context, contactsURL string, item Item) string {
	fr := e.fetchWithRetry(ctx, contactsURL)
	var rawHTML string
	if fr != nil && fr.Status == StatusActive {
		rawHTML = fr.HTML
	}

	// Decide whether to render: render when the raw contacts page is thin or
	// carries no contact facts. The contacts page is rendered more aggressively
	// than the homepage — it is the canonical contact source, so a contactless
	// raw contacts page is the strongest signal the contacts are JS-injected.
	if e.browserFetch == nil {
		return rawHTML
	}
	rawFacts := extract.ExtractFactsForCity(rawHTML, contactsURL, item.City)
	if rawHTML != "" && hasContactFacts(rawFacts) {
		return rawHTML // raw already carries contacts — no render needed
	}

	rendered, err := e.browserFetch(ctx, contactsURL)
	if err != nil || len(rendered) < minRenderShellBytes {
		// Render failed or returned a bot-protection error shell — degrade to the
		// raw fetch (which may itself be ""), never adopt the shell.
		e.metrics.browserRenderError()
		e.logger.DebugContext(ctx, "enriche: contacts page render failed/shell",
			"url", contactsURL, "rendered_bytes", len(rendered), "err", err)
		return rawHTML
	}
	e.metrics.browserRender("contacts_page")
	// Adopt the render only if it yields more contact facts than the raw page.
	renderedFacts := extract.ExtractFactsForCity(rendered, contactsURL, item.City)
	if contactFactCount(renderedFacts) > contactFactCount(rawFacts) {
		return rendered
	}
	return rawHTML
}
