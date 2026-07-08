package enriche

import (
	"context"
	"math/rand/v2"
	"net/url"
	"strings"
	"time"

	"github.com/anatolykoptev/go-enriche/extract"
	"github.com/anatolykoptev/go-enriche/fetch"
)

func (e *Enricher) fetchAndExtract(ctx context.Context, item Item, result *Result, r *resolver) { //nolint:gocognit,cyclop,funlen // pre-existing fetch/extract/fallback orchestration complexity; this change adds only a straight-line source-coord re-seed
	// Start ox-browser readability in parallel (if configured).
	type oxResult struct {
		content string
		title   string
		author  string
		excerpt string
	}
	var oxCh chan *oxResult
	if e.oxBrowser != nil {
		oxCh = make(chan *oxResult, 1)
		go func() {
			// item.URL is caller-supplied (e.g. an advertiser website field) and
			// ox-browser fetches it server-side in a different process, so
			// fetch.Fetcher's dial-time guard cannot protect this hop — gate the
			// target explicitly before dispatch (SSRF guard, see checkTarget).
			if err := e.checkTarget(ctx, item.URL); err != nil {
				e.metrics.targetBlocked("ox_browser_item")
				e.logger.DebugContext(ctx, "enriche: ox-browser target blocked", "url", item.URL, "err", err)
				oxCh <- nil
				return
			}
			// Time the ox-browser leg. It runs in PARALLEL with the direct
			// homepage fetch, so this overlaps PhaseHomepageFetch and is NOT
			// additive to total — but the render is the slowest single leg in the
			// pipeline, and it was previously the only fetch leg with no timing.
			// Its output is consumed (fallback when the direct fetch fails, or a
			// longer-content merge on success — see mergeOxBrowserResult), so the
			// leg is not wasted; it was just unmeasured.
			oxStart := time.Now()
			ox, err := e.oxBrowser.Extract(ctx, item.URL)
			e.metrics.phaseTiming(PhaseHomepageOxBrowser, time.Since(oxStart).Seconds())
			if err != nil {
				e.logger.DebugContext(ctx, "enriche: ox-browser failed", "url", item.URL, "err", err)
				oxCh <- nil
				return
			}
			oxCh <- &oxResult{content: ox.Content, title: ox.Title, author: ox.Author, excerpt: ox.Excerpt}
		}()
	}

	homeFetchStart := time.Now()
	fr := e.fetchWithRetry(ctx, item.URL)
	e.metrics.phaseTiming(PhaseHomepageFetch, time.Since(homeFetchStart).Seconds())
	if fr == nil { //nolint:nestif // pre-existing nested fetch-error handling
		// If ox-browser is running, wait for it — it may have succeeded.
		if oxCh != nil {
			if ox := <-oxCh; ox != nil && len(ox.content) > 0 {
				result.Status = fetch.StatusActive
				result.Content = ox.content
				if e.maxContentLen > 0 {
					result.Content = truncateRunes(result.Content, e.maxContentLen)
				}
				result.Metadata = &ContentMeta{Title: ox.title, Author: ox.author, Description: ox.excerpt}
				return
			}
		}
		result.Status = fetch.StatusUnreachable
		return
	}

	result.Status = fr.Status
	if fr.FinalURL != "" {
		result.URL = fr.FinalURL
	}

	if fr.Status != fetch.StatusActive {
		e.logger.DebugContext(ctx, "enriche: fetch non-active", "url", item.URL, "status", fr.Status, "code", fr.StatusCode)
		if fr.Status == fetch.StatusUnreachable {
			e.metrics.fetchError()
		}
		return
	}

	e.logger.DebugContext(ctx, "enriche: fetched", "url", item.URL, "status", fr.Status, "code", fr.StatusCode)

	// Extract text + metadata via trafilatura.
	pageURL, _ := url.Parse(item.URL)
	textResult, textErr := extract.ExtractText(
		strings.NewReader(fr.HTML), pageURL, extract.WithFormat(e.format),
	)
	if textErr == nil && textResult != nil {
		result.Content = textResult.Content
		if e.maxContentLen > 0 {
			result.Content = truncateRunes(result.Content, e.maxContentLen)
		}
		result.Metadata = &ContentMeta{
			Title:       textResult.Title,
			Author:      textResult.Author,
			Description: textResult.Description,
			Language:    textResult.Language,
			SiteName:    textResult.SiteName,
		}
		if !textResult.Date.IsZero() {
			t := textResult.Date
			result.PublishedAt = &t
		}
		if textResult.Image != "" {
			result.Image = &textResult.Image
		}
	}

	// Goquery Tier 2 fallback for thin content.
	e.goqueryFallback(fr.HTML, result)

	// Browser fallback (full-JS render) for the official site. Two triggers:
	//   1. thin readability content (< minExtractChars) — the page is a JS
	//      shell whose article text only exists post-render; the existing
	//      content-quality path.
	//   2. absent contact facts — the raw HTML carried NO phone/address/hours,
	//      so a contacts/hours block injected by JS (SPA venue sites: Tilda,
	//      Bitrix, React) is invisible to a raw fetch. Render to surface it.
	// rawFacts is extracted ONCE from the raw HTML and reused below as the
	// merge input when no render happens, so a render is the only added cost.
	rawFacts := extract.ExtractFactsForCity(fr.HTML, item.URL, item.City)
	// homeRawPoisoned is the raw homepage's DNI verdict, computed once here
	// and reused wherever a "was this raw page poisoned" signal is needed:
	// the render-adoption carry-forward below (Facts.Phone) AND the
	// SiteNumbers sidecar accumulation further down. rawFacts.PhonePoisoned
	// ALONE is not enough: resolvePhoneForCityDNI never even checks for a
	// DNI vendor when the raw page has ZERO phone candidates to poison (it
	// short-circuits before that check), so a raw homepage that carries a
	// DNI script but no phone candidate at all — exactly the case that
	// forces the render below — would leave PhonePoisoned false despite
	// the vendor being active. extract.HasDNIVendor checks vendor presence
	// directly, independent of candidate count, closing that gap — see
	// CollectSiteNumbersHTML's pagePoisoned doc comment and
	// fetchContactsHTML's rawPoisoned in enriche_contacts.go (same pattern,
	// contacts-page mirror).
	homeRawPoisoned := rawFacts.PhonePoisoned || extract.HasDNIVendor(fr.HTML)
	siteFacts := rawFacts
	// siteHTML is the HTML that backs siteFacts — fr.HTML unless a render
	// below is adopted (siteFacts reassigned to renderedFacts), so the
	// SiteNumbers accumulator (see r.addSiteNumbers below) always scans the
	// same page the merged facts came from.
	siteHTML := fr.HTML
	// discoveryHTML is the homepage HTML used to discover the contacts subpage:
	// the rendered DOM when a homepage render happened (some homepages are JS
	// shells whose nav links only exist post-render), else the raw HTML.
	discoveryHTML := fr.HTML
	thinContent := len([]rune(result.Content)) < minExtractChars
	// rawSiteNumbers is the raw homepage's full candidate phone-number SET,
	// collected ONCE here on fr.HTML BEFORE the render decision and reused as the
	// SiteNumbers accumulator input when no render is adopted (homeSiteNumbersFor)
	// so the raw page is parsed for numbers exactly once.
	rawSiteNumbers := extract.CollectSiteNumbersHTML(fr.HTML, homeRawPoisoned)
	// rawSufficient: the raw fetch already yields the contact data a render would
	// surface — either a single-winner Facts contact (the pre-existing no-render
	// case) OR a trustworthy anchored site number (branch_json/schema_place,
	// invisible to hasContactFacts). The trust-gated arm is fail-closed: any
	// poison signal forces homeRawPoisoned=true and the render always fires (see
	// rawContactsSufficient). thinContent stays an INDEPENDENT OR — a genuinely
	// thin page always renders regardless of SiteNumbers.
	rawSufficient := rawContactsSufficient(rawFacts, rawSiteNumbers, homeRawPoisoned, e.renderSkipDisabled)
	shouldRenderHomepage := e.browserFetch != nil && (thinContent || !rawSufficient)
	// Record the trust-gated render-skip (provenance flag + per-leg counter) when
	// the render the OLD gate WOULD have fired (absent contacts, non-thin) was
	// avoided by a trustworthy anchored raw number — all branching lives in the
	// helper, not this F-graded orchestrator (ADR-5 complexity ceiling).
	e.noteHomepageRenderSkip(result, e.browserFetch != nil, thinContent, hasContactFacts(rawFacts), rawSufficient)
	if shouldRenderHomepage {
		// item.URL is caller-supplied (e.g. an advertiser website field) and
		// browserFetch dispatches it to an out-of-process render service
		// (go-wowa), so fetch.Fetcher's dial-time guard on the RAW fetch above
		// does not carry over to this hop — gate the target explicitly (SSRF
		// guard, see checkTarget) before handing it off. thinContent/absent-
		// contacts is a common trigger for SPA/Tilda/Bitrix/React venue
		// homepages, so this path is attacker-craftable, not a corner case.
		if err := e.checkTarget(ctx, item.URL); err != nil {
			e.metrics.targetBlocked("homepage_render")
			e.logger.DebugContext(ctx, "enriche: homepage render target blocked", "url", item.URL, "err", err)
			shouldRenderHomepage = false
		}
	}
	if shouldRenderHomepage {
		reason := renderReason(thinContent)
		homeRenderStart := time.Now()
		rendered, err := e.browserFetch(ctx, item.URL)
		e.metrics.phaseTiming(PhaseHomepageRender, time.Since(homeRenderStart).Seconds())
		switch {
		case err == nil && len(rendered) >= minRenderShellBytes:
			e.metrics.browserRender(reason)
			discoveryHTML = rendered
			// Content path: adopt rendered text only if it is longer.
			e.browserFallback(rendered, item.URL, result)
			// Facts path: adopt the rendered DOM only when it yields STRICTLY
			// MORE contact facts than the raw HTML did — a render that surfaces
			// nothing new must not displace the raw extraction (and must never
			// let a render-only artifact override a raw contact fact).
			renderedFacts := extract.ExtractFactsForCity(rendered, item.URL, item.City)
			if contactFactCount(renderedFacts) > contactFactCount(rawFacts) {
				// Poison-OR: a DNI verdict from the RAW HTML must survive even when
				// the (clean) rendered DOM replaces the fact-set. A page whose raw
				// markup carries a call-tracking widget is a DNI site regardless of
				// what the post-render DOM looks like (the widget may rewrite/remove
				// itself at runtime). Carrying the poison flag forward keeps the
				// rotating-proxy phone refused at the resolver. homeRawPoisoned (not
				// rawFacts.PhonePoisoned alone) so a raw page with a DNI vendor but
				// ZERO phone candidates still forces the carry-forward — see
				// homeRawPoisoned's doc comment above.
				if homeRawPoisoned && !renderedFacts.PhonePoisoned {
					renderedFacts.PhonePoisoned = true
					renderedFacts.Phone = nil
				}
				siteFacts = renderedFacts
				siteHTML = rendered
			}
		default:
			// Render failed or returned a bot-protection error shell (too short)
			// — keep the raw extraction, never adopt the shell, and record the
			// failure so the real go-wowa hit-rate is observable.
			e.metrics.browserRenderError()
			e.logger.DebugContext(ctx, "enriche: homepage render failed/shell",
				"url", item.URL, "rendered_bytes", len(rendered), "err", err)
			// Render ATTEMPTED-BUT-FAILED → the result now rests on the SAME
			// raw-only Facts/SiteNumbers an intentional skip produces, with NO
			// successful render corroboration. Mark RenderSkipped so the go-wp
			// Correctable gate treats a render-failed-degrade like a skip, not as
			// render-confirmed (a NON-poisoned thin page whose render fails and
			// falls back to raw with SiteNumbers is the reachable wrong-with-number
			// case; a poisoned page still drops its phone via Poison-OR).
			result.RenderSkipped = true
		}
	}

	// Merge ox-browser result: pick longer content.
	if oxCh != nil {
		if ox := <-oxCh; ox != nil {
			e.mergeOxBrowserResult(ox.content, ox.title, ox.author, ox.excerpt, result)
		}
	}

	// MERGE the official-site facts (raw or rendered, decided above) through the
	// source-priority resolver at sourceOfficialSite — site values override any
	// maps/search value on conflict, while maps fills only what the site left
	// empty. The resolver, not assignment order, decides winners. siteFacts was
	// selected by the render block: == rawFacts unless a render surfaced
	// strictly more contact facts.
	if siteHasAnyFact(siteFacts) {
		e.metrics.siteResolved()
	}
	r.mergeSite(siteFacts)
	// Accumulate the homepage's full candidate phone-number SET (Phase P2,
	// additive) into the resolver's SiteNumbers sidecar — the SAME page
	// (siteHTML) whose facts siteFacts just merged, so tags stay consistent.
	// homeRawPoisoned (computed once above, right after rawFacts) carries the
	// Poison-OR forward into the sidecar too — same fail-closed reasoning as
	// the Facts.Phone carry-forward in the render block above, and
	// fetchContactsHTML's rawPoisoned in enriche_contacts.go (contacts-page
	// mirror).
	homeSiteNumbers := homeSiteNumbersFor(rawSiteNumbers, fr.HTML, siteHTML, homeRawPoisoned)
	r.addSiteNumbers(homeSiteNumbers, item.City)

	// Contacts-subpage discovery: the homepage often links a dedicated /contacts
	// page that carries the canonical, richer contact set (email, hours, address)
	// the homepage omits. Discover it from the homepage links, fetch+render it,
	// and merge its facts at sourceOfficialSite AFTER this homepage merge so a
	// contacts-page value wins on conflict. siteFacts is the homepage's resolved
	// facts — the contacts page must beat it to be adopted. homeSiteNumbers lets
	// the gate widen to a homepage that is complete on hours/email/address but
	// carries no ANCHORED phone member at all (see homepageMissingRichField).
	e.resolveContactsPage(ctx, item, result, r, discoveryHTML, siteFacts, homeSiteNumbers)

	// Source-provided coordinates are owned by seedSourceCoords (applied at the
	// top of Enrich); the resolver's mergeSite never touches coords, so the
	// up-front seed survives — no re-seed needed now that Facts is no longer
	// reset by a wholesale assign.

	// OG image fallback.
	if result.Image == nil {
		result.Image = extract.ExtractOGImage(fr.HTML)
	}

	// Date fallback.
	if result.PublishedAt == nil {
		result.PublishedAt = extract.ExtractDate(strings.NewReader(fr.HTML), pageURL)
	}
}

