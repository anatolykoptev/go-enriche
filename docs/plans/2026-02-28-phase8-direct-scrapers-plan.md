# Phase 8: Direct Search Scrapers Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add DDG Direct and Startpage Direct as search.Provider implementations, enabling free search without SearXNG or API keys via go-stealth TLS fingerprinting.

**Architecture:** Port DDG HTML lite + d.js JSON API and Startpage POST scraping from go-engine, adapting them to go-enriche's `search.Provider` interface. Each scraper uses `stealth.BrowserClient.Do()` directly (not `*http.Client`). Results aggregate via the existing `SearXNG.aggregate()` pattern. New dependency: `github.com/PuerkitoBio/goquery` for HTML parsing.

**Tech Stack:** go-stealth (BrowserClient.Do), goquery (HTML parsing), httptest (mock servers)

---

### Task 1: BrowserDoer Interface + ChromeHeaders Helper

**Files:**
- Create: `search/doer.go`
- Test: (no test needed — interface definition)

**Step 1: Create the BrowserDoer interface and ChromeHeaders helper**

```go
package search

import (
	"io"

	stealth "github.com/anatolykoptev/go-stealth"
)

// BrowserDoer performs HTTP requests with browser-like TLS fingerprint.
// *stealth.BrowserClient satisfies this interface.
type BrowserDoer interface {
	Do(method, url string, headers map[string]string, body io.Reader) ([]byte, map[string]string, int, error)
}

// ChromeHeaders returns browser-like HTTP headers for direct scraping.
func ChromeHeaders() map[string]string {
	return stealth.ChromeHeaders()
}
```

**Step 2: Verify it compiles**

Run: `cd /home/krolik/src/go-enriche && go build ./...`
Expected: exit 0

**Step 3: Commit**

```bash
git add search/doer.go
git commit -m "feat(search): add BrowserDoer interface for direct scrapers"
```

---

### Task 2: DDG Direct Provider

**Files:**
- Create: `search/ddg.go`
- Create: `search/ddg_test.go`

**Step 1: Write the failing test**

Create `search/ddg_test.go` with 4 tests:
1. `TestDDG_SearchHTML` — mock HTML lite endpoint returning DDG-formatted HTML, verify parsing
2. `TestDDG_UnwrapURL` — test DDG redirect URL unwrapping
3. `TestDDG_ErrorStatus` — mock returning 403, verify error
4. `TestDDG_MaxResults` — verify maxResults limits output

```go
package search

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// mockBrowser implements BrowserDoer for testing.
type mockBrowser struct {
	handler func(method, url string, headers map[string]string, body io.Reader) ([]byte, map[string]string, int, error)
}

func (m *mockBrowser) Do(method, url string, headers map[string]string, body io.Reader) ([]byte, map[string]string, int, error) {
	return m.handler(method, url, headers, body)
}

func TestDDG_SearchHTML(t *testing.T) {
	t.Parallel()
	html := `<html><body>
		<div class="result">
			<a class="result__a" href="https://example.com/1">Result 1</a>
			<a class="result__snippet">First result snippet</a>
		</div>
		<div class="result">
			<a class="result__a" href="https://example.com/2">Result 2</a>
			<a class="result__snippet">Second result snippet</a>
		</div>
	</body></html>`

	bc := &mockBrowser{handler: func(method, url string, headers map[string]string, body io.Reader) ([]byte, map[string]string, int, error) {
		return []byte(html), nil, http.StatusOK, nil
	}}

	ddg := NewDDG(bc)
	result, err := ddg.Search(context.Background(), "test query", "")
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}
	if len(result.Sources) == 0 {
		t.Error("expected at least 1 source")
	}
}

func TestDDG_UnwrapURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		want  string
	}{
		{"//duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.com&rut=abc", "https://example.com"},
		{"https://example.com/page", "https://example.com/page"},
		{"/relative/path", ""},
	}
	for _, tc := range tests {
		got := ddgUnwrapURL(tc.input)
		if got != tc.want {
			t.Errorf("ddgUnwrapURL(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestDDG_ErrorStatus(t *testing.T) {
	t.Parallel()
	bc := &mockBrowser{handler: func(method, url string, headers map[string]string, body io.Reader) ([]byte, map[string]string, int, error) {
		return nil, nil, http.StatusForbidden, nil
	}}

	ddg := NewDDG(bc)
	_, err := ddg.Search(context.Background(), "q", "")
	if err == nil {
		t.Error("expected error on 403")
	}
}

func TestDDG_MaxResults(t *testing.T) {
	t.Parallel()
	var htmlParts []string
	for i := range 10 {
		htmlParts = append(htmlParts, fmt.Sprintf(
			`<div class="result"><a class="result__a" href="https://example.com/%d">R%d</a><a class="result__snippet">S%d</a></div>`,
			i, i, i))
	}
	html := "<html><body>" + strings.Join(htmlParts, "") + "</body></html>"

	bc := &mockBrowser{handler: func(method, url string, headers map[string]string, body io.Reader) ([]byte, map[string]string, int, error) {
		return []byte(html), nil, http.StatusOK, nil
	}}

	ddg := NewDDG(bc, WithDDGMaxResults(2))
	result, err := ddg.Search(context.Background(), "q", "")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(result.Sources) > 2 {
		t.Errorf("expected max 2 sources, got %d", len(result.Sources))
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /home/krolik/src/go-enriche && go test ./search/ -run TestDDG -v`
Expected: FAIL — `NewDDG` undefined

