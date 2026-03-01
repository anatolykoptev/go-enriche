package enriche

// Metrics provides callback hooks for observability.
// Any nil field is safely ignored (no-op).
type Metrics struct {
	OnCacheHit       func()
	OnCacheMiss      func()
	OnFetchError     func()
	OnSearchError    func()
	OnMapsCheckError func()
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
