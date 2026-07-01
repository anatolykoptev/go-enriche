package enriche

import (
	"context"
	"net/url"
	"strings"

	"golang.org/x/sync/errgroup"

	"github.com/anatolykoptev/go-enriche/extract"
)

const defaultSearchFetchLimit = 5

// fetchSearchSources fetches top N URLs from search results when the item has
// no primary URL. Extracts text content and structured facts from each page,
// merging everything into the result (resolver fill-only for facts at
// sourceSearch, concatenate content).
//
// closedStands suppresses the StatusActive upgrade: a search-discovered
// third-party page is NOT authority to refute a maps closed-status (only a
// reachable, active official site may). When closedStands is true the closed
// verdict is preserved — content/facts may still be collected, but the venue is
// not resurrected to active by a stale aggregator listing.
func (e *Enricher) fetchSearchSources(ctx context.Context, item Item, result *Result, r *resolver, closedStands bool) {
	sources := e.searchSourcesToFetch(result)
	if len(sources) == 0 {
		return
	}

	e.logger.DebugContext(ctx, "enriche: fetching search sources",
		"name", item.Name, "count", len(sources))

	contents, fetchedAny := e.fetchAndExtractSources(ctx, sources, item.City, result, r)

	if len(contents) > 0 {
		joined := strings.Join(contents, "\n\n---\n\n")
		if e.maxContentLen > 0 {
			joined = truncateRunes(joined, e.maxContentLen)
		}
		result.Content = joined
	}

	if fetchedAny && !closedStands {
		result.Status = StatusActive
	}
}

// searchSourcesToFetch returns up to searchFetchLimit URLs from search results.
func (e *Enricher) searchSourcesToFetch(result *Result) []string {
	if len(result.SearchSources) == 0 {
		return nil
	}
	limit := e.searchFetchLimit
	if limit <= 0 {
		limit = defaultSearchFetchLimit
	}
	sources := result.SearchSources
	if len(sources) > limit {
		sources = sources[:limit]
	}
	return sources
}

// sourceResult holds per-URL extraction output for ordered assembly.
type sourceResult struct {
	content string
	facts   extract.Facts
	image   string
}

// fetchAndExtractSources fetches URLs in parallel, extracts text and facts.
// Results are assembled in original order to keep content deterministic.
func (e *Enricher) fetchAndExtractSources(
	ctx context.Context, sources []string, city string, result *Result, r *resolver,
) ([]string, bool) {
	results := make([]sourceResult, len(sources))

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(len(sources)) // all in parallel

	for i, srcURL := range sources {
		g.Go(func() error {
			results[i] = e.fetchOneSource(gctx, srcURL, city)
			return nil
		})
	}
	_ = g.Wait()

	// Assemble in order: merge facts, collect content, pick first image.
	var contents []string
	fetchedAny := false

	for _, sr := range results {
		if sr.content == "" && sr.facts == (extract.Facts{}) {
			continue
		}
		fetchedAny = true

		if sr.content != "" {
			contents = append(contents, sr.content)
		}
		// Search-discovered pages are NOT the venue's own site — fold their facts
		// in at sourceSearch (lowest priority): fill-only, never override a
		// maps/official-site value.
		r.mergeSearchFacts(sr.facts)
		if result.Image == nil && sr.image != "" {
			img := sr.image
			result.Image = &img
		}
	}

	return contents, fetchedAny
}

// fetchOneSource fetches a single URL and extracts content + facts.
// Uses ox-browser readability as fallback when go-stealth fetch fails or yields thin content.
func (e *Enricher) fetchOneSource(ctx context.Context, srcURL, city string) sourceResult {
	// Start ox-browser in parallel if available.
	type oxOut struct {
		content string
	}
	var oxCh chan *oxOut
	if e.oxBrowser != nil {
		oxCh = make(chan *oxOut, 1)
		go func() {
			// srcURL comes from search results — ox-browser fetches it server-side
			// in a different process, so fetch.Fetcher's dial-time guard cannot
			// protect this hop; gate it explicitly (SSRF guard, see checkTarget).
			if err := e.checkTarget(ctx, srcURL); err != nil {
				e.logger.DebugContext(ctx, "enriche: ox-browser search-source target blocked", "url", srcURL, "err", err)
				oxCh <- nil
				return
			}
			ox, err := e.oxBrowser.Extract(ctx, srcURL)
			if err != nil || ox == nil {
				oxCh <- nil
				return
			}
			oxCh <- &oxOut{content: ox.Content}
		}()
	}

	fr := e.fetchWithRetry(ctx, srcURL)

	var sr sourceResult

	if fr != nil && fr.HTML != "" {
		pageURL, _ := url.Parse(srcURL)
		tr, err := extract.ExtractText(
			strings.NewReader(fr.HTML), pageURL, extract.WithFormat(e.format),
		)
		if err == nil && tr != nil {
			sr.content = tr.Content
			sr.image = tr.Image
		}
		sr.facts = extract.ExtractFactsForCity(fr.HTML, srcURL, city)
	}

	// Merge ox-browser: use if longer.
	if oxCh != nil {
		if ox := <-oxCh; ox != nil && len([]rune(ox.content)) > len([]rune(sr.content)) {
			sr.content = ox.content
		}
	}

	return sr
}
