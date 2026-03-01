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
	textResult, textErr := extract.ExtractText(strings.NewReader(fr.HTML), pageURL)
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

	// Extract structured facts.
	result.Facts = extract.ExtractFacts(fr.HTML, item.URL)

	// OG image fallback.
	if result.Image == nil {
		result.Image = extract.ExtractOGImage(fr.HTML)
	}

	// Date fallback.
	if result.PublishedAt == nil {
		result.PublishedAt = extract.ExtractDate(strings.NewReader(fr.HTML), pageURL)
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

	// One retry for transient errors (502, 503, 504, 429, connection failure).
	e.logger.DebugContext(ctx, "enriche: transient error, retrying", "url", rawURL, "code", fr.StatusCode)
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
