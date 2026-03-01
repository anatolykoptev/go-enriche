# Phase 5: go-wp Migration Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace go-wp's monolithic enrichment code with go-enriche library calls, keeping the MCP handler as a thin wrapper.

**Architecture:** go-wp's `tool_enrich.go` handler stays as MCP glue — parses input, converts types, calls `enriche.Enrich()`, maps results back to output types. Two helper files (`tool_enrich_extract.go`, `tool_enrich_fetch.go`) are deleted entirely. The research store L1 cache for places and `statusWebsiteDown` upgrade logic remain in go-wp as they're go-wp-specific concerns.

**Tech Stack:** go-enriche (enriche, cache, search, fetch packages), go-wp (engine, research, wpserver)

---

## Context

**What exists today in go-wp:**
- `internal/wpserver/tool_enrich.go` (364 lines) — MCP handler + `enrichSingle()` + `buildSearxngQuery()` + `fetchSearxngContext()`
- `internal/wpserver/tool_enrich_extract.go` (259 lines) — `extractArticleText()`, `extractFacts()`, `extractOGImage()`, `applyLDJSON()`, `applyRegexFacts()`, regex helpers
- `internal/wpserver/tool_enrich_fetch.go` (110 lines) — `fetchPageWithStatus()`, `fetchPage()`, `extractHost()`, status constants
- `internal/wpserver/tool_enrich_test.go` (95 lines) — tests for `effectiveName()`, `buildSearxngQuery()`, `extractHost()`

**What go-enriche provides:**
- `enriche.New(opts...).Enrich(ctx, Item) (*Result, error)` — full pipeline: fetch+extract+search+cache
- `search.NewSearXNG(baseURL)` implementing `search.Provider`
- `cache.NewMemory()`, `cache.NewRedis()`, `cache.NewTiered()` implementing `cache.Cache`
- `fetch.NewFetcher(fetch.WithClient(c))` — HTTP fetch with status detection + singleflight
- `extract.ExtractFacts()`, `extract.ExtractText()`, `extract.ExtractOGImage()`, `extract.ExtractDate()` — all extraction

**go-wp-specific logic to preserve:**
1. Research store L1 cache for places mode (Redis-backed, 7-day TTL)
2. `statusWebsiteDown` upgrade: if status=unreachable AND SearXNG has context → upgrade to website_down
3. `effectiveName()`: name→title backward compat
4. `engine.CacheGetJSON/CacheSetJSON` for per-URL/per-query caching (go-wp's own cache, not go-enriche's)
5. Input/output MCP types (`EnrichInput`, `EnrichedItem`, `ItemFacts`, `EnrichOutput`)
6. `toolutil.RecoverLog("enrich")` panic recovery per goroutine

**Migration approach:**
- Do NOT use go-enriche's built-in cache (engine cache is go-wp's concern)
- Do NOT use go-enriche's built-in concurrency (handler already has goroutine-per-item)
- Use go-enriche's Enricher with `WithStealth()` + `WithSearch()` only
- Call `enricher.Enrich()` per item, replacing `enrichSingle()` internals
- Map `enriche.Result` → `EnrichedItem` in the handler

---

### Task 1: Add go-enriche dependency to go-wp

**Files:**
- Modify: `/home/krolik/src/go-wp/go.mod`

**Step 1: Add go-enriche dependency**

```bash
cd /home/krolik/src/go-wp && go get github.com/anatolykoptev/go-enriche@latest
```

**Step 2: Verify dependency resolves**

Run: `cd /home/krolik/src/go-wp && go mod tidy`
Expected: No errors. go-enriche appears in go.mod require block.

**Step 3: Verify build still passes**

Run: `cd /home/krolik/src/go-wp && go build ./...`
Expected: exit 0

---

### Task 2: Rewrite tool_enrich.go handler to use go-enriche

**Files:**
- Modify: `/home/krolik/src/go-wp/internal/wpserver/tool_enrich.go`

**Key decisions:**
- Create the `enriche.Enricher` once per `enrichSingle()` call (or lazily in `registerEnrich`)
- Actually, create it lazily in module scope (sync.Once) so stealth + SearXNG are configured once
- The handler keeps its own per-URL cache via `engine.CacheGetJSON/CacheSetJSON`
- Research store L1 check/store stays in the handler
- `statusWebsiteDown` upgrade stays in the handler

**Step 1: Rewrite `tool_enrich.go`**