**Step 3: Write DDG Direct provider**

Create `search/ddg.go`:

```go
package search

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
	stealth "github.com/anatolykoptev/go-stealth"
)

// DDG implements Provider using direct DuckDuckGo scraping via go-stealth.
// Uses HTML lite endpoint as primary source.
type DDG struct {
	bc         BrowserDoer
	maxResults int
}

// DDGOption configures DDG.
type DDGOption func(*DDG)

// WithDDGMaxResults sets the max results to aggregate.
func WithDDGMaxResults(n int) DDGOption {
	return func(d *DDG) { d.maxResults = n }
}

// NewDDG creates a DDG Direct search provider.
// The BrowserDoer is typically a *stealth.BrowserClient.
func NewDDG(bc BrowserDoer, opts ...DDGOption) *DDG {
	d := &DDG{
		bc:         bc,
		maxResults: defaultMaxResults,
	}
	for _, o := range opts {
		o(d)
	}
	return d
}

// Search queries DuckDuckGo HTML lite and returns aggregated results.
func (d *DDG) Search(ctx context.Context, query string, timeRange string) (*SearchResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	formBody := "q=" + url.QueryEscape(query) + "&df="

	headers := stealth.ChromeHeaders()
	headers["referer"] = "https://html.duckduckgo.com/"
	headers["content-type"] = "application/x-www-form-urlencoded"

	data, _, status, err := d.bc.Do(http.MethodPost, "https://html.duckduckgo.com/html/", headers, strings.NewReader(formBody))
	if err != nil {
		return nil, fmt.Errorf("ddg: request: %w", err)
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("ddg: HTTP %d", status)
	}

	results, err := parseDDGHTML(data)
	if err != nil {
		return nil, fmt.Errorf("ddg: parse: %w", err)
	}

	return d.aggregate(results), nil
}

func (d *DDG) aggregate(results []searxngResult) *SearchResult {
	s := &SearXNG{maxResults: d.maxResults}
	return s.aggregate(results)
}

// parseDDGHTML extracts search results from DDG HTML lite response.
func parseDDGHTML(data []byte) ([]searxngResult, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(data)))
	if err != nil {
		return nil, fmt.Errorf("goquery parse: %w", err)
	}

	var results []searxngResult

	doc.Find(".result, .web-result").Each(func(_ int, s *goquery.Selection) {
		link := s.Find("a.result__a, .result__title a, a.result-link").First()
		title := strings.TrimSpace(link.Text())
		href, exists := link.Attr("href")
		if !exists || title == "" {
			return
		}

		href = ddgUnwrapURL(href)
		if href == "" {
			return
		}

		snippet := s.Find(".result__snippet, .result__body").First()
		content := strings.TrimSpace(snippet.Text())

		results = append(results, searxngResult{
			URL:     href,
			Title:   title,
			Content: content,
		})
	})

	return results, nil
}

// ddgUnwrapURL extracts the actual URL from DDG redirect wrappers.
// DDG HTML wraps links as: //duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.com&rut=...
func ddgUnwrapURL(href string) string {
	if strings.Contains(href, "duckduckgo.com/l/") || strings.Contains(href, "uddg=") {
		if u, err := url.Parse(href); err == nil {
			if uddg := u.Query().Get("uddg"); uddg != "" {
				return uddg
			}
		}
	}
	if strings.HasPrefix(href, "http") {
		return href
	}
	return ""
}
```

**Step 4: Add goquery dependency**

Run: `cd /home/krolik/src/go-enriche && go get github.com/PuerkitoBio/goquery`

**Step 5: Run tests to verify they pass**

Run: `cd /home/krolik/src/go-enriche && go test ./search/ -run TestDDG -v`
Expected: 4/4 PASS

