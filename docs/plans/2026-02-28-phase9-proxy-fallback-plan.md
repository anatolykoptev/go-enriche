# Phase 9: Proxy Fallback (Tor + ProxyPool) Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add Tor as a free proxy fallback when Webshare (paid) returns 402, using a new `proxypool.Static` pool in go-stealth, and wire DDG/Startpage providers to use ProxyPool rotation.

**Architecture:** Three repos touched: go-stealth (new Static pool type), krolik-server (Tor Docker service), go-enriche (ProxyPool support in DDG/Startpage). go-wp wires it all together with init-time fallback: try Webshare → if 402 → use Tor.

**Tech Stack:** Go 1.24+, go-stealth proxypool, Docker (Tor SOCKS5), go-enriche search providers

---

## Context

- DDG/Startpage providers require a proxy — data center IPs are blocked (DDG: timeout, Startpage: 307→CAPTCHA)
- Current API: `NewDDG(proxyURL string)` — accepts a single proxy URL
- Webshare (paid) provides rotating proxies via API; returns 402 when subscription expires
- Tor provides free SOCKS5 proxy at `socks5://tor:9050` in Docker network
- `go-stealth/proxypool` has `ProxyPool` interface (`Next()`, `Len()`, `TransportProxy()`) and `ProxyPoolProvider` interface (`Next()` only)
- `go-stealth` has `WithProxyPool(pool ProxyPoolProvider)` client option for per-request rotation

## Key Interfaces (existing)

```go
// go-stealth/proxypool/proxypool.go
type ProxyPool interface {
    Next() string
    Len() int
    TransportProxy() func(*http.Request) (*url.URL, error)
}

// go-stealth/client.go
type ProxyPoolProvider interface {
    Next() string
}
```

## Key Files

| File | Repo | Action |
|------|------|--------|
| `proxypool/static.go` | go-stealth | Create |
| `proxypool/static_test.go` | go-stealth | Create |
| `docker-compose.yml` | krolik-server | Modify (add Tor service) |
| `search/ddg.go` | go-enriche | Modify (add ProxyPool option) |
| `search/startpage.go` | go-enriche | Modify (add ProxyPool option) |
| `search/ddg_test.go` | go-enriche | Modify (add ProxyPool test) |
| `search/startpage_test.go` | go-enriche | Modify (add ProxyPool test) |

---

### Task 1: Static ProxyPool in go-stealth

**Files:**
- Create: `/home/krolik/src/go-stealth/proxypool/static.go`
- Create: `/home/krolik/src/go-stealth/proxypool/static_test.go`

**Step 1: Write the failing tests**

File: `/home/krolik/src/go-stealth/proxypool/static_test.go`

```go
package proxypool

import (
	"testing"
)

func TestNewStatic_SingleProxy(t *testing.T) {
	pool := NewStatic("socks5://tor:9050")
	if pool.Len() != 1 {
		t.Fatalf("expected 1 proxy, got %d", pool.Len())
	}
	got := pool.Next()
	if got != "socks5://tor:9050" {
		t.Fatalf("expected socks5://tor:9050, got %s", got)
	}
}

func TestNewStatic_MultipleProxies(t *testing.T) {
	pool := NewStatic("socks5://a:1080", "socks5://b:1080", "socks5://c:1080")
	if pool.Len() != 3 {
		t.Fatalf("expected 3 proxies, got %d", pool.Len())
	}

	first := pool.Next()
	second := pool.Next()
	third := pool.Next()
	fourth := pool.Next()

	if first == second && second == third {
		t.Fatal("round-robin should return different proxies")
	}
	if first != fourth {
		t.Fatalf("expected rotation back to first, got %s vs %s", first, fourth)
	}
}

func TestNewStatic_Empty(t *testing.T) {
	pool := NewStatic()
	if pool.Len() != 0 {
		t.Fatalf("expected 0 proxies, got %d", pool.Len())
	}
	got := pool.Next()
	if got != "" {
		t.Fatalf("expected empty string, got %s", got)
	}
}

func TestStatic_TransportProxy(t *testing.T) {
	pool := NewStatic("http://10.0.0.1:8080")
	fn := pool.TransportProxy()
	if fn == nil {
		t.Fatal("TransportProxy should not return nil")
	}

	proxyURL, err := fn(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if proxyURL.Hostname() != "10.0.0.1" {
		t.Fatalf("expected host 10.0.0.1, got %s", proxyURL.Hostname())
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `cd /home/krolik/src/go-stealth && go test ./proxypool/ -run TestNewStatic -v`
Expected: FAIL — `NewStatic` undefined

**Step 3: Write minimal implementation**

File: `/home/krolik/src/go-stealth/proxypool/static.go`

```go
package proxypool