The new file should:
1. Remove all constants except `enrichModeNews/Places/Events` (status constants come from go-enriche)
2. Remove `cachedPage`, `cachedSearxng` structs
3. Remove `enrichSingle()`, `buildSearxngQuery()`, `fetchSearxngContext()` functions
4. Keep: `EnrichInput`, `enrichInputItem`, `effectiveName()`, `EnrichedItem`, `ItemFacts`, `EnrichOutput`
5. Keep: `registerEnrich()` MCP handler
6. Add: lazy `enriche.Enricher` initialization (sync.Once)
7. Add: new `enrichSingle()` that:
   - Checks research store L1 (places mode) — same as before
   - Calls `enricher.Enrich(ctx, enriche.Item{...})`
   - Maps `enriche.Result` → `EnrichedItem`
   - Applies `statusWebsiteDown` upgrade
   - Stores to research store L1 (places mode) — same as before

**New `tool_enrich.go` structure (complete code):**

```go
package wpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	enriche "github.com/anatolykoptev/go-enriche"
	"github.com/anatolykoptev/go-enriche/search"
	"github.com/anatolykoptev/go-wp/internal/engine"
	"github.com/anatolykoptev/go-wp/internal/research"
	"github.com/anatolykoptev/go-wp/internal/toolutil"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	enrichModeNews   = "news"
	enrichModePlaces = "places"
	enrichModeEvents = "events"
)

// EnrichInput is the typed input for wp_enrich.
type EnrichInput struct {
	Mode string `json:"mode,omitempty" jsonschema:"Enrichment mode: news (default), places, events"`
	Data string `json:"data"           jsonschema:"JSON array of items to enrich"`
}

// enrichInputItem is a unified input item parsed from Data JSON.
type enrichInputItem struct {
	Name    string `json:"name"`
	Title   string `json:"title"`
	URL     string `json:"url"`
	City    string `json:"city"`
	Source  string `json:"source"`
	Topic   string `json:"topic"`
	Snippet string `json:"snippet"`
	NewsID  string `json:"news_id"`
}

func (it *enrichInputItem) effectiveName() string {
	if it.Name != "" {
		return it.Name
	}
	return it.Title
}

// EnrichedItem is a single enriched item in the output.
type EnrichedItem struct {
	Name           string    `json:"name"`
	URL            string    `json:"url,omitempty"`
	Source         string    `json:"source,omitempty"`
	Topic          string    `json:"topic,omitempty"`
	Status         string    `json:"status,omitempty"`
	Content        string    `json:"content,omitempty"`
	Image          *string   `json:"image,omitempty"`
	SearxngContext string    `json:"searxng_context,omitempty"`
	SearxngSources []string  `json:"searxng_sources,omitempty"`
	Facts          ItemFacts `json:"facts"`
}

// ItemFacts holds structured fact extraction results.
type ItemFacts struct {
	PlaceName *string `json:"place_name,omitempty"`
	PlaceType *string `json:"place_type,omitempty"`
	Address   *string `json:"address,omitempty"`
	EventDate *string `json:"event_date,omitempty"`
	Price     *string `json:"price,omitempty"`
	Website   *string `json:"website,omitempty"`
	Phone     *string `json:"phone,omitempty"`
	Hours     *string `json:"hours,omitempty"`
}

// EnrichOutput is the typed output for wp_enrich.
type EnrichOutput struct {
	Items []*EnrichedItem `json:"items"`
	Total int             `json:"total"`
}

var (
	enricher     *enriche.Enricher
	enricherOnce sync.Once
)

func getEnricher() *enriche.Enricher {
	enricherOnce.Do(func() {
		var opts []enriche.Option

		// Use stealth client if available.
		if sc := engine.StealthClient(); sc != nil {
			opts = append(opts, enriche.WithStealth(sc))
		}

		// Use SearXNG if configured.
		if engine.Cfg.SearxngURL != "" {
			provider := search.NewSearXNG(engine.Cfg.SearxngURL)
			opts = append(opts, enriche.WithSearch(provider))
		}

		enricher = enriche.New(opts...)
	})
	return enricher
}

func modeToEnriche(mode string) enriche.Mode {
	switch mode {
	case enrichModePlaces:
		return enriche.ModePlaces
	case enrichModeEvents:
		return enriche.ModeEvents
	default:
		return enriche.ModeNews
	}
}

func registerEnrich(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "wp_enrich",
		Description: "Enriches items by fetching page content, extracting facts (JSON-LD, regex), og:image, and SearXNG context. " +
			"Modes: news (default) — news articles; places — venues/businesses with status detection; events — time-bound events.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input EnrichInput) (*mcp.CallToolResult, *EnrichOutput, error) {
		engine.IncrToolCall()

		if input.Data == "" {
			return nil, nil, errors.New("data is required")
		}

		mode := input.Mode
		if mode == "" {
			mode = enrichModeNews
		}

		var items []enrichInputItem
		if err := json.Unmarshal([]byte(input.Data), &items); err != nil {
			return nil, nil, fmt.Errorf("parse input data: %w", err)
		}

		results := make([]*EnrichedItem, len(items))
		var wg sync.WaitGroup
		for i, item := range items {
			wg.Add(1)
			go func(idx int, it enrichInputItem) {
				defer wg.Done()
				defer toolutil.RecoverLog("enrich")
				results[idx] = enrichSingle(ctx, mode, it)
			}(i, item)
		}
		wg.Wait()

		return nil, &EnrichOutput{Items: results, Total: len(results)}, nil
	})
}

func enrichSingle(ctx context.Context, mode string, item enrichInputItem) *EnrichedItem {
	name := item.effectiveName()
	result := &EnrichedItem{
		Name:   name,
		URL:    item.URL,
		Source: item.Source,
		Topic:  item.Topic,
	}

	// L1 research cache: short-circuit for places mode if fresh data exists.
	if mode == enrichModePlaces && researchStore != nil && item.City != "" {
		cached, err := researchStore.GetPlaceResearch(ctx, name, item.City)
		if err == nil && cached != nil {
			return researchToEnrichedItem(cached)
		}
	}

	// Call go-enriche for the heavy lifting.
	enrichResult, err := getEnricher().Enrich(ctx, enriche.Item{
		Name:   name,
		URL:    item.URL,
		City:   item.City,
		Mode:   modeToEnriche(mode),
		Source: item.Source,
		Topic:  item.Topic,
	})
	if err != nil {
		slog.Debug("enrich: enriche.Enrich failed", "name", name, "err", err)
		return result
	}

	// Map enriche.Result → EnrichedItem.
	result.URL = enrichResult.URL
	result.Status = string(enrichResult.Status)
	result.Content = enrichResult.Content
	result.Image = enrichResult.Image
	result.SearxngContext = enrichResult.SearchContext
	result.SearxngSources = enrichResult.SearchSources
	result.Facts = factsToItemFacts(enrichResult.Facts)

	// go-wp specific: upgrade unreachable → website_down if search found context.
	if result.Status == string(enriche.StatusUnreachable) && result.SearxngContext != "" {
		result.Status = string(enriche.StatusWebsiteDown)
	}

	// L1 research cache: persist places enrichment result.
	if mode == enrichModePlaces && researchStore != nil && item.City != "" {
		pr := &research.PlaceResearch{
			Name:           result.Name,
			URL:            result.URL,
			Status:         result.Status,
			Content:        result.Content,
			Image:          result.Image,
			SearxngContext: result.SearxngContext,
			SearxngSources: result.SearxngSources,
			EnrichedAt:     time.Now().UTC(),
			Facts: &research.Facts{
				PlaceName: result.Facts.PlaceName,
				PlaceType: result.Facts.PlaceType,
				Address:   result.Facts.Address,
				EventDate: result.Facts.EventDate,
				Price:     result.Facts.Price,
				Website:   result.Facts.Website,
				Phone:     result.Facts.Phone,
				Hours:     result.Facts.Hours,
			},
		}
		_ = researchStore.SetPlaceResearch(ctx, name, item.City, pr)
	}

	return result
}

func factsToItemFacts(f enriche.Facts) ItemFacts {
	return ItemFacts{
		PlaceName: f.PlaceName,
		PlaceType: f.PlaceType,
		Address:   f.Address,
		EventDate: f.EventDate,
		Price:     f.Price,
		Website:   f.Website,
		Phone:     f.Phone,
		Hours:     f.Hours,
	}
}

func researchToEnrichedItem(cached *research.PlaceResearch) *EnrichedItem {
	out := &EnrichedItem{
		Name:           cached.Name,
		URL:            cached.URL,
		Status:         cached.Status,
		Content:        cached.Content,
		Image:          cached.Image,
		SearxngContext: cached.SearxngContext,
		SearxngSources: cached.SearxngSources,
	}
	if cached.Facts != nil {
		out.Facts = ItemFacts{
			PlaceName: cached.Facts.PlaceName,
			PlaceType: cached.Facts.PlaceType,
			Address:   cached.Facts.Address,
			EventDate: cached.Facts.EventDate,
			Price:     cached.Facts.Price,
			Website:   cached.Facts.Website,
			Phone:     cached.Facts.Phone,
			Hours:     cached.Facts.Hours,
		}
	}
	return out
}
```

