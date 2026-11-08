# Contributing to go-enriche

Changes are welcome when they keep the exported API stable and are covered by tests.

## Development

Requires Go 1.26 and golangci-lint v2.12.2.

```sh
make test     # GOWORK=off go test -race -count=1 ./...
make lint     # GOWORK=off golangci-lint run ./...
make build    # GOWORK=off go build ./...
```

CI (`.github/workflows/ci.yml`) runs the tests with the race detector on Go 1.24 and 1.25, and
golangci-lint v2.12.2. If those pass locally they pass there. A pull request that changes only
documents skips them. The optional `.pre-commit-config.yaml` runs the same lint locally.

## Commits

Conventional Commits (`feat:`, `fix:`, `refactor:`, `deps:`, `chore:`, `docs:`, `ci:`) with the PR
number appended by the merge. There is no release-please: a maintainer tags `v1.x.y` by hand and
updates [CHANGELOG.md](CHANGELOG.md), so leave the changelog and tags alone.

## Pull requests

- Branch from `main`, keep the change focused, and fill in the pull-request template.
- Add tests for behaviour changes; extraction changes need a fixture or a table case.
- Update [docs/INDEX.md](docs/INDEX.md) when you add or rename a document.
- Do not add secrets, hostnames or personal data to any file; the repository is public.

## Reporting problems

Bugs and feature requests go through the issue templates. Report vulnerabilities privately as
described in [SECURITY.md](SECURITY.md).
