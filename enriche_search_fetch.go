package enriche

import (
	"context"
	"net/url"
	"strings"

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

// fetchAndExtractSources fetches each URL, extracts text and facts.
func (e *Enricher) fetchAndExtractSources(
	ctx context.Context, sources []string, result *Result,
) (contents []string, fetchedAny bool) {
	for _, srcURL := range sources {
		if ctx.Err() != nil {
			break
		}

		fr := e.fetchWithRetry(ctx, srcURL)
		if fr == nil || fr.HTML == "" {
			continue
		}
		fetchedAny = true

		contents = e.extractSourceContent(srcURL, fr.HTML, contents, result)
		mergeFacts(extract.ExtractFacts(fr.HTML, srcURL), &result.Facts)
	}
	return contents, fetchedAny
}

// extractSourceContent extracts text from a fetched page and appends to contents.
func (e *Enricher) extractSourceContent(
	srcURL, html string, contents []string, result *Result,
) []string {
	pageURL, _ := url.Parse(srcURL)
	tr, err := extract.ExtractText(
		strings.NewReader(html), pageURL, extract.WithFormat(e.format),
	)
	if err != nil || tr == nil || tr.Content == "" {
		return contents
	}

	if result.Image == nil && tr.Image != "" {
		result.Image = &tr.Image
	}
	return append(contents, tr.Content)
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