**Step 2: Verify build**

Run: `cd /home/krolik/src/go-wp && go build ./...`
Expected: exit 0

---

### Task 3: Delete old extract and fetch files

**Files:**
- Delete: `/home/krolik/src/go-wp/internal/wpserver/tool_enrich_extract.go`
- Delete: `/home/krolik/src/go-wp/internal/wpserver/tool_enrich_fetch.go`

**Step 1: Delete files**

```bash
cd /home/krolik/src/go-wp
rm internal/wpserver/tool_enrich_extract.go internal/wpserver/tool_enrich_fetch.go
```

**Step 2: Verify build**

Run: `cd /home/krolik/src/go-wp && go build ./...`
Expected: exit 0 — no references remain to deleted functions.

**Step 3: Verify no broken references**

Run: `cd /home/krolik/src/go-wp && grep -rn 'extractArticleText\|extractOGImage\|extractFacts\|fetchPageWithStatus\|fetchPage\b\|applyLDJSON\|applyRegexFacts\|extractTagContent\|extractHost\|extractLDAddress\|extractLDPrice\|jsonString\|regexExtract\|normalizeURL' internal/wpserver/tool_enrich*.go || echo "clean"`
Expected: "clean" — all old functions removed from enrich files. (normalizeURL still exists in tool_discover_news.go which is fine.)

