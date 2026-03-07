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
// merging everything into the result (fill-nil-only for facts, concatenate content).
func (e *Enricher) fetchSearchSources(ctx context.Context, item Item, result *Result) {
	sources := e.searchSourcesToFetch(result)
	if len(sources) == 0 {
		return
	}

	e.logger.DebugContext(ctx, "enriche: fetching search sources",
		"name", item.Name, "count", len(sources))

	contents, fetchedAny := e.fetchAndExtractSources(ctx, sources, result)

	if len(contents) > 0 {
		joined := strings.Join(contents, "\n\n---\n\n")
		if e.maxContentLen > 0 {
			joined = truncateRunes(joined, e.maxContentLen)
		}
		result.Content = joined
	}

	if fetchedAny {
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
	ctx context.Context, sources []string, result *Result,
) ([]string, bool) {
	results := make([]sourceResult, len(sources))

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(len(sources)) // all in parallel

	for i, srcURL := range sources {
		g.Go(func() error {
			results[i] = e.fetchOneSource(gctx, srcURL)
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
		mergeFacts(sr.facts, &result.Facts)
		if result.Image == nil && sr.image != "" {
			img := sr.image
			result.Image = &img
		}
	}

	return contents, fetchedAny
}

// fetchOneSource fetches a single URL and extracts content + facts.
func (e *Enricher) fetchOneSource(ctx context.Context, srcURL string) sourceResult {
	fr := e.fetchWithRetry(ctx, srcURL)
	if fr == nil || fr.HTML == "" {
		return sourceResult{}
	}

	var sr sourceResult

	pageURL, _ := url.Parse(srcURL)
	tr, err := extract.ExtractText(
		strings.NewReader(fr.HTML), pageURL, extract.WithFormat(e.format),
	)
	if err == nil && tr != nil {
		sr.content = tr.Content
		sr.image = tr.Image
	}

	sr.facts = extract.ExtractFacts(fr.HTML, srcURL)
	return sr
}

// mergeFacts copies non-nil fields from src into dst (fill-nil-only).
func mergeFacts(src extract.Facts, dst *extract.Facts) {
	mergeFactPtr(&dst.PlaceName, src.PlaceName)
	mergeFactPtr(&dst.PlaceType, src.PlaceType)
	mergeFactPtr(&dst.Address, src.Address)
	mergeFactPtr(&dst.Phone, src.Phone)
	mergeFactPtr(&dst.Price, src.Price)
	mergeFactPtr(&dst.Website, src.Website)
	mergeFactPtr(&dst.Hours, src.Hours)
	mergeFactPtr(&dst.EventDate, src.EventDate)
	if src.Latitude != nil && dst.Latitude == nil {
		dst.Latitude = src.Latitude
		dst.Longitude = src.Longitude
	}
}

// mergeFactPtr sets *dst to src if *dst is nil and src is non-nil.
func mergeFactPtr(dst **string, src *string) {
	if *dst == nil && src != nil {
		*dst = src
	}
}
