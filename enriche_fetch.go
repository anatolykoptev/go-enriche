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

func (e *Enricher) fetchAndExtract(ctx context.Context, item Item, result *Result) {
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
			ox, err := e.oxBrowser.Extract(ctx, item.URL)
			if err != nil {
				e.logger.DebugContext(ctx, "enriche: ox-browser failed", "url", item.URL, "err", err)
				oxCh <- nil
				return
			}
			oxCh <- &oxResult{content: ox.Content, title: ox.Title, author: ox.Author, excerpt: ox.Excerpt}
		}()
	}

	fr := e.fetchWithRetry(ctx, item.URL)
	if fr == nil {
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

	// Browser fallback for JS-heavy pages with thin content.
	html := fr.HTML
	if e.browserFetch != nil && len([]rune(result.Content)) < minExtractChars {
		if rendered, err := e.browserFetch(ctx, item.URL); err == nil && rendered != "" {
			if e.browserFallback(rendered, item.URL, result) {
				html = rendered
			}
		}
	}

	// Merge ox-browser result: pick longer content.
	if oxCh != nil {
		if ox := <-oxCh; ox != nil {
			e.mergeOxBrowserResult(ox.content, ox.title, ox.author, ox.excerpt, result)
		}
	}

	// Extract structured facts.
	result.Facts = extract.ExtractFacts(html, item.URL)

	// Geocode: resolve address to coordinates (places only).
	e.geocodeIfNeeded(ctx, item, result)

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

// geocodeIfNeeded resolves address to lat/lon for place items.
func (e *Enricher) geocodeIfNeeded(ctx context.Context, item Item, result *Result) {
	if e.geocoder == nil || item.Mode != ModePlaces {
		return
	}
	if result.Facts.Address == nil || result.Facts.Latitude != nil {
		return
	}

	city := item.City
	if city == "" {
		city = "Санкт-Петербург"
	}

	lat, lon, ok := e.geocoder.Geocode(ctx, *result.Facts.Address, city)
	if ok {
		result.Facts.Latitude = &lat
		result.Facts.Longitude = &lon
	}
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
