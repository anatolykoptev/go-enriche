# CLAUDE.md

Notes for coding agents working in go-enriche: a Go library (module
`github.com/anatolykoptev/go-enriche`) that enriches places, news and events from the web and
returns facts with per-field provenance. Several services import it, so the exported API and the
cached `Result` shape are contracts. Every document is indexed in [docs/INDEX.md](docs/INDEX.md).

## Layout

The root package holds the `Enricher` (`enriche.go`, `enriche_fetch.go`, `enriche_contacts.go`,
`enriche_search_fetch.go`), the fact resolver (`resolve.go`), options, metrics and types. Sub-packages:
`extract`, `structured`, `fetch`, `search`, `maps`, `cache`, `news`. Tests sit beside the code;
`extract/testdata` holds fixtures.

## Gate

CI (`.github/workflows/ci.yml`, `GOWORK=off`) runs:

```sh
go test -race -count=1 -coverprofile=coverage.out ./...   # matrix: Go 1.24 and 1.25
golangci-lint run                                          # golangci-lint v2.12.2, Go 1.25
```

`make test`, `make lint` and `make build` run the same through `GOWORK=off`. A docs-only PR
(Markdown, `docs/`, licence, templates) skips both jobs through the `docs-scope` job in the same
workflow; that block is canonical across the fleet and compared byte for byte, so do not edit it
here. There is no release automation: a release is a hand-pushed `v1.x.y` tag, and
[CHANGELOG.md](CHANGELOG.md) is edited by hand.

## Invariants that are easy to break

- **Official site beats maps beats search.** All contact facts go through the single resolver in
  `resolve.go` (`official_site > aggregator > maps > search`); do not add a second writer to
  `Result.Facts`. The official site is always fetched, and a reachable site refutes a maps-only
  "closed" status.
- **A call-tracking (DNI) number is omitted, never shipped.** A poison verdict outranks maps and
  search and must propagate to `Result.Facts` and `Provenance`. Phone selection is fail-closed.
- **Provenance and `operator_verified`** ride beside the plain `Facts` fields so consumers can
  protect operator-confirmed values on re-enrich. Do not change `Facts` field types.
- **The cache key must cover every input that changes the result** (`cacheKey` folds in
  `SkipMapsCheck`). A new `Item` field that affects output needs to join the key, and a change to
  the cached `Result` shape needs a bump of `cacheSchemaVersion`.
- **Every outbound fetch passes the SSRF guard** (`WithTargetGuard`, default go-kit
  `httputil.CheckRawURL`), including targets handed to ox-browser or `WithBrowserFetch`.
- **The render-skip kill switch** (`WithRenderSkipDisabled`) must keep restoring the pre-skip
  behaviour; a wrong skip writes data that a revert cannot undo.
- **Dependencies are not vendored** here (`/vendor/` is ignored); always use `GOWORK=off`.
- **This repository is public.** No hostnames, operator paths, account names, tokens or personal
  data in any tracked file, tests and fixtures included.