import (
	"net/http"
	"net/url"
	"sync/atomic"
)

// Static implements ProxyPool with a fixed list of proxy URLs.
// Useful for wrapping known proxies (e.g. Tor SOCKS5) into a pool.
type Static struct {
	proxies []string
	counter atomic.Uint64
}

// NewStatic creates a ProxyPool from static proxy URLs.
//
// Example:
//
//	pool := proxypool.NewStatic("socks5://tor:9050")
//	pool := proxypool.NewStatic("socks5://a:1080", "socks5://b:1080")
func NewStatic(urls ...string) *Static {
	return &Static{proxies: urls}
}

// Next returns the next proxy URL in round-robin order.
// Returns empty string if the pool is empty.
func (s *Static) Next() string {
	if len(s.proxies) == 0 {
		return ""
	}
	idx := s.counter.Add(1) % uint64(len(s.proxies))
	return s.proxies[idx]
}

// Len returns the number of proxies in the pool.
func (s *Static) Len() int {
	return len(s.proxies)
}

// TransportProxy returns a function suitable for http.Transport.Proxy.
func (s *Static) TransportProxy() func(*http.Request) (*url.URL, error) {
	return func(_ *http.Request) (*url.URL, error) {
		next := s.Next()
		if next == "" {
			return nil, nil
		}
		return url.Parse(next)
	}
}
```

**Step 4: Run tests to verify they pass**

Run: `cd /home/krolik/src/go-stealth && go test ./proxypool/ -run TestNewStatic -v`
Expected: PASS (4 tests)

Run full suite: `cd /home/krolik/src/go-stealth && go test ./...`
Expected: All existing tests still pass

**Step 5: Lint**

Run: `cd /home/krolik/src/go-stealth && golangci-lint run ./proxypool/`
Expected: 0 issues

**Step 6: Commit**

```bash
cd /home/krolik/src/go-stealth
git add proxypool/static.go proxypool/static_test.go
git commit -m "feat(proxypool): add Static pool for fixed proxy URLs

Wraps one or more static proxy URLs (e.g. Tor SOCKS5) into a ProxyPool
with round-robin rotation. Useful as a free fallback when Webshare (paid)
returns 402.

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

### Task 2: Add Tor Service to Docker Compose

**Files:**
- Modify: `/home/krolik/deploy/krolik-server/docker-compose.yml`

**Step 1: Add Tor service**

Add after the `redis` service block (before `cliproxyapi`), the following service:

```yaml
  # Tor SOCKS5 proxy — free fallback when Webshare (paid) returns 402
  # Used by go-search, go-wp, go-job for DDG/Startpage direct scrapers
  tor:
    image: dperson/torproxy:latest
    container_name: tor
    restart: unless-stopped
    labels:
      dozor.group: "infra"
    security_opt:
      - no-new-privileges:true
    logging: *default-logging
    environment:
      - TZ=America/Los_Angeles
    deploy:
      resources:
        limits:
          memory: 128M
    healthcheck:
      test: ["CMD-SHELL", "curl -sf --socks5-hostname localhost:9050 https://check.torproject.org/api/ip || exit 1"]
      interval: 60s
      timeout: 10s
      retries: 3
      start_period: 30s
    networks:
      - backend
      - egress  # Tor needs outbound internet access
```

Note: No port publishing needed — only accessible from other Docker containers via `socks5://tor:9050`.

**Step 2: Start Tor and verify**

Run:
```bash
cd ~/deploy/krolik-server
docker compose up -d tor
docker compose ps tor
docker compose logs tor | tail -5
```
Expected: Tor container running, connected to Tor network

**Step 3: Verify SOCKS5 works from another container**

Run:
```bash
docker compose exec go-search wget -q -O - --timeout=15 \
  -e "https_proxy=socks5://tor:9050" \
  https://check.torproject.org/api/ip 2>/dev/null || echo "SOCKS5 test via wget failed, trying curl..."
docker compose exec go-search sh -c 'curl -sf --socks5-hostname tor:9050 https://check.torproject.org/api/ip' 2>/dev/null || echo "curl also unavailable in container"
```
Expected: JSON response with `IsTor: true` (or confirmation that connectivity works)

