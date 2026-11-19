## Summary

<!-- What does this PR change, and why? Link the issue it closes. -->

Closes #

## Type of change

- [ ] Bug fix (non-breaking)
- [ ] New feature (non-breaking)
- [ ] Breaking change (CLI surface, behavior contract, or exit codes)
- [ ] Documentation
- [ ] CI, release, or tooling
- [ ] Refactor (no behavior change)

## Checklist

- [ ] One logical change; no unrelated edits are mixed in.
- [ ] `make fmt-check` passes (`gofmt -l .` prints nothing) on the final tree.
- [ ] `go test ./... -count=1` passes.
- [ ] `make ci` passes (or I explain below what could not run).
- [ ] `go vet ./...` and `make seam` pass.
- [ ] `golangci-lint run ./...` was run with the project's Go (v2.12.2), if installed locally.
- [ ] Tests were added or updated for every behavior change.
- [ ] `CHANGELOG.md` has an entry under `[Unreleased]` for user-visible changes.
- [ ] Documentation was updated when commands, flags, exit codes, or behavior contracts changed (`docs/cenvkit.md`, `docs/guide.md`, and the matching `docs/ru/` file when one exists).
- [ ] Compose-related change: `go test ./test/... -v -count=1` was run with Docker Compose available and `SMOKE_SKIP_DOCKER` unset (result below).
- [ ] Shell touched: `for f in cenvkit scripts/ci/*.sh; do sh -n "$f"; done` and `sh scripts/ci/cenvkit-install-selftest.sh` pass.
- [ ] No secrets, tokens, `.secrets.env` values, or real hostnames in the diff or fixtures.
- [ ] I read `AGENTS.md` and kept the documented behavior contracts.

## Verification

<!-- Commands you ran, their result, and the relevant versions (cenvkit version, go version, docker compose version --short). -->

```text

```
