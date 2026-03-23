// Package enriche provides web content enrichment: fetch pages, extract text,
// parse structured data, search for context.
package enriche

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/anatolykoptev/go-enriche/cache"
	"github.com/anatolykoptev/go-enriche/extract"
	"github.com/anatolykoptev/go-enriche/fetch"
	"github.com/anatolykoptev/go-enriche/maps"
	"github.com/anatolykoptev/go-enriche/search"
)

const (
	defaultConcurrency = 5
	defaultCacheTTL    = 24 * time.Hour
)

// Enricher orchestrates web content enrichment.
type Enricher struct {
	fetcher          *fetch.Fetcher
	cache            cache.Cache
	search           search.Provider
	mapsChecker      maps.Checker
	geocoder         *maps.Geocoder
	format           extract.Format
	concurrency      int
	cacheTTL         time.Duration
	maxContentLen    int
	browserFetch     func(ctx context.Context, url string) (string, error)
	oxBrowser        *fetch.OxBrowserClient
	searchFetchLimit int
	logger           *slog.Logger
	metrics          *Metrics
}

// discardHandler silently discards all log records.
type discardHandler struct{}

func (discardHandler) Enabled(context.Context, slog.Level) bool  { return false }
func (discardHandler) Handle(context.Context, slog.Record) error { return nil }
func (d discardHandler) WithAttrs([]slog.Attr) slog.Handler      { return d }
func (d discardHandler) WithGroup(string) slog.Handler           { return d }

// New creates an Enricher with the given options.
func New(opts ...Option) *Enricher {
	e := &Enricher{
		fetcher:     fetch.NewFetcher(),
		format:      extract.FormatText,
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

	// Maps status check (places only) — short-circuit if permanently closed.
	if e.checkMapsStatus(ctx, item, result) {
		if e.search != nil {
			e.doSearch(ctx, item, result)
		}
		if e.cache != nil {
			e.cache.Set(ctx, cacheKey(item), result, e.cacheTTL)
		}
		return result, nil
	}

	// Fetch + extract.
	if item.URL != "" {
		e.fetchAndExtract(ctx, item, result)
	}

	// Search.
	if e.search != nil {
		e.doSearch(ctx, item, result)
	}

	// When no primary URL, fetch top search source URLs for content + facts.
	if item.URL == "" && result.Content == "" {
		e.fetchSearchSources(ctx, item, result)
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

// checkMapsStatus queries the maps checker for place closure status.
// Only runs for ModePlaces. Returns true if the place is permanently closed.
// When OrgData is available, merges business data into facts (fill-nil-only).
func (e *Enricher) checkMapsStatus(ctx context.Context, item Item, result *Result) bool {
	if e.mapsChecker == nil || item.Mode != ModePlaces {
		return false
	}
	cr, err := e.mapsChecker.Check(ctx, item.Name, item.City)
	if err != nil {
		e.logger.DebugContext(ctx, "enriche: maps check failed", "name", item.Name, "err", err)
		e.metrics.mapsCheckError()
		return false
	}

	// Merge business data from maps (fill-nil-only).
	if cr.OrgData != nil {
		mergeOrgDataToFacts(cr.OrgData, &result.Facts)
		e.logger.DebugContext(ctx, "enriche: merged maps org data",
			"name", item.Name, "org_name", cr.OrgData.Name)
	}

	if cr.IsClosed() {
		result.Status = fetch.StatusClosed
		e.logger.InfoContext(ctx, "enriche: place permanently closed on maps",
			"name", item.Name, "map_url", cr.MapURL)
		return true
	}
	return false
}

// mergeOrgDataToFacts copies maps business data into facts (fill-nil-only).
func mergeOrgDataToFacts(od *maps.OrgData, facts *Facts) {
	setFactIfNil(&facts.PlaceName, od.Name)
	setFactIfNil(&facts.Address, od.Address)
	setFactIfNil(&facts.Phone, od.Phone)
	setFactIfNil(&facts.Website, od.Website)
	setFactIfNil(&facts.Hours, od.Hours)
	if od.Latitude != 0 && facts.Latitude == nil {
		facts.Latitude = &od.Latitude
		facts.Longitude = &od.Longitude
	}
}

// setFactIfNil sets *dst to &src if *dst is nil and src is non-empty.
func setFactIfNil(dst **string, src string) {
	if *dst == nil && src != "" {
		*dst = &src
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

// Search exposes the configured search provider for direct queries.
func (e *Enricher) Search(ctx context.Context, query, timeRange string) (*search.SearchResult, error) {
	if e.search == nil {
		return nil, errors.New("search provider not configured")
	}
	return e.search.Search(ctx, query, timeRange)
}

func cacheKey(item Item) string {
	if item.URL != "" {
		return "enriche:" + item.URL
	}
	return "enriche:search:" + item.Name
}