// mergeOxBrowserResult replaces content with ox-browser result if it's longer.
func (e *Enricher) mergeOxBrowserResult(oxContent, oxTitle, oxAuthor, oxExcerpt string, result *Result) {
	oxRunes := len([]rune(oxContent))
	curRunes := len([]rune(result.Content))

	if oxRunes <= curRunes {
		e.logger.Debug("enriche: ox-browser shorter, keeping trafilatura",
			"ox_len", oxRunes, "traf_len", curRunes)
		return
	}

	e.logger.Debug("enriche: ox-browser longer, using readability",
		"ox_len", oxRunes, "traf_len", curRunes)

	result.Content = oxContent
	if e.maxContentLen > 0 {
		result.Content = truncateRunes(result.Content, e.maxContentLen)
	}

	// Fill metadata from ox-browser if trafilatura didn't provide it.
	if result.Metadata == nil {
		result.Metadata = &ContentMeta{}
	}
	if result.Metadata.Title == "" && oxTitle != "" {
		result.Metadata.Title = oxTitle
	}
	if result.Metadata.Author == "" && oxAuthor != "" {
		result.Metadata.Author = oxAuthor
	}
	if result.Metadata.Description == "" && oxExcerpt != "" {
		result.Metadata.Description = oxExcerpt
	}
}