**Step 6: Commit**

```bash
git add search/ddg.go search/ddg_test.go go.mod go.sum
git commit -m "feat(search): add DDG Direct provider with HTML lite scraping"
```

---

### Task 3: Startpage Direct Provider

**Files:**
- Create: `search/startpage.go`
- Create: `search/startpage_test.go`

**Step 1: Write the failing test**

Create `search/startpage_test.go` with 3 tests:
1. `TestStartpage_Search` — mock returning Startpage-formatted HTML, verify parsing
2. `TestStartpage_ErrorStatus` — mock returning 403, verify error
3. `TestStartpage_MaxResults` — verify maxResults limits

```go
package search

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestStartpage_Search(t *testing.T) {
	t.Parallel()
	html := `<html><body>
		<div class="w-gl__result">
			<a class="w-gl__result-title" href="https://example.com/1">Result 1</a>
			<p class="w-gl__description">First result description</p>
		</div>
		<div class="w-gl__result">
			<a class="w-gl__result-title" href="https://example.com/2">Result 2</a>
			<p class="w-gl__description">Second result description</p>
		</div>
	</body></html>`

	bc := &mockBrowser{handler: func(method, url string, headers map[string]string, body io.Reader) ([]byte, map[string]string, int, error) {
		if method != http.MethodPost {
			t.Errorf("expected POST, got %s", method)
		}
		return []byte(html), nil, http.StatusOK, nil
	}}

	sp := NewStartpage(bc)
	result, err := sp.Search(context.Background(), "test query", "")
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}
	if len(result.Sources) != 2 {
		t.Errorf("expected 2 sources, got %d", len(result.Sources))
	}
	if result.Context == "" {
		t.Error("expected non-empty context")
	}
}

func TestStartpage_ErrorStatus(t *testing.T) {
	t.Parallel()
	bc := &mockBrowser{handler: func(method, url string, headers map[string]string, body io.Reader) ([]byte, map[string]string, int, error) {
		return nil, nil, http.StatusForbidden, nil
	}}

	sp := NewStartpage(bc)
	_, err := sp.Search(context.Background(), "q", "")
	if err == nil {
		t.Error("expected error on 403")
	}
}

func TestStartpage_MaxResults(t *testing.T) {
	t.Parallel()
	var htmlParts []string
	for i := range 10 {
		htmlParts = append(htmlParts, fmt.Sprintf(
			`<div class="w-gl__result"><a class="w-gl__result-title" href="https://example.com/%d">R%d</a><p class="w-gl__description">D%d</p></div>`,
			i, i, i))
	}
	html := "<html><body>" + strings.Join(htmlParts, "") + "</body></html>"

	bc := &mockBrowser{handler: func(method, url string, headers map[string]string, body io.Reader) ([]byte, map[string]string, int, error) {
		return []byte(html), nil, http.StatusOK, nil
	}}

	sp := NewStartpage(bc, WithStartpageMaxResults(2))
	result, err := sp.Search(context.Background(), "q", "")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(result.Sources) > 2 {
		t.Errorf("expected max 2 sources, got %d", len(result.Sources))
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /home/krolik/src/go-enriche && go test ./search/ -run TestStartpage -v`
Expected: FAIL — `NewStartpage` undefined

**Step 3: Write Startpage Direct provider**

Create `search/startpage.go`:

