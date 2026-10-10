package maps

import (
	"context"
	"strings"
	"time"
)

const orgFetchTimeout = 15 * time.Second

// OxBrowserOrgFetcher returns an OrgFetcher that renders Yandex Maps org pages
// via ox-browser /fetch. Uses Chrome TLS fingerprint to bypass Yandex's
// anti-bot detection (plain HTTP gets blocked/empty response).
// Fast (~2s) unlike byparr headless (~6s).
// Requests carry X-Internal-Secret from INTERNAL_SERVICE_SECRET when set
// (scoped to the ox-browser origin by go-kit svcauth).
func OxBrowserOrgFetcher(oxBrowserURL string) OrgFetcher {
	baseURL := strings.TrimRight(oxBrowserURL, "/")
	client := oxHTTPClient(orgFetchTimeout, baseURL)

	return func(ctx context.Context, orgURL string) (string, error) {
		return oxFetch(ctx, client, baseURL, orgURL)
	}
}
