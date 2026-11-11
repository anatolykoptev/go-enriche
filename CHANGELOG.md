# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project uses semantic
versioning. Entries before v1.40 are summarised from the commit history.

## [Unreleased]

### Changed

- Licence changed from MIT to Apache-2.0 from this version on.

### Added
- Send the internal service secret to the ox-browser service through go-kit svcauth.

### Changed
- Docs-only pull requests skip the heavy CI jobs.
- Repository documents brought up to the fleet standard.

## [1.40.3] - 2026-07-18

### Changed
- Bump go-stealth to v1.19.1; scrub private-infrastructure references from the repository.

## [1.40.0] - 2026-07-09

### Added
- Classify phone role labels as general or departmental (`extract`).
- Recover sites with a TLS certificate mismatch through a guarded plain-HTTP fallback (`fetch`).

### Fixed
- Skip navigation boilerplate in the phone role-label scanner; read the role label from the
  narrowest live node.

## [1.32.0] - 2026-07-03

### Added
- `OnPhaseTiming` hook for per-phase enrichment latency; `SkipMapsCheck` to suppress the maps
  closure check; `WithRenderSkipDisabled` kill switch; `WithFollowRedirects` and `WithUserAgent`
  fetcher options.
- Render skip when the raw fetch already carries a trustworthy anchored site number.
- Site-number set sidecar (`SiteNumbers`) with city membership tags, branch-locator JSON and
  schema.org multi-location phone extraction.

## [1.20.0] - 2026-06-17

### Added
- Source-priority resolver (official site beats maps beats search), persisted per-field provenance
  with an `operator_verified` tier, DNI/call-tracking detection, JS render for sites without contact
  facts, contacts-subpage discovery, legal versus venue address split.
- SSRF guard for caller-supplied fetch targets, later routed onto go-kit.

## [1.10.0] - 2026-06-01

### Added
- 2GIS and DaData geocoders, address-anchored map lookups, source-provided coordinate seeding,
  resilient circuit-breaker decorator for map checkers.

## [1.0.0] - 2026-03-05

### Added
- First public release: `extract`, `structured`, `fetch`, `search`, `cache` packages, the `Enricher`
  with `Enrich` and `EnrichBatch`, functional options, multi-provider search fallback.

[Unreleased]: https://github.com/anatolykoptev/go-enriche/compare/v1.40.3...HEAD