// goqueryFallback tries goquery extraction when trafilatura output is thin
// or when markdown format lacks links (trafilatura strips hrefs from ContentNode).
func (e *Enricher) goqueryFallback(rawHTML string, result *Result) {
	contentRunes := len([]rune(result.Content))
	if !needsGoqueryFallback(e.format, result.Content, contentRunes) {
		return
	}

	gqContent, gqTitle := extract.ExtractGoquery(rawHTML, e.format)
	if !shouldUseGoquery(gqContent, contentRunes) {
		return
	}

	result.Content = gqContent
	if e.maxContentLen > 0 {
		result.Content = truncateRunes(result.Content, e.maxContentLen)
	}
	if result.Metadata != nil && result.Metadata.Title == "" && gqTitle != "" {
		result.Metadata.Title = gqTitle
	}
}

// browserFallback re-extracts content from browser-rendered HTML.
// Replaces result fields only if the new content is longer.
// Returns true if the rendered HTML produced better content.
func (e *Enricher) browserFallback(rendered, rawURL string, result *Result) bool {
	pageURL, _ := url.Parse(rawURL)
	tr, err := extract.ExtractText(
		strings.NewReader(rendered), pageURL, extract.WithFormat(e.format),
	)
	if err != nil || tr == nil {
		return false
	}

	newRunes := len([]rune(tr.Content))
	if newRunes <= len([]rune(result.Content)) {
		return false
	}

	result.Content = tr.Content
	if e.maxContentLen > 0 {
		result.Content = truncateRunes(result.Content, e.maxContentLen)
	}
	if tr.Title != "" && (result.Metadata == nil || result.Metadata.Title == "") {
		if result.Metadata == nil {
			result.Metadata = &ContentMeta{}
		}
		result.Metadata.Title = tr.Title
	}
	return true
}

