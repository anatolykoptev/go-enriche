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
//
// Source-priority ordering (the official-site-first invariant): all contact
// facts flow through one resolver whose precedence is
// official_site > aggregator > maps > search. The maps card fills facts as a
// LOWER-priority fallback; the official site is ALWAYS fetched (even when maps
// reports the venue closed) and its values OVERRIDE the maps card on conflict.
// A reachable, active site also refutes a maps-only "closed" status. This
// collapses the former two independent fact writers (the maps merge and the
// wholesale site-extraction assign) into a single authority — see resolve.go.
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

	// Seed source-provided coordinates unconditionally so every path
	// (events, places with no URL, places with transient 2GIS errors) preserves them.
	seedSourceCoords(item, &result.Facts)

	// One resolver owns every fact write for this call. The maps merge and the
	// site extraction both feed it; it enforces official_site > maps precedence.
	r := &resolver{facts: &result.Facts, prov: &factProvenance{}, m: e.metrics}

	// Operator-verified seed FIRST: pin any hand-verified field at the top
	// source priority so no enrich-derived value (maps card, rotating DNI tel:,
	// search snippet, even the official site) can overwrite it. The content
	// layer re-supplies these on every re-enrich (persistence-survival path).
	r.seedOperatorValues(item.Seed)

	// Maps status check (places only). Fills facts at sourceMaps (lower
	// priority) and returns a CANDIDATE closed-status — it no longer
	// short-circuits before the official site is consulted. An empty status
	// means "not reported closed".
	mapsClosedStatus := e.checkMapsStatus(ctx, item, result, r)

	// Fetch + extract the official site. ALWAYS run when a URL is present,
	// including the maps-closed case: the site's own tel:/contacts are the
	// authority, and a live site refutes a stale maps "closed". Merged at
	// sourceOfficialSite (overrides maps on conflict).
	siteFetched := false
	if item.URL != "" {
		e.fetchAndExtract(ctx, item, result, r)
		siteFetched = true
	}

	// Reconcile closed-status against the official site (false-closed class).
	// closedStands is true when maps reported the venue closed and the official
	// site did NOT refute it (no/unreachable/down site) — the closed verdict is
	// final and a lower-authority source must not overturn it.
	closedStands := e.reconcileClosedStatus(ctx, item, result, mapsClosedStatus, siteFetched)

	// Search.
	if e.search != nil {
		e.doSearch(ctx, item, result, r)
	}

	// When no primary URL, fetch top search source URLs for content + facts.
	// A search-discovered third-party page is NOT authority to refute a maps
	// closed-status, so its Status upgrade is suppressed when closedStands —
	// only a reachable, active official site may resurrect a closed venue.
	if item.URL == "" && result.Content == "" {
		e.fetchSearchSources(ctx, item, result, r, closedStands)
	}

	// Export the resolved per-field provenance onto the public Result so the
	// content layer can persist {source, confidence} and protect operator
	// values on re-enrich (Phase 3 ONE_WAY contract).
	result.Provenance = r.snapshot()

	// Phone-source telemetry: report the winning phone's provenance once.
	if item.Mode == ModePlaces {
		e.metrics.phoneSource(r.phoneSource())
	}

	// Cache store.
	if e.cache != nil {
		e.cache.Set(ctx, cacheKey(item), result, e.cacheTTL)
	}

	e.logger.DebugContext(ctx, "enriche: done", "name", item.Name, "status", result.Status)

	return result, nil
}