**Step 4: Commit (no git for docker-compose, just verify)**

docker-compose.yml is in krolik-server (not a git repo per CLAUDE.md), so no commit needed.

---

### Task 3: DDG/Startpage ProxyPool Support in go-enriche

**Files:**
- Modify: `/home/krolik/src/go-enriche/search/ddg.go`
- Modify: `/home/krolik/src/go-enriche/search/startpage.go`
- Modify: `/home/krolik/src/go-enriche/search/ddg_test.go`
- Modify: `/home/krolik/src/go-enriche/search/startpage_test.go`
- Modify: `/home/krolik/src/go-enriche/search/doer.go`

**Step 1: Write the failing tests**

Add to `/home/krolik/src/go-enriche/search/ddg_test.go`:

```go
func TestDDG_WithProxyPool(t *testing.T) {
	t.Parallel()

	html := `<html><body>
		<div class="result">
			<a class="result__a" href="https://example.com/pool">Pool Result</a>
			<span class="result__snippet">Pool snippet</span>
		</div>
	</body></html>`

	mock := &mockBrowser{
		handler: func(_, _ string, _ map[string]string, _ io.Reader) ([]byte, map[string]string, int, error) {
			return []byte(html), nil, 200, nil
		},
	}

	// ProxyPool set → proxyURL can be empty.
	ddg, err := NewDDG("", WithDDGDoer(mock), WithDDGProxyPool(&staticPool{url: "socks5://tor:9050"}))
	if err != nil {
		t.Fatalf("NewDDG with pool should succeed: %v", err)
	}

	result, err := ddg.Search(context.Background(), "test", "")
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}
	if len(result.Sources) != 1 {
		t.Errorf("expected 1 source, got %d", len(result.Sources))
	}
}
```

Add to `/home/krolik/src/go-enriche/search/startpage_test.go`:

```go
func TestStartpage_WithProxyPool(t *testing.T) {
	t.Parallel()

	html := `<html><body>
		<div class="w-gl__result">
			<a class="w-gl__result-title" href="https://example.com/pool">Pool Result</a>
			<p class="w-gl__description">Pool description</p>
		</div>
	</body></html>`

	mock := &mockBrowser{
		handler: func(_, _ string, _ map[string]string, _ io.Reader) ([]byte, map[string]string, int, error) {
			return []byte(html), nil, 200, nil
		},
	}

	sp, err := NewStartpage("", WithStartpageDoer(mock), WithStartpageProxyPool(&staticPool{url: "socks5://tor:9050"}))
	if err != nil {
		t.Fatalf("NewStartpage with pool should succeed: %v", err)
	}

	result, err := sp.Search(context.Background(), "test", "")
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}
	if len(result.Sources) != 1 {
		t.Errorf("expected 1 source, got %d", len(result.Sources))
	}
}
```

Add mock `staticPool` to `/home/krolik/src/go-enriche/search/ddg_test.go` (shared by both test files since same package):

```go
// staticPool is a test mock implementing stealth.ProxyPoolProvider.
type staticPool struct{ url string }

func (p *staticPool) Next() string { return p.url }
```

**Step 2: Run tests to verify they fail**

Run: `cd /home/krolik/src/go-enriche && go test ./search/ -run TestDDG_WithProxyPool -v`
Expected: FAIL — `WithDDGProxyPool` undefined

**Step 3: Update DDG implementation**

Modify `/home/krolik/src/go-enriche/search/ddg.go`:

1. Add `proxyPool` field to `DDG` struct:
```go
type DDG struct {
	bc         BrowserDoer
	proxyPool  ProxyPoolProvider  // if set, used instead of single proxy
	maxResults int
}
```

2. Add `ProxyPoolProvider` interface to `doer.go` (to avoid importing stealth in consumers):
```go
// ProxyPoolProvider returns the next proxy URL for rotation.
// Compatible with stealth.ProxyPoolProvider.
type ProxyPoolProvider interface {
	Next() string
}
```

3. Add `WithDDGProxyPool` option:
```go
// WithDDGProxyPool sets a proxy pool for per-request rotation.
// When set, proxyURL in NewDDG can be empty.
func WithDDGProxyPool(pool ProxyPoolProvider) DDGOption {
	return func(d *DDG) { d.proxyPool = pool }
}
```

