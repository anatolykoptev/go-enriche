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
//
// homeSiteNumbers is the homepage's full candidate phone-number SET (Phase
// P2, additive — extract.CollectSiteNumbersHTML(siteHTML) computed once at
// the caller), reused here ONLY to widen the discovery gate (see
// homepageMissingRichField) — it does not otherwise change this function's
// behavior.
func (e *Enricher) resolveContactsPage(ctx context.Context, item Item, result *Result, r *resolver, homeHTML string, homeFacts extract.Facts, homeSiteNumbers []extract.PhoneNumberFact) {
	if homeHTML == "" {
		return
	}
	// Gate the (network-cost) contacts-page discovery+fetch on the homepage
	// actually MISSING a RICH field that lives on a /contacts page — hours,
	// email, address, or (P2) an ANCHORED phone member. A homepage that already
	// carries all four has nothing to gain from the second fetch, so skip it
	// and spare the round-trip. The gate is deliberately NOT a blunt "homepage
	// has no contact fact": a phone-only homepage still fetches /contacts (that
	// is exactly where the «часы»/email we are after live), it just no longer
	// fetches when the homepage is complete.
	if !homepageMissingRichField(homeFacts, homeSiteNumbers) {
		return
	}
	contactsURL, ok := extract.DiscoverContactsPage(homeHTML, result.URL)
	if !ok || contactsURL == item.URL || contactsURL == result.URL {
		return
	}
	e.metrics.contactsPageDiscovered()
	e.logger.DebugContext(ctx, "enriche: contacts page discovered", "name", item.Name, "url", contactsURL)

	contactsHTML, rawPoisoned, contactsRenderSkipped, contactsSiteNumbers := e.fetchContactsHTML(ctx, contactsURL, item)
	if contactsRenderSkipped {
		result.RenderSkipped = true
	}
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
	// Accumulate the contacts page's full candidate phone-number SET (Phase
	// P2, additive) into the resolver's SiteNumbers sidecar UNCONDITIONALLY,
	// same as the homepage merge (enriche_fetch.go) — deliberately BEFORE the
	// richness-gate early-return below. The richness gate governs only the
	// single-winner Facts MERGE; the full-candidate-set sidecar is an
	// orthogonal, read-only accumulator that must reflect EVERY page actually
	// fetched+parsed, regardless of whether that page won the merge. A
	// /contacts page fetched specifically because the homepage lacked an
	// anchored phone member (see homepageMissingRichField) is exactly the
	// page most likely to carry FEWER total facts than a richer homepage
	// (hours+email+address) while still being the page the anchored phone
	// lives on — gating this accumulation on richness would silently drop
	// the feature's own headline case. rawPoisoned carries the Poison-OR
	// forward into the sidecar (mirrors the rawPoisoned carry-forward onto
	// contactsFacts.PhonePoisoned above): a /contacts page whose RAW fetch
	// ran a DNI widget that rewrote/removed itself before contactsHTML (a
	// render) was captured must still fail closed — see
	// CollectSiteNumbersHTML's pagePoisoned doc comment.
	r.addSiteNumbers(contactsSiteNumbers, item.City)

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
// hours, email, address, or (Phase P2) an ANCHORED phone member. When all four
// are already present the homepage is "complete" for contacts-page purposes
// and the second fetch is skipped (a perf gate — the contacts page could only
// re-supply what we already have). PhonePoisoned is irrelevant to the gate —
// it concerns the phone's TRUSTWORTHINESS, not whether an anchored member
// exists at all.
//
// The phone leg is deliberately keyed on siteNumbers (the DOM-level candidate
// SET), not f.Phone: pickPhoneCandidate already collapsed f.Phone to a single
// winner, which says nothing about whether that winner — or any candidate —
// was ANCHORED (contacts-region/social-link/microdata) versus a bare-body/
// demoted read. A homepage whose only phone signal is unanchored is exactly
// the class a /contacts page is likely to supply the real, anchored number
// for (the P2 calibration gap: Lazermed's real site tel: differs from the
// resolver's top-tier pick).
func homepageMissingRichField(f extract.Facts, siteNumbers []extract.PhoneNumberFact) bool {
	return f.Hours == nil || f.Email == nil || f.Address == nil || !hasAnchoredSiteNumber(siteNumbers)
}

// hasAnchoredSiteNumber reports whether siteNumbers carries at least one
// Anchored candidate (contacts-region tel: / social-link / microdata) — i.e.
// NOT merely a bare-body tel: or a call-tracking-demoted one.
func hasAnchoredSiteNumber(siteNumbers []extract.PhoneNumberFact) bool {
	for _, n := range siteNumbers {
		if n.Anchored {
			return true
		}
	}
	return false
}

// Render-skip signal labels: leg (which fetch leg skipped) and reason (why the
// skip was safe), shared by the homepage (fetchAndExtract) and contacts
// (fetchContactsHTML) legs and the OnBrowserRenderSkipped counter.
const (
	renderSkipReasonRawSufficient = "raw_sufficient"
	renderSkipLegHomepage         = "homepage"
	renderSkipLegContacts         = "contacts"
)

// rawContactsSufficient is the SINGLE render-skip predicate shared by BOTH fetch
// legs — fetchAndExtract's homepage render and fetchContactsHTML's contacts-
// subpage render: the raw fetch already yields the contact data a (15-30s)
// headless render would surface, so the render is unnecessary. True when EITHER
// the raw HTML already carried a single-winner structured contact fact
// (hasContactFacts — the pre-existing no-render case) OR it carried a
// TRUSTWORTHY ANCHORED site number: a non-poisoned page (!poisoned) with at
// least one anchored candidate (hasAnchoredSiteNumber). The anchored arm is
// PROVABLY fail-closed: dniTrustworthy marks an anchored candidate Trustworthy
// exactly when the page is non-DNI, and poisoned already OR-folds HasDNIVendor
// (homeRawPoisoned / rawPoisoned), so "!poisoned && anchored" is equivalent to
// "a Trustworthy anchored raw number exists" — any poison signal forces
// poisoned=true, the anchored arm goes false, and the render always fires.
// Factored into ONE definition (the fitness test asserts the poisoned+anchored
// composite lives here alone) so the two legs can never diverge and launder a
// number through one of them.
func rawContactsSufficient(rawFacts extract.Facts, siteNumbers []extract.PhoneNumberFact, poisoned, renderSkipDisabled bool) bool {
	// ADR-8 kill-switch: with the render-skip disabled, collapse to the
	// pre-v1.30.0 gate — only a single-winner Facts contact counts as
	// sufficient, so the trust-gated anchored-SiteNumber arm never suppresses a
	// render. The ops revert lever for the one-way data consequence of a wrong
	// skip (see WithRenderSkipDisabled); both legs consult this ONE predicate so
	// the switch can never take effect on one leg but not the other.
	if renderSkipDisabled {
		return hasContactFacts(rawFacts)
	}
	return hasContactFacts(rawFacts) || (!poisoned && hasAnchoredSiteNumber(siteNumbers))
}

// fetchContactsHTML returns the best available HTML for the contacts page, the
// RAW fetch's DNI verdict (rawPoisoned), a renderSkipped flag, and the returned
// page's SiteNumbers SET (collected once — see rawSiteNumbers). The HTML is the
// raw fast fetch, upgraded to the rendered DOM when the raw page is thin or
// carries no contact facts (the JS-injected-contacts case the contacts page is
// most likely to be). A render that fails or returns an error shell degrades to
// the raw HTML — a shell is never adopted. Returns ("", …) when unreachable.
//
// rawPoisoned is the PhonePoisoned verdict of the RAW contacts HTML, returned
// separately so the caller can carry it forward even when a (clean-looking)
// render replaces the fact-set — a render must never launder a rotating proxy by
// hiding the call-tracking widget the raw markup exposed.
//
// renderSkipped is true when the result rests on raw-only with NO successful
// render corroboration: EITHER the render was intentionally skipped
// (rawContactsSufficient) OR it was attempted and failed / returned a shell and
// degraded to raw. The caller ORs it into Result.RenderSkipped (see its doc).
//
// siteNumbers is CollectSiteNumbersHTML of the RETURNED html, computed once here
// (reusing the hoisted rawSiteNumbers on every raw-HTML return) so the caller
// need not re-parse the same page — the contacts-leg reuse mirror of
// homeSiteNumbersFor on the homepage leg.
func (e *Enricher) fetchContactsHTML(ctx context.Context, contactsURL string, item Item) (html string, rawPoisoned, renderSkipped bool, siteNumbers []extract.PhoneNumberFact) {
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
	// rawSiteNumbers is the raw contacts page's candidate phone-number SET,
	// collected ONCE here and reused for BOTH the render-skip decision
	// (rawContactsSufficient) AND every raw-HTML return (the caller's SiteNumbers
	// accumulator, siteNumbers), so the raw page is parsed for numbers exactly
	// once — the contacts-leg mirror of the homepage homeSiteNumbersFor hoist.
	rawSiteNumbers := extract.CollectSiteNumbersHTML(rawHTML, rawPoisoned)

	// Decide whether to render: render when the raw contacts page is thin or
	// carries no contact facts. The contacts page is rendered more aggressively
	// than the homepage — it is the canonical contact source, so a contactless
	// raw contacts page is the strongest signal the contacts are JS-injected.
	if e.browserFetch == nil {
		return rawHTML, rawPoisoned, false, rawSiteNumbers
	}
	// Skip the (15-30s) contacts render when the raw contacts page is already
	// contacts-sufficient — the SAME shared predicate the homepage leg uses
	// (rawContactsSufficient), so the two render legs can never diverge. The
	// trust-gated arm (a non-poisoned anchored SiteNumber with no single-winner
	// Facts contact — a branch_json/schema_place number invisible to
	// hasContactFacts) is the new render-avoidance; the hasContactFacts arm is
	// the pre-existing skip (which never rendered, so it is NOT flagged as a new
	// skip).
	if rawHTML != "" && rawContactsSufficient(rawFacts, rawSiteNumbers, rawPoisoned, e.renderSkipDisabled) {
		skipped := !hasContactFacts(rawFacts)
		if skipped {
			e.metrics.browserRenderSkipped(renderSkipLegContacts, renderSkipReasonRawSufficient)
		}
		return rawHTML, rawPoisoned, skipped, rawSiteNumbers
	}

	// This render fires whenever the raw contacts fetch was thin/contactless —
	// including when e.fetchWithRetry above refused the target outright (Guard A
	// on fetch.Fetcher's transport), leaving rawHTML == "". browserFetch is an
	// external delegate this package does not control the dial for, so that
	// refusal is NOT inherited here; gate contactsURL explicitly (SSRF guard,
	// see checkTarget) before handing it off.
	if err := e.checkTarget(ctx, contactsURL); err != nil {
		e.metrics.targetBlocked("contacts_page_render")
		e.logger.DebugContext(ctx, "enriche: contacts page render target blocked", "url", contactsURL, "err", err)
		return rawHTML, rawPoisoned, false, rawSiteNumbers
	}

	rendered, err := e.browserFetch(ctx, contactsURL)
	if err != nil || len(rendered) < minRenderShellBytes {
		// Render ATTEMPTED-BUT-FAILED (error or a bot-protection shell too short
		// to adopt) — degrade to the raw fetch, never adopt the shell. The result
		// now rests on raw-only with NO successful render corroboration, the same
		// state an intentional skip produces, so mark renderSkipped=true (the
		// go-wp Correctable gate must not read a render-failed-degrade as
		// render-confirmed).
		e.metrics.browserRenderError()
		e.logger.DebugContext(ctx, "enriche: contacts page render failed/shell",
			"url", contactsURL, "rendered_bytes", len(rendered), "err", err)
		return rawHTML, rawPoisoned, true, rawSiteNumbers
	}
	e.metrics.browserRender("contacts_page")
	// Adopt the render only if it yields more contact facts than the raw page. A
	// successful render corroborates the page, so renderSkipped stays false even
	// when nothing richer is adopted; siteNumbers reflect whichever HTML wins.
	renderedFacts := extract.ExtractFactsForCity(rendered, contactsURL, item.City)
	if contactFactCount(renderedFacts) > contactFactCount(rawFacts) {
		return rendered, rawPoisoned, false, extract.CollectSiteNumbersHTML(rendered, rawPoisoned)
	}
	return rawHTML, rawPoisoned, false, rawSiteNumbers
}