// reconcileClosedStatus decides the final Status when maps flagged the venue
// closed. mapsClosedStatus is the candidate verdict (StatusClosed /
// StatusTemporaryClosed, or "" when maps did not flag closed). The official
// site, if reachable and active, refutes a maps-only closed verdict (the
// false-closed class: a wrong Yandex/2GIS card marks an operating venue
// closed). When the site is NOT active (or no site exists), the maps
// closed-status stands.
//
// Returns true when the closed verdict STANDS (final) — the caller must then
// suppress any lower-authority Status upgrade (e.g. the search fallback), since
// only a reachable, active official site may resurrect a closed venue.
func (e *Enricher) reconcileClosedStatus(ctx context.Context, item Item, result *Result, mapsClosedStatus fetch.PageStatus, siteFetched bool) bool {
	if mapsClosedStatus == "" {
		return false
	}
	if siteFetched && siteRefutesClosed(result.Status) {
		// result.Status already holds the site's StatusActive from fetchAndExtract.
		e.logger.InfoContext(ctx, "enriche: live official site refutes maps closed-status",
			"name", item.Name, "status", result.Status)
		return false
	}
	// Site absent / unreachable / down — keep the maps closed verdict.
	result.Status = mapsClosedStatus
	e.logger.InfoContext(ctx, "enriche: maps closed-status stands (site not active)",
		"name", item.Name, "status", result.Status)
	return true
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
// Only runs for ModePlaces. Returns the candidate closed-status
// (StatusClosed / StatusTemporaryClosed) when the place is reported closed on
// maps, or "" otherwise — but DOES NOT short-circuit: the official site is
// still fetched afterward (resolver enforces site-over-maps, and a live site
// can refute the closed verdict). When OrgData is available, merges business
// data into facts at sourceMaps via the resolver.
func (e *Enricher) checkMapsStatus(ctx context.Context, item Item, _ *Result, r *resolver) fetch.PageStatus {
	if e.mapsChecker == nil || item.Mode != ModePlaces {
		return ""
	}
	cr, err := e.mapsChecker.Check(ctx, item.Name, item.City, item.Address)
	if err != nil {
		e.logger.DebugContext(ctx, "enriche: maps check failed", "name", item.Name, "err", err)
		e.metrics.mapsCheckError()
		return ""
	}

	// Merge business data from maps at sourceMaps (lower priority — the official
	// site overrides on conflict).
	if cr.OrgData != nil {
		r.mergeOrg(cr.OrgData)
		e.logger.DebugContext(ctx, "enriche: merged maps org data",
			"name", item.Name, "org_name", cr.OrgData.Name)
	}

	if cr.IsClosed() {
		e.logger.InfoContext(ctx, "enriche: place reported permanently closed on maps",
			"name", item.Name, "map_url", cr.MapURL)
		return fetch.StatusClosed
	}
	if cr.IsTemporaryClosed() {
		e.logger.InfoContext(ctx, "enriche: place reported temporarily closed on maps",
			"name", item.Name, "map_url", cr.MapURL)
		return fetch.StatusTemporaryClosed
	}
	return ""
}

// seedSourceCoords writes item.Latitude/Longitude into facts when the item
// carries a pair of source-authoritative coordinates (both non-nil).
// Called unconditionally at the top of Enrich so all paths (events, no-URL
// places, transient maps errors) preserve source coords.
// Must also be re-called in fetchAndExtract after extract.ExtractFacts resets
// Facts to a zero-value struct — see enriche_fetch.go.
func seedSourceCoords(item Item, facts *Facts) {
	if item.Latitude == nil || item.Longitude == nil {
		return // absent or incomplete pair — treat as not provided
	}
	facts.Latitude = item.Latitude
	facts.Longitude = item.Longitude
}

func (e *Enricher) doSearch(ctx context.Context, item Item, result *Result, r *resolver) {
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

	// Extract facts from search snippets at sourceSearch (lowest priority —
	// fills nil fields only, never overrides site/maps).
	r.mergeSnippet(sr.Context)
}

// Search exposes the configured search provider for direct queries.
func (e *Enricher) Search(ctx context.Context, query, timeRange string) (*search.SearchResult, error) {
	if e.search == nil {
		return nil, errors.New("search provider not configured")
	}
	return e.search.Search(ctx, query, timeRange)
}

// cacheSchemaVersion is bumped whenever the cached Result shape changes in a
// way that must NOT be deserialized from an older blob. v2 = Phase 3: Result
// gained the Provenance sidecar; an old (v1) blob has no provenance, so we move
// to a fresh key namespace rather than silently reading provenance-less data.
const cacheSchemaVersion = "v2"

func cacheKey(item Item) string {
	if item.URL != "" {
		return "enriche:" + cacheSchemaVersion + ":" + item.URL
	}
	return "enriche:search:" + cacheSchemaVersion + ":" + item.Name
}