4. Update `NewDDG` constructor to support pool:
```go
func NewDDG(proxyURL string, opts ...DDGOption) (*DDG, error) {
	d := &DDG{maxResults: defaultMaxResults}
	for _, o := range opts {
		o(d)
	}

	// Custom doer already set (testing).
	if d.bc != nil {
		return d, nil
	}

	// Need at least a proxy URL or a proxy pool.
	if proxyURL == "" && d.proxyPool == nil {
		return nil, errors.New("ddg: proxy URL or pool is required (data center IPs are blocked)")
	}

	var stealthOpts []stealth.ClientOption
	stealthOpts = append(stealthOpts, stealth.WithTimeout(defaultStealthTimeout))
	if d.proxyPool != nil {
		stealthOpts = append(stealthOpts, stealth.WithProxyPool(d.proxyPool))
	} else {
		stealthOpts = append(stealthOpts, stealth.WithProxy(proxyURL))
	}

	bc, err := stealth.NewClient(stealthOpts...)
	if err != nil {
		return nil, fmt.Errorf("ddg: stealth client: %w", err)
	}
	d.bc = bc
	return d, nil
}
```

**Step 4: Update Startpage implementation (identical pattern)**

Same changes to `/home/krolik/src/go-enriche/search/startpage.go`:
- Add `proxyPool ProxyPoolProvider` field
- Add `WithStartpageProxyPool(pool)` option
- Update `NewStartpage` constructor with same pool-or-url logic

**Step 5: Run tests**

Run: `cd /home/krolik/src/go-enriche && go test ./search/ -v -count=1`
Expected: All tests pass (existing + new ProxyPool tests)

Run: `cd /home/krolik/src/go-enriche && go test ./... -count=1`
Expected: All 132+ tests pass

**Step 6: Lint**

Run: `cd /home/krolik/src/go-enriche && golangci-lint run ./...`
Expected: 0 issues

**Step 7: Commit**

```bash
cd /home/krolik/src/go-enriche
git add search/doer.go search/ddg.go search/startpage.go search/ddg_test.go search/startpage_test.go
git commit -m "feat(search): add ProxyPool support to DDG and Startpage providers

WithDDGProxyPool and WithStartpageProxyPool options enable per-request
proxy rotation via stealth.ProxyPoolProvider. When a pool is set, the
proxyURL argument can be empty.

Enables Webshare → Tor fallback: init Webshare pool, if 402 use
proxypool.NewStatic(\"socks5://tor:9050\") instead.

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

### Task 4: Tag and Push go-stealth

**Step 1: Run full test suite**

Run: `cd /home/krolik/src/go-stealth && go test ./... -v -count=1`
Expected: All tests pass

**Step 2: Tag release**

```bash
cd /home/krolik/src/go-stealth
git tag v1.1.0
git push origin main --tags
```

---

### Task 5: Update go-enriche dependency and tag

**Step 1: Update go-stealth dependency**

```bash
cd /home/krolik/src/go-enriche
go get github.com/anatolykoptev/go-stealth@v1.1.0
go mod tidy
```

**Step 2: Run full test suite**

Run: `cd /home/krolik/src/go-enriche && go test ./... -v -count=1`
Expected: All tests pass

**Step 3: Tag release**

```bash
cd /home/krolik/src/go-enriche
git add go.mod go.sum
git commit -m "chore: bump go-stealth to v1.1.0 (Static pool)

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
git tag v0.5.0
git push origin main --tags
```

---

### Task 6: Update go-wp consumer and deploy

**Step 1: Update go-enriche dependency**

```bash
cd /home/krolik/src/go-wp
go get github.com/anatolykoptev/go-enriche@v0.5.0
go mod tidy
```

**Step 2: Wire DDG/Startpage with proxy fallback in go-wp**

Modify `/home/krolik/src/go-wp/internal/wpserver/tool_enrich.go` `getEnricher()`:

```go
func getEnricher() *enriche.Enricher {
	wpEnricherOnce.Do(func() {
		var opts []enriche.Option

		if sc := engine.StealthClient(); sc != nil {
			opts = append(opts, enriche.WithStealth(sc))
		}

		// Build search provider chain: DDG → Startpage → SearXNG fallback.
		var searchProviders []search.Provider

		// Direct scrapers need a proxy (data center IPs are blocked).
		// Try Webshare (paid) → fallback to Tor (free).
		proxyPool := engine.SearchProxyPool()
		if proxyPool != nil {
			if ddg, err := search.NewDDG("", search.WithDDGProxyPool(proxyPool)); err == nil {
				searchProviders = append(searchProviders, ddg)
			}
			if sp, err := search.NewStartpage("", search.WithStartpageProxyPool(proxyPool)); err == nil {
				searchProviders = append(searchProviders, sp)
			}
		}

		if engine.Cfg.SearxngURL != "" {
			searchProviders = append(searchProviders, search.NewSearXNG(engine.Cfg.SearxngURL))
		}

		if len(searchProviders) > 0 {
			opts = append(opts, enriche.WithSearch(search.NewFallback(searchProviders...)))
		}

		opts = append(opts, enriche.WithMaxContentLen(4000))

		wpEnricher = enriche.New(opts...)
	})
	return wpEnricher
}
```

**Step 3: Add `SearchProxyPool()` to go-wp engine**

Create or modify `/home/krolik/src/go-wp/internal/engine/stealth.go` to add:

```go
// SearchProxyPool returns a ProxyPoolProvider for direct search scrapers.
// Tries Webshare (paid) first; falls back to Tor (free) if Webshare fails.
// Returns nil if neither is available.
func SearchProxyPool() stealth.ProxyPoolProvider {
	apiKey := os.Getenv("WEBSHARE_API_KEY")
	if apiKey != "" {
		pool, err := proxypool.NewWebshare(apiKey)
		if err != nil {
			slog.Warn("webshare pool init failed, trying Tor fallback", slog.Any("error", err))
		} else {
			slog.Info("search proxy: using Webshare", slog.Int("proxies", pool.Len()))
			return pool
		}
	}

	// Fallback: Tor SOCKS5 (free, available in Docker network).
	torProxy := os.Getenv("TOR_PROXY")
	if torProxy == "" {
		torProxy = "socks5://tor:9050"
	}
	slog.Info("search proxy: using Tor fallback", slog.String("proxy", torProxy))
	return proxypool.NewStatic(torProxy)
}
```

**Step 4: Add `TOR_PROXY` env var to docker-compose.yml go-wp service** (optional, default is `socks5://tor:9050`):