const (
	minExtractChars    = 200
	maxGoqueryRatio    = 3
	markdownLinkMarker = "]("
)

// renderReason names why the headless render fired, for the OnBrowserRender
// metric. thin_content (no article text) is reported in preference to
// absent_contacts when both hold, since thin content is the stronger signal
// that the whole page is JS-gated.
//
// absent_contacts means ALL structured contact fields were absent from the raw
// HTML — phone (and not PhonePoisoned), address, AND hours AND email were every
// one nil (see hasContactFacts). It is NOT "some contact field missing": a
// single raw contact fact suppresses the render, because the contacts are then
// demonstrably not JS-gated.
func renderReason(thinContent bool) string {
	if thinContent {
		return "thin_content"
	}
	return "absent_contacts"
}

// contactFactCount counts how many of the three structured CONTACT fields
// (phone, address, hours) the extraction produced. Used to decide whether a
// rendered DOM is strictly richer than the raw HTML before adopting it as the
// facts source — a render that surfaces nothing new must not displace the raw
// extraction. PhonePoisoned counts as a present phone signal: the DNI verdict
// is itself information the resolver must keep, so a render that only re-shows
// the rotating proxy does not count as "more".
func contactFactCount(f extract.Facts) int {
	n := 0
	if f.Phone != nil || f.PhonePoisoned {
		n++
	}
	if f.Address != nil {
		n++
	}
	// A legal/registered address counts as a contact fact too — a /contacts page
	// that surfaces ONLY a legal seat (no venue address/phone/hours/email) must
	// still be adopted so its «Реквизиты» reach the consumer; otherwise the page
	// would tie a contactless homepage and never merge.
	if f.LegalAddress != nil {
		n++
	}
	if f.Hours != nil {
		n++
	}
	if f.Email != nil {
		n++
	}
	return n
}