---

### Task 4: Update tests

**Files:**
- Modify: `/home/krolik/src/go-wp/internal/wpserver/tool_enrich_test.go`

The old tests tested `buildSearxngQuery()`, `extractHost()` — both now deleted. The `effectiveName()` tests remain valid. We need to:
1. Remove `TestBuildSearxngQuery_*` tests (5 tests — this logic moved to go-enriche)
2. Remove `TestExtractHost` test (this logic moved to go-enriche)
3. Keep `TestEffectiveName_*` tests (3 tests — still in go-wp)

**New `tool_enrich_test.go`:**

```go
package wpserver

import (
	"testing"
)

func TestEffectiveName_Name(t *testing.T) {
	it := enrichInputItem{Name: "Fun Jump"}
	if got := it.effectiveName(); got != "Fun Jump" {
		t.Errorf("got %q, want %q", got, "Fun Jump")
	}
}

func TestEffectiveName_TitleFallback(t *testing.T) {
	it := enrichInputItem{Title: "News Title"}
	if got := it.effectiveName(); got != "News Title" {
		t.Errorf("got %q, want %q", got, "News Title")
	}
}

func TestEffectiveName_NameOverTitle(t *testing.T) {
	it := enrichInputItem{Name: "Place", Title: "News"}
	if got := it.effectiveName(); got != "Place" {
		t.Errorf("got %q, want %q", got, "Place")
	}
}
```

**Step 2: Run tests**

Run: `cd /home/krolik/src/go-wp && go test ./internal/wpserver/ -run TestEffective -v`
Expected: 3/3 PASS

---

### Task 5: Lint and full verification

**Step 1: Run go mod tidy**

Run: `cd /home/krolik/src/go-wp && go mod tidy`
Expected: No errors. Old dependencies (if any) removed.

**Step 2: Run linter**

Run: `cd /home/krolik/src/go-wp && make lint`
Expected: 0 issues

**Step 3: Run full test suite**

Run: `cd /home/krolik/src/go-wp && go test ./... -count=1`
Expected: All tests pass

**Step 4: Commit**

```bash
cd /home/krolik/src/go-wp
git add -A
git commit -m "refactor: replace enrichment internals with go-enriche library

- tool_enrich.go: enrichSingle() now delegates to enriche.Enrich()
- Lazy Enricher initialization with stealth + SearXNG
- Delete tool_enrich_extract.go (259 lines) and tool_enrich_fetch.go (110 lines)
- Preserve: research store L1 cache, statusWebsiteDown upgrade, effectiveName()
- Remove tests for migrated functions (buildSearxngQuery, extractHost)
- Keep effectiveName() tests (go-wp-specific backward compat)"
```

---

### Task 6: Update go-enriche ROADMAP.md

**Files:**
- Modify: `/home/krolik/src/go-enriche/docs/ROADMAP.md`

**Step 1: Mark Phase 5 complete**

Update Phase 5 section to mark all items as done and add ✅.

**Step 2: Commit**

```bash
cd /home/krolik/src/go-enriche
git add docs/ROADMAP.md
git commit -m "docs: mark Phase 5 (Migration) complete in roadmap"
```
