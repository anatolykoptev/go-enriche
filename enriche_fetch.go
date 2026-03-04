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
	fr := e.fetchWithRetry(ctx, item.URL)
	if fr == nil {
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

	// Extract text + metadata.
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

	// Extract structured facts.
	result.Facts = extract.ExtractFacts(fr.HTML, item.URL)

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

// fetchWithRetry fetches a URL with retries on transient/proxy errors.
// Returns nil if all attempts fail.
func (e *Enricher) fetchWithRetry(ctx context.Context, rawURL string) *fetch.FetchResult {
	fr, err := e.fetcher.Fetch(ctx, rawURL)
	if err != nil {
		e.logger.DebugContext(ctx, "enriche: fetch failed", "url", rawURL, "err", err)
		e.metrics.fetchError()
		return nil
	}

	if !e.shouldRetryFetch(fr) {
		return fr
	}

	// First retry.
	fr = e.retryFetch(ctx, rawURL, fr)
	if fr == nil {
		return nil
	}

	// Second retry (only with retryOn403 — proxy pool rotation benefits from extra attempt).
	if e.retryOn403 && fr.IsProxyRetryable() {
		fr = e.retryFetch(ctx, rawURL, fr)
		if fr == nil {
			return nil
		}
	}

	return fr
}

// shouldRetryFetch checks if a fetch result warrants a retry.
func (e *Enricher) shouldRetryFetch(fr *fetch.FetchResult) bool {
	if e.retryOn403 {
		return fr.IsProxyRetryable()
	}
	return fr.IsTransient()
}

// retryFetch performs a single retry with jitter. Returns nil on context cancel or error.
func (e *Enricher) retryFetch(ctx context.Context, rawURL string, prev *fetch.FetchResult) *fetch.FetchResult {
	e.logger.DebugContext(ctx, "enriche: retrying fetch", "url", rawURL, "code", prev.StatusCode)
	jitter := time.Duration(100+rand.IntN(200)) * time.Millisecond //nolint:mnd,gosec
	timer := time.NewTimer(jitter)
	select {
	case <-ctx.Done():
		timer.Stop()
		e.metrics.fetchError()
		return nil
	case <-timer.C:
	}

	fr, err := e.fetcher.Fetch(ctx, rawURL)
	if err != nil {
		e.logger.DebugContext(ctx, "enriche: retry failed", "url", rawURL, "err", err)
		e.metrics.fetchError()
		return nil
	}
	return fr
}