// homeSiteNumbersFor returns the SiteNumbers set for siteHTML, reusing the
// pre-hoisted rawSiteNumbers (collected once on the raw HTML) when a render was
// NOT adopted (siteHTML is still the raw HTML — the no-render / render-skip
// case), so the raw page is parsed for numbers exactly once. When a render WAS
// adopted (siteHTML is the rendered DOM), it re-collects from that DOM with the
// raw poison verdict carried forward — identical to the pre-hoist unconditional
// collection at the SiteNumbers accumulator call site.
func homeSiteNumbersFor(rawSiteNumbers []extract.PhoneNumberFact, rawHTML, siteHTML string, poisoned bool) []extract.PhoneNumberFact {
	if siteHTML == rawHTML {
		return rawSiteNumbers
	}
	return extract.CollectSiteNumbersHTML(siteHTML, poisoned)
}

// noteHomepageRenderSkip records the trust-gated homepage render-skip — the
// RenderSkipped provenance flag on result plus the OnBrowserRenderSkipped
// per-leg counter — when a render the OLD escalation gate would have fired
// (absent contacts on a non-thin page) was avoided because the raw fetch
// already carried a trustworthy anchored number (rawSufficient with no Facts
// contact). A thin-content render is never a trust-skip (the content still
// needs the render); a page that already had a Facts contact never rendered
// under the old gate either — neither is flagged. Isolating this branching here
// keeps it out of the F-graded fetchAndExtract orchestrator (ADR-5).
func (e *Enricher) noteHomepageRenderSkip(result *Result, canRender, thinContent, hasFacts, rawSufficient bool) {
	if !canRender || thinContent || hasFacts || !rawSufficient {
		return
	}
	result.RenderSkipped = true
	e.metrics.browserRenderSkipped(renderSkipLegHomepage, renderSkipReasonRawSufficient)
}

