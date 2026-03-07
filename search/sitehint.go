package search

import (
	"context"
	"strings"
)

// SiteHint wraps a Provider and appends "site:x OR site:y" restrictions
// to the query. Useful for prioritizing results from trusted regional sources.
// The inner provider (typically DDG or OxBrowser) receives the site-restricted query.
type SiteHint struct {
	inner      Provider
	restriction string
}

// NewSiteHint creates a provider that appends site: restrictions to queries.
// sites is a list of domains (e.g. "fontanka.ru", "sobaka.ru").
func NewSiteHint(inner Provider, sites []string) *SiteHint {
	var parts []string
	for _, s := range sites {
		parts = append(parts, "site:"+s)
	}
	return &SiteHint{
		inner:      inner,
		restriction: strings.Join(parts, " OR "),
	}
}

// Search appends site restrictions to the query and delegates to the inner provider.
func (s *SiteHint) Search(ctx context.Context, query string, timeRange string) (*SearchResult, error) {
	if s.restriction != "" {
		query = query + " " + s.restriction
	}
	return s.inner.Search(ctx, query, timeRange)
}
