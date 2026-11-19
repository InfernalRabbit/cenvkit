**English** · [Русский](docs/ru/CONTRIBUTING.md)

# Contributing to cenvkit

Thanks for taking the time to contribute. cenvkit is a small Go CLI that
assembles a project env chain for Docker Compose and debugs the gap between
`env_file:` values and compose-time ${VAR} interpolation.

- Command and behavior reference: [`docs/cenvkit.md`](docs/cenvkit.md)
- Full user guide: [`docs/guide.md`](docs/guide.md)
- Architecture and semantic boundaries: [`AGENTS.md`](AGENTS.md) — read this
  before changing code.

## Before you start

- Search existing issues and the documentation before opening a new one.
- For a large change (a new command or flag, a behavior contract, a dependency
  bump), open an issue or a draft PR before writing a lot of code.
- **One logical change = one PR.** Do not mix a refactor, a feature, and a
  formatting pass in the same pull request.
- Security issues do not belong in a public issue — see [`SECURITY.md`](SECURITY.md).
- Participation is covered by the [`CODE_OF_CONDUCT.md`](CODE_OF_CONDUCT.md).

## Requirements

- **Go** — the exact version declared in [`go.mod`](go.mod) and pinned as
  `GO_VERSION` in [`.github/workflows/ci.yml`](.github/workflows/ci.yml); do not
  bump the `go` directive incidentally.
- **POSIX `sh`** — the tracked `cenvkit` launcher and every `scripts/ci/*.sh`
  script are POSIX sh, not bash. Keep them `sh -n`-clean.
- **Docker Compose** — only for the compose-touching commands and the acceptance
  suite (`cenvkit compose`, `cenvkit validate`, `go test ./test/...`). The
  acceptance tests are config-only and need no daemon, but they do probe
  `docker compose version`.
- **golangci-lint v2.12.2**, built with the project's Go (see below). A prebuilt
  binary compiled with an older Go refuses this module.
- No Python, no Node.

## Local development

```sh
git clone https://github.com/InfernalRabbit/cenvkit.git
cd cenvkit

go build ./...
go run ./cmd/cenvkit version
make dev-build          # local binary at .cenvkit.bin (gitignored)
```

Run the CLI from source with `go run ./cmd/cenvkit <args>` or as
`./.cenvkit.bin <args>`. The tracked root file `cenvkit` is the POSIX-sh
launcher — never overwrite it with a compiled binary.

## The local gate

A change is ready when these pass on the final tree. Run the formatting check
**after** your last edit — `go test` does not check formatting:

```sh
gofmt -l .                  # must print nothing
go vet ./...
make seam                   # only internal/engine may import compose-go
go test ./... -count=1
make ci                     # vet + fmt-check + seam + build + test + lint (if installed)
```

`-count=1` is deliberate: it disables the test result cache, so a previous
`ok` cannot stand in for an actual run. `make ci` runs `golangci-lint` only
when the binary is on `PATH`; a "green" run without it is not a full lint pass.

Lint locally the way CI does it — with the linter built by the project's Go and
`GOTOOLCHAIN` pinned to the `go.mod` version:

```sh
go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2
GOTOOLCHAIN=go$(go list -m -f '{{.GoVersion}}') golangci-lint run ./...
```

`make lint` performs the same pinning (it reads the version from `go.mod`).
Pinning matters in both directions: an older-Go linter cannot lint this module,
and a linter built for the project's Go cannot typecheck a newer stdlib.

Docker-free suite (docker-gated assertions skip themselves):

```sh
SMOKE_SKIP_DOCKER=1 go test ./... -count=1
```

### Acceptance suite (required for compose-related changes)

Changes touching `internal/engine`, chain assembly for `compose`/`env-files`, or
`include:`/`env_file:` handling also need the acceptance suite, with Docker
Compose available and `SMOKE_SKIP_DOCKER` unset:

```sh
docker compose version
go test ./test/... -v -count=1
```

A skipped Docker assertion does not establish Compose parity. CI runs this
suite against a **pinned** Compose release (version and sha256 in
[`.github/workflows/ci.yml`](.github/workflows/ci.yml)); do not assume the
runner's bundled version is used.