// needsGoqueryFallback checks if goquery extraction should be attempted.
func needsGoqueryFallback(format extract.Format, content string, contentRunes int) bool {
	if contentRunes < minExtractChars {
		return true
	}
	return format == extract.FormatMarkdown &&
		!strings.Contains(content, markdownLinkMarker)
}

// shouldUseGoquery checks if goquery result is better than current content.
func shouldUseGoquery(gqContent string, contentRunes int) bool {
	gqRunes := len([]rune(gqContent))
	if contentRunes < minExtractChars {
		return gqRunes > contentRunes
	}
	hasLinks := strings.Contains(gqContent, markdownLinkMarker)
	notTooNoisy := contentRunes == 0 || gqRunes/contentRunes <= maxGoqueryRatio
	return hasLinks && gqRunes >= contentRunes && notTooNoisy
}

// fetchWithRetry fetches a URL with one retry on transient errors.
// Returns nil if all attempts fail.
func (e *Enricher) fetchWithRetry(ctx context.Context, rawURL string) *fetch.FetchResult {
	fr, err := e.fetcher.Fetch(ctx, rawURL)
	if err != nil {
		e.logger.DebugContext(ctx, "enriche: fetch failed", "url", rawURL, "err", err)
		e.metrics.fetchError()
		return nil
	}

	if !fr.IsTransient() {
		return fr
	}

	// One retry with jitter.
	e.logger.DebugContext(ctx, "enriche: retrying transient", "url", rawURL, "code", fr.StatusCode)
	jitter := time.Duration(100+rand.IntN(200)) * time.Millisecond //nolint:mnd,gosec
	timer := time.NewTimer(jitter)
	select {
	case <-ctx.Done():
		timer.Stop()
		e.metrics.fetchError()
		return nil
	case <-timer.C:
	}

	fr, err = e.fetcher.Fetch(ctx, rawURL)
	if err != nil {
		e.logger.DebugContext(ctx, "enriche: retry failed", "url", rawURL, "err", err)
		e.metrics.fetchError()
		return nil
	}
	return fr
}
