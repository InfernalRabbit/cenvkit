**English** · [Русский](../ru/agent-notes/testing-and-ci.md)

# Tests, gates, and CI

## Local gate (DoD)

```sh
go build ./...
go vet ./...
gofmt -l .                                       # must be empty
go test ./... -count=1
SMOKE_SKIP_DOCKER=1 go test ./... -count=1       # docker-free subset
go test ./test/... -v -count=1                   # with docker compose available
golangci-lint run ./...                          # see "version traps"
goreleaser check                                 # when editing the release config
```

`make ci` (`Makefile`) = `vet fmt-check seam build test` (+lint if the binary is
found). At the same time `make lint`/`make ci` SILENTLY skip golangci-lint when
it is not in PATH — "fully green" on such a machine is not trustworthy.

**Green without the docker path does not count as complete:**
`SMOKE_SKIP_DOCKER=1` skips docker-gated assertions, and those check real parity
with compose. A skipped assertion proves nothing.

## gofmt — after the LAST edit

`go test` does not check formatting. The classic miss: the gate was run, one more
edit was made — and `gofmt -l .` is no longer empty. Before "green", rerun
`make fmt-check` (or `gofmt -l .`) on the final tree.

## Version traps

### golangci-lint

- Build the linter with the PROJECT Go:
  `go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2`.
  A prebuilt binary built with an older Go refuses to lint a module with a newer
  `go` in `go.mod` ("the Go language version used to build golangci-lint is
  lower than the targeted Go version"). CI does exactly `go install`.
- Locally the default `go` may be NEWER than the project one → the linter fails
  on the stdlib typecheck. Run with `GOTOOLCHAIN=go<version from go.mod>`
  (currently 1.26.11).
- The linter does NOT PRINT EVERYTHING: `max-same-issues`/`max-issues-per-linter`
  cut the list. "11 issues" may mean ~40 sites — drive it to a true `0`.
- v2-format config (`.golangci.yml`): `linters.enable` (errcheck, govet,
  ineffassign, staticcheck, unused) + `formatters.enable` (gofmt).
- A dirty cache produces false positives (for example, a batch of `SA5011` in
  tests). It is cured by `golangci-lint cache clean`, not by editing code.

### Docker Compose

- The CI engine is PINNED (`COMPOSE_VERSION` + sha256 in `ci.yml`); locally you run
  whatever the installation brought, and a non-blocking `compose-latest` job runs the
  same acceptance against upstream latest to report drift. Tolerance for `include:` +
  overriding an imported service differs: a tolerant engine merges silently, a
  strict one fails (`services.X conflicts with imported resource`).
- Consequence: a green local acceptance does not prove a green CI. For
  compose-related changes, look at `docker compose version` in the log and verify
  on a strict engine (the pinned container), not only on your own.
- The acceptance tests are config-only (no daemon needed), but
  `dockerAvailable()` requires a successful `docker compose version`.

## `go test` cache

Go reuses the `go test` result cache when the inputs have not changed, while CI
runs tests without `-count=1`. `dockerAvailable()` is an external command and is
not part of the cache key, so a cached PASS can hide that the docker-gated
assertions never ran at all. Locally, always `-count=1`.

## Acceptance harness (`test/`)

- `TestMain` builds ITS OWN binary (`go build -o <tmp>/cenvkit ../cmd/cenvkit`):
  a prebuilt one is not needed. The build goes into the shared cache, so on a cold
  machine a flake is possible under parallel `go test ./...`; it did not always
  reproduce — keep it in mind when diagnosing "blinking" failures.
- `stageMonorepo` copies `examples/monorepo` into `t.TempDir()` and seeds the root
  dotfiles from `example.*`. We never mutate the source fixture.
- `dockerAvailable()`: `SMOKE_SKIP_DOCKER=1` → false (short circuit), otherwise a
  one-time `docker compose version` probe (`sync.Once`). That way the suite skips
  correctly on a host without docker.
- Helpers: `runCenvkit` (CombinedOutput), `runCenvkitSplit` (stdout/stderr/exit
  separately — for negative checks), `envWithout(...)` (the process environment
  minus keys — for the child binary).

## Assertion counter

- `const declaredAssertions = <N> + reviewRegressionAssertions` is the single
  source of truth. The file header (line 2, "Current assertion count: N.") is
  checked by `TestAssertionCountHeader`: change one — change the other.
- ASSERTIONS are counted, not `func Test`: there are always fewer functions than
  assertions (one scenario yields several). That is not a discrepancy by itself.
- A historical disease — stale numbers in other comments of the same file. After
  an edit, search the whole file:
  `grep -nE '[0-9]+ (total|assertion|count)|count: [0-9]+' test/cenvkit-acceptance_test.go`
  and check each one (careful with `0o644` — it is not a counter).

## CI (`.github/workflows/ci.yml`)

| Job | What it does |
|---|---|
| `lint` | `go vet`, a `gofmt -l` gate, the compose-go seam check, `golangci-lint` via `go install`, `sh -n` over `cenvkit`+`scripts/ci/*.sh`, and the offline installer self-test |
| `goreleaser-check` | `goreleaser check` |
| `release-snapshot` | `goreleaser release --snapshot --clean --skip=publish` — all 6 targets + archives are built BEFORE the tag |
| `test` (ubuntu + macos) | `go build`, `go test ./... -count=1` with `SMOKE_SKIP_DOCKER=1` (== no-docker acceptance: `./...` includes `./test`) |
| `acceptance-docker` | installs the **pinned** Compose (`COMPOSE_VERSION` + sha256 from the env block), verifies `version --short`, `go test ./test/... -v -count=1` |
| `compose-latest` | optional (`continue-on-error`) drift detector: the same acceptance against upstream latest |

The `scripts/ci/` scripts: `cenvkit-install.sh` (version pin + sha256,
`CENVKIT_MIRROR` / `CENVKIT_MIRROR_AUTH`, `CENVKIT_LOCAL_BIN` for air-gap),
`cenvkit-publish.sh` (Generic Package Registry: verify `checksums.txt` → upload →
verify the download). `cenvkit-install-selftest.sh` builds a release-shaped
archive, serves it over a `file://` mirror and checks that a correct sha256
installs while a wrong one does not. It runs in CI on every push and needs no
network.

Seam check: `go list -f '{{.ImportPath}} {{join .Imports " "}}' ./...` WITHOUT
`-deps` (with `-deps` the left column also contains compose-go's own subpackages
→ a permanent false positive). The only exception is a line exactly
`<module>/internal/engine `; on a leak it `exit 1`. `make seam` mirrors this
check.

## Run traps

- A subprocess failed — print `CombinedOutput()`: the wrapped error "exit status 1"
  hides the real cause (that is how a compose failure was missed once).
- You are editing a test that someone is editing in parallel: re-read the CURRENT
  file and run `-count=1`, otherwise you fix against a stale assertion.
- A behavior contract changes — review ALL `*_test.go`, not just the named
  directory: package tests are often wider than the task suggests.
- Do not go "green" on `go test` without `gofmt` and without the docker path:
  both misses have happened before and both reached CI.