The acceptance `TestMain` builds its own temporary CLI, so no prebuilt binary is
needed; fixtures copy [`examples/monorepo/`](examples/monorepo/) and seed dot-env
files from `example.*`. If you change the counted assertions, update
`declaredAssertions` and the "Current assertion count" line-2 header in
[`test/cenvkit-acceptance_test.go`](test/cenvkit-acceptance_test.go) together.

When you touch the launcher, the installer, or another `scripts/ci/*.sh` script,
keep the shell gate green:

```sh
for f in cenvkit scripts/ci/*.sh; do sh -n "$f"; done
sh scripts/ci/cenvkit-install-selftest.sh
```

Release-configuration changes (`.goreleaser.yaml`, release jobs in
[`.github/workflows/ci.yml`](.github/workflows/ci.yml)) also need
`goreleaser check`; it is a separate CI job.

## Commit messages

Use [Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/):

```text
<type>(<optional scope>): <summary>
```

- Types: `feat`, `fix`, `docs`, `test`, `refactor`, `perf`, `build`, `ci`,
  `chore`.
- Scopes where useful: `chain`, `engine`, `envfiles`, `provenance`, `bootstrap`,
  `cli`, `docs`, `ci`.
- Summary in the imperative mood, lower-case, no trailing period.
- Mark breaking changes with `!` and explain them in a `BREAKING CHANGE:` footer.
- Use the body to explain **why**; the diff already shows **what**.

Examples (format only):

```text
feat(cli): add --json to the example command
fix(engine): keep service env_file paths out of the run chain
docs: clarify the interpolation/runtime split
test: cover an empty named chain
```

## Pull requests

1. Fork the repository and branch from `main`; keep the branch focused.
2. Make the change and run the local gate above.
3. Fill in [`.github/PULL_REQUEST_TEMPLATE.md`](.github/PULL_REQUEST_TEMPLATE.md).
4. A PR that changes behavior includes:
   - a test that fails before the change and passes after it (a unit test and/or
     an acceptance assertion);
   - a `CHANGELOG.md` entry under `[Unreleased]` (`Added` / `Changed` /
     `Fixed` / `Removed`);
   - documentation updates in `docs/cenvkit.md` and/or `docs/guide.md` when a
     command, flag, exit code, or behavior contract changes.
5. Compose-related PRs: state which acceptance command you ran and paste the
   result.
6. CI must be green. The docker acceptance job runs against a pinned Compose
   release; the non-blocking `compose-latest` job only reports upstream drift.
7. Do not include secrets, `.secrets.env` values, tokens, or real hostnames; use
   dummy values in fixtures.

A maintainer may ask for changes, ask you to split a mixed PR, or squash the
branch on merge. Please keep review threads addressed or resolved.

## Documentation and translations

- [`docs/cenvkit.md`](docs/cenvkit.md) is the one-page command and behavior
  reference; [`docs/guide.md`](docs/guide.md) is the full user guide.
- [`docs/agent-notes/`](docs/agent-notes/) holds durable implementation notes
  (compose-go facts, env semantics, testing/CI gotchas, known traps). They are
  notes, not a specification — verify claims against the current code and tests.
- [`docs/integrations/gitlab-offline.md`](docs/integrations/gitlab-offline.md)
  covers offline CI usage.
- Russian mirrors live in `docs/ru/`; the community documents are
  [CONTRIBUTING](docs/ru/CONTRIBUTING.md), [SECURITY](docs/ru/SECURITY.md), and
  [CODE_OF_CONDUCT](docs/ru/CODE_OF_CONDUCT.md).
  Keep the language switch on line 1, keep the section order and headings
  aligned with the English file, and copy commands, flags, paths, variable
  names, exit codes, and code blocks byte-for-byte instead of translating them.

## Issues and security reports

- Bug: use the "Bug report" form
  ([`.github/ISSUE_TEMPLATE/bug_report.yml`](.github/ISSUE_TEMPLATE/bug_report.yml)).
  Include `cenvkit version`, OS/arch, the exact invocation, expected vs actual
  behavior, and output captured with `--color=never`.
- Feature: use the "Feature request" form
  ([`.github/ISSUE_TEMPLATE/feature_request.yml`](.github/ISSUE_TEMPLATE/feature_request.yml)).
- Vulnerability: report it privately, never in a public issue — see
  [`SECURITY.md`](SECURITY.md).

## License

By contributing, you agree that your contributions are licensed under the
[MIT License](LICENSE), the same license as the project.
