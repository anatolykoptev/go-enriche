// Package enriche provides web content enrichment: fetch pages, extract text,
// parse structured data, search for context.
package enriche

import (
	"context"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/anatolykoptev/go-enriche/cache"
	"github.com/anatolykoptev/go-enriche/extract"
	"github.com/anatolykoptev/go-enriche/fetch"
	"github.com/anatolykoptev/go-enriche/search"
)

const (
	defaultConcurrency = 5
	defaultCacheTTL    = 24 * time.Hour
)

// Enricher orchestrates web content enrichment.
type Enricher struct {
	fetcher       *fetch.Fetcher
	cache         cache.Cache
	search        search.Provider
	concurrency   int
	cacheTTL      time.Duration
	maxContentLen int
	logger        *slog.Logger
	metrics       *Metrics
}

// discardHandler silently discards all log records.
type discardHandler struct{}

func (discardHandler) Enabled(context.Context, slog.Level) bool  { return false }
func (discardHandler) Handle(context.Context, slog.Record) error { return nil }
func (d discardHandler) WithAttrs([]slog.Attr) slog.Handler      { return d }
func (d discardHandler) WithGroup(string) slog.Handler            { return d }

// New creates an Enricher with the given options.
func New(opts ...Option) *Enricher {
	e := &Enricher{
		fetcher:     fetch.NewFetcher(),
		concurrency: defaultConcurrency,
		cacheTTL:    defaultCacheTTL,
		logger:      slog.New(discardHandler{}),
	}
	for _, o := range opts {
		o(e)
	}
	return e
}

// Enrich enriches a single item: fetch, extract, search, cache.
// Returns a partial result on failures (graceful degradation).
func (e *Enricher) Enrich(ctx context.Context, item Item) (*Result, error) {
	result := &Result{
		Name: item.Name,
		URL:  item.URL,
	}

	// Cache check.
	if e.cache != nil {
		key := cacheKey(item)
		if e.cache.Get(ctx, key, result) {
			e.logger.DebugContext(ctx, "enriche: cache hit", "name", item.Name, "key", key)
			e.metrics.cacheHit()
			return result, nil
		}
		e.logger.DebugContext(ctx, "enriche: cache miss", "name", item.Name, "key", key)
		e.metrics.cacheMiss()
	}

	// Fetch + extract.
	if item.URL != "" {
		e.fetchAndExtract(ctx, item, result)
	}

	// Search.
	if e.search != nil {
		e.doSearch(ctx, item, result)
	}

	// Cache store.
	if e.cache != nil {
		e.cache.Set(ctx, cacheKey(item), result, e.cacheTTL)
	}

	e.logger.DebugContext(ctx, "enriche: done", "name", item.Name, "status", result.Status)

	return result, nil
}

// EnrichBatch enriches multiple items concurrently with bounded concurrency.
// Respects context cancellation — unstarted items are skipped.
func (e *Enricher) EnrichBatch(ctx context.Context, items []Item) []*Result {
	results := make([]*Result, len(items))
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(e.concurrency)

	for i, item := range items {
		g.Go(func() error {
			r, _ := e.Enrich(gctx, item)
			results[i] = r
			return nil
		})
	}

	_ = g.Wait()
	return results
}

func (e *Enricher) fetchAndExtract(ctx context.Context, item Item, result *Result) {
	fr, err := e.fetcher.Fetch(ctx, item.URL)
	if err != nil {
		e.logger.DebugContext(ctx, "enriche: fetch failed", "url", item.URL, "err", err)
		e.metrics.fetchError()
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

func (e *Enricher) doSearch(ctx context.Context, item Item, result *Result) {
	query, timeRange := search.BuildQuery(int(item.Mode), item.Name, item.City)
	sr, err := e.search.Search(ctx, query, timeRange)
	if err != nil || sr == nil {
		if err != nil {
			e.logger.DebugContext(ctx, "enriche: search failed", "name", item.Name, "err", err)
			e.metrics.searchError()
		}
		return
	}

	e.logger.DebugContext(ctx, "enriche: search done", "name", item.Name, "sources", len(sr.Sources))
	result.SearchContext = sr.Context
	result.SearchSources = sr.Sources

	// Extract facts from search snippets (fills nil fields only).
	extract.ExtractSnippetFacts(sr.Context, &result.Facts)
}

func cacheKey(item Item) string {
	if item.URL != "" {
		return "enriche:" + item.URL
	}
	return "enriche:search:" + item.Name
}