Add `tor` to go-wp's `depends_on`:
```yaml
    depends_on:
      redis:
        condition: service_healthy
      cliproxyapi:
        condition: service_healthy
      go-search:
        condition: service_started
      tor:
        condition: service_healthy
```

**Step 5: Build and deploy**

```bash
cd ~/deploy/krolik-server
docker compose build --no-cache go-wp
docker compose up -d --no-deps --force-recreate go-wp
docker compose ps go-wp
docker compose logs go-wp 2>&1 | grep -i "search proxy" | head -3
```
Expected: Logs show "search proxy: using Webshare" (if paid) or "search proxy: using Tor fallback"

**Step 6: Verify enrichment works**

Test with a real enrichment call through MCP and check logs for DDG/Startpage activity.

**Step 7: Commit go-wp changes**

```bash
cd /home/krolik/src/go-wp
git add go.mod go.sum internal/engine/stealth.go internal/wpserver/tool_enrich.go
git commit -m "feat: wire DDG/Startpage search with proxy fallback (Webshare → Tor)

Search providers now use direct scrapers (DDG, Startpage) as primary,
with SearXNG as fallback. Proxy pool: Webshare (paid) if available,
Tor SOCKS5 (free) otherwise.

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
git push origin main
```

---

## Dependency Order

```
Task 1 (go-stealth: Static pool)
  → Task 4 (go-stealth: tag v1.1.0)
    → Task 5 (go-enriche: bump go-stealth, tag v0.5.0)

Task 2 (docker-compose: Tor service) — independent

Task 3 (go-enriche: ProxyPool options) — depends on Task 1
  → Task 5 (go-enriche: tag v0.5.0)
    → Task 6 (go-wp: wire + deploy) — depends on Task 2 + Task 5
```

## Verification Checklist

- [ ] `go-stealth`: `NewStatic` works, all proxypool tests pass
- [ ] `docker-compose`: Tor container running, SOCKS5 reachable from other containers
- [ ] `go-enriche`: DDG/Startpage accept ProxyPool, all 134+ tests pass
- [ ] `go-wp`: Enricher uses DDG→Startpage→SearXNG fallback chain
- [ ] `go-wp`: Logs show proxy source (Webshare or Tor)
- [ ] Integration: real search query returns results via DDG or Startpage through Tor
