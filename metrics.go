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
	// OnConflict fires once per field-adjudication where two DIFFERENT-priority
	// sources offered a genuinely DIFFERENT value — order-independently: both the
	// override case (a higher source overrides a present, differing lower value)
	// and the rejection case (a lower source is rejected because a higher source
	// already owns a differing value). field is the fact name
	// (enrich_conflict_total{field}).
	OnConflict func(field string)

	// OnBrowserRender fires once per Enrich call where the headless-browser
	// render was triggered for the official site, with the reason it fired:
	//   "thin_content"     — readability content was below minExtractChars
	//   "absent_contacts"  — the raw HTML carried no phone/address/hours fact,
	//                        so JS-injected contacts may be hiding behind render
	// Lets the consumer track enrich_browser_render_total{reason} and confirm
	// the full-JS render path actually fires in production (observability for
	// the JS-injected-contacts class).
	OnBrowserRender func(reason string)
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

func (m *Metrics) browserRender(reason string) {
	if m != nil && m.OnBrowserRender != nil && reason != "" {
		m.OnBrowserRender(reason)
	}
}