```go
package search

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"
	stealth "github.com/anatolykoptev/go-stealth"
)

// Startpage implements Provider using direct Startpage scraping via go-stealth.
type Startpage struct {
	bc         BrowserDoer
	maxResults int
}

// StartpageOption configures Startpage.
type StartpageOption func(*Startpage)

// WithStartpageMaxResults sets the max results to aggregate.
func WithStartpageMaxResults(n int) StartpageOption {
	return func(s *Startpage) { s.maxResults = n }
}

// NewStartpage creates a Startpage Direct search provider.
// The BrowserDoer is typically a *stealth.BrowserClient.
func NewStartpage(bc BrowserDoer, opts ...StartpageOption) *Startpage {
	s := &Startpage{
		bc:         bc,
		maxResults: defaultMaxResults,
	}
	for _, o := range opts {
		o(s)
	}
	return s
}

// Search queries Startpage directly and returns aggregated results.
func (sp *Startpage) Search(ctx context.Context, query string, timeRange string) (*SearchResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	formBody := "query=" + url.QueryEscape(query) + "&cat=web&language=english"

	headers := stealth.ChromeHeaders()
	headers["referer"] = "https://www.startpage.com/"
	headers["content-type"] = "application/x-www-form-urlencoded"
	headers["accept"] = "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8"

	data, _, status, err := sp.bc.Do(http.MethodPost, "https://www.startpage.com/sp/search", headers, strings.NewReader(formBody))
	if err != nil {
		return nil, fmt.Errorf("startpage: request: %w", err)
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("startpage: HTTP %d", status)
	}

	results, err := parseStartpageHTML(data)
	if err != nil {
		return nil, fmt.Errorf("startpage: parse: %w", err)
	}

	return sp.aggregate(results), nil
}

func (sp *Startpage) aggregate(results []searxngResult) *SearchResult {
	s := &SearXNG{maxResults: sp.maxResults}
	return s.aggregate(results)
}

// parseStartpageHTML extracts search results from Startpage HTML response.
func parseStartpageHTML(data []byte) ([]searxngResult, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(data)))
	if err != nil {
		return nil, fmt.Errorf("goquery parse: %w", err)
	}

	var results []searxngResult

	doc.Find(".w-gl__result, .result").Each(func(_ int, s *goquery.Selection) {
		link := s.Find("a.w-gl__result-title, h3 a, a.result-link").First()
		title := strings.TrimSpace(link.Text())
		href, exists := link.Attr("href")
		if !exists || title == "" {
			return
		}

		desc := s.Find("p.w-gl__description, .w-gl__description, p.result-description").First()
		content := strings.TrimSpace(desc.Text())

		// Skip empty/ad results.
		if href == "" || strings.Contains(href, "startpage.com/do/") {
			return
		}

		results = append(results, searxngResult{
			URL:     href,
			Title:   title,
			Content: content,
		})
	})

	return results, nil
}
```

**Step 4: Run tests to verify they pass**

Run: `cd /home/krolik/src/go-enriche && go test ./search/ -run TestStartpage -v`
Expected: 3/3 PASS

**Step 5: Commit**

```bash
git add search/startpage.go search/startpage_test.go
git commit -m "feat(search): add Startpage Direct provider with form scraping"
```

---

### Task 4: Full Test Suite + Lint + ROADMAP Update

**Files:**
- Modify: `docs/ROADMAP.md`

**Step 1: Run full test suite**

Run: `cd /home/krolik/src/go-enriche && go test -race ./...`
Expected: All tests pass (126 old + 7 new = ~133)

**Step 2: Run linter**

Run: `cd /home/krolik/src/go-enriche && make lint`
Expected: 0 issues

**Step 3: Fix any lint issues**

Fix gosec, errcheck, or other issues found.

**Step 4: Update ROADMAP.md**

Add Phase 8 section after Phase 7:

```markdown
## Phase 8: Direct Search Scrapers ✅

**Goal**: Free search without SearXNG or API keys via go-stealth TLS fingerprinting.

- [x] `search/doer.go` — `BrowserDoer` interface + `ChromeHeaders()` helper
- [x] `search/ddg.go` — DDG HTML lite scraper implementing `Provider`, goquery parsing, URL unwrapping
- [x] `search/startpage.go` — Startpage Direct scraper implementing `Provider`, goquery parsing
- [x] Tests: 7 new (4 DDG + 3 Startpage), lint clean
- [x] ~133 total tests, race-clean

**Success**: DDG and Startpage as Provider implementations. Works without SearXNG. ✅
```

**Step 5: Run full verification again**

Run: `cd /home/krolik/src/go-enriche && go test -race ./... && make lint`
Expected: All pass, 0 lint issues

**Step 6: Commit, tag, push**

```bash
git add docs/ROADMAP.md
git commit -m "docs: mark Phase 8 (direct scrapers) complete"
git tag v0.4.0
git push origin main --tags
```

---

### Task 5: Update go-wp dependency

**Files:**
- Modify: `/home/krolik/src/go-wp/go.mod`

**Step 1: Update go-enriche in go-wp**

Run: `cd /home/krolik/src/go-wp && go get github.com/anatolykoptev/go-enriche@v0.4.0 && go mod tidy`

**Step 2: Verify go-wp builds and tests pass**

Run: `cd /home/krolik/src/go-wp && go test ./... && make lint`
Expected: All pass

**Step 3: Commit**

```bash
cd /home/krolik/src/go-wp
git add go.mod go.sum
git commit -m "deps: upgrade go-enriche to v0.4.0 (direct scrapers)"
```

**Step 4: Deploy go-wp**

Run: `cd ~/deploy/krolik-server && docker compose build --no-cache go-wp && docker compose up -d --no-deps --force-recreate go-wp`

**Step 5: Verify health**

Run: `curl -s http://127.0.0.1:8894/health`
Expected: OK
