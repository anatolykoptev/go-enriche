package enriche

// Metrics provides callback hooks for observability.
// Any nil field is safely ignored (no-op).
type Metrics struct {
	OnCacheHit       func()
	OnCacheMiss      func()
	OnFetchError     func()
	OnSearchError    func()
	OnMapsCheckError func()

	// OnPhoneSource fires once per Enrich(ModePlaces) call that resolves a
	// phone, with the winning source ("official_site" | "aggregator" | "maps" |
	// "search"). Lets the consumer track enrich_phone_source_total{source}.
	OnPhoneSource func(source string)
	// OnSiteResolved fires once per Enrich call where the official site was
	// fetched and yielded at least one fact (enrich_site_resolved_total).
	OnSiteResolved func()
	// OnConflict fires when the source-priority resolver overrode a present,
	// differing lower-source value with a higher-source one (e.g. the official
	// site overriding a maps phone). field is the fact name
	// (enrich_conflict_total{field}).
	OnConflict func(field string)
}

func (m *Metrics) cacheHit() {
	if m != nil && m.OnCacheHit != nil {
		m.OnCacheHit()
	}
}

func (m *Metrics) cacheMiss() {
	if m != nil && m.OnCacheMiss != nil {
		m.OnCacheMiss()
	}
}

func (m *Metrics) fetchError() {
	if m != nil && m.OnFetchError != nil {
		m.OnFetchError()
	}
}

func (m *Metrics) searchError() {
	if m != nil && m.OnSearchError != nil {
		m.OnSearchError()
	}
}

func (m *Metrics) mapsCheckError() {
	if m != nil && m.OnMapsCheckError != nil {
		m.OnMapsCheckError()
	}
}

func (m *Metrics) phoneSource(source string) {
	if m != nil && m.OnPhoneSource != nil && source != "" {
		m.OnPhoneSource(source)
	}
}

func (m *Metrics) siteResolved() {
	if m != nil && m.OnSiteResolved != nil {
		m.OnSiteResolved()
	}
}

func (m *Metrics) conflict(field string) {
	if m != nil && m.OnConflict != nil {
		m.OnConflict(field)
	}
}
