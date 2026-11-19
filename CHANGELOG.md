# Changelog

All notable changes to cenvkit are documented here. This project adheres to
[Semantic Versioning](https://semver.org/spec/v2.0.0.html) and to
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

## [0.9.2] — 2026-11-19

### Documentation

- Docs brought up to the 0.9 contract: `docs/guide.md` §7 rewritten around the
  cascading `--show` views, filters, `--redact` and `--show deps` (EN + RU);
  README debug sections and the monorepo blueprint use the new grammar; example
  fixtures/comments and `AGENTS.md` mention the dependency view.

## [0.9.1] — 2026-11-19

### Changed

- **Help text brought up to date.** Every command now documents its current
  contract (Long description + runnable examples): the cascading `--show` views
  including `deps`, the filter matrix, defaults, conflicts and exit codes; the
  `compose` passthrough rules (wrapper flags are extracted only before `--`);
  `gap-report` exit codes; `run` exit codes; `env` formats; `init` symlink
  refusal. `cenvkit help compose` shows cenvkit's own text where
  `cenvkit compose --help` intentionally shows docker's.

## [0.9.0] — 2026-11-19

### Added

- **`--show deps --var X`** — the dependency closure: where `X` is defined (chain
  files and service `env_file:`), what `X`'s own value needs (transitive forward
  references), which keys reference `X` (transitive reverse references) and which
  services receive it. `--json --show deps` emits a `deps` section; the view
  requires `--var` and is not narrowed by `--service`.
- The engine now captures interpolation edges (`Report.Refs`) from every raw layer
  value: `${VAR}`, `${VAR:-def}`, `$VAR`, nested defaults (`${A:-${B}}`) and
  `$$` escapes. An unterminated reference stays literal, so no edge is invented.

## [0.8.1] — 2026-11-19

### Fixed

- A `--var` / `--service` filter that matches nothing in a **view** now exits **1**
  with a message on stderr and empty stdout; it used to exit 0, which read as
  success in CI. Queries keep exit 0: `--value` prints an empty value and
  `--trace` answers `not set` about an unset variable.
- `--redact` no longer hides the `CENVKIT_ENV` header: the tier is metadata, not a
  secret.

## [0.8.0] — 2026-11-19

### Changed (breaking)

- **`env-debug` views cascade.** `--show` is repeatable and renders `list`,
  `files`, `overview`, `effective`, `trace` in the order given, instead of the
  first flag winning. No `--show` keeps the previous default (the Layer-1 list).
  The single-view flags (`--list`, `--files`, `--overview`, `--effective`,
  `--trace`) still work as aliases and warn on the human path only.
- **Filters apply everywhere the data exists.** `--var KEY` narrows `list`,
  `files`, `overview`, `effective`, `trace` and `--json`; `--service NAME`
  narrows the per-service views and the JSON service blocks.
- **Impossible combinations exit 2** with an explanation instead of silently
  falling back: `--value` with another view or `--json`, `--trace`/`--show trace`
  without `--var`, `--service` with a view set that has no per-service data.

### Added

- `--redact`: values become `<redacted>`, keys/files/layers stay visible.
- `--json --show …` emits only the requested sections (files, services, vars,
  layers); `--json` alone keeps the historical whole-report document.
- `--trace --var KEY` explains an `env_file`-only key
  (`runtime-only: <file> (env_file)`) instead of printing `not set`.
- A stderr warning when the removed `COMPOSE_ENV` selector is set.
- Parameter interaction matrices in `docs/cenvkit.md` and `docs/ru/cenvkit.md`.

## [0.7.0] — 2026-11-19

### Added

- **`--env-file`** on `env-debug`, `gap-report`, `env` and `run`: layer an extra
  env file **after** the project chain, mirroring `docker compose --env-file`.
  A variable supplied only by that flag no longer looks like a gap, and the file
  participates in interpolation exactly where Compose would apply it. A missing
  file is an error, not a silent skip (Compose errors too). `cenvkit compose`
  is unchanged: it still forwards docker's own `--env-file` untouched.
- Bilingual documentation: the English docs are canonical and Russian copies live
  under [`docs/ru/`](docs/ru/) (`cenvkit.md`, `guide.md`, integrations, agent
  notes), plus [`README.ru.md`](README.ru.md). Both variants carry a language
  switcher.
- Community files: `CODE_OF_CONDUCT.md`, `CONTRIBUTING.md`, `SECURITY.md`, three
  issue templates and a pull-request template.

### Changed

- compose-go **v2.11.0 → v2.17.0**, which requires Go ≥ 1.26.11 (`go.mod` and the
  CI pin follow). The full suite and the Docker acceptance path pass on the new
  version; `govulncheck` reports no reachable vulnerabilities (the logrus bump
  clears GO-2025-4188).
- `env --format dotenv` escapes carriage returns as `\r`; compose-go decodes that
  back to CR, so emitted lines never carry raw control bytes.
- `THIRD_PARTY_NOTICES.md` regenerated for the new dependency set (28 modules:
  8 Apache-2.0, 5 BSD-3-Clause, 15 MIT).
- CI's non-blocking `compose-latest` job now publishes a pinned-vs-latest drift
  table into the run summary and raises an annotation when upstream has moved on,
  instead of failing invisibly.

## [0.6.2] — 2026-11-19

Integration release: the offline/CI material that was previously handed out as
one-off snippets now ships with the tool.

### Added

- `scripts/ci/cenvkit-install.sh` — install a **pinned** cenvkit and verify it:
  `CENVKIT_VERSION` + `CENVKIT_SHA256` + `CENVKIT_MIRROR` (layout
  `<MIRROR>/<version>/<asset>`), optional `CENVKIT_MIRROR_AUTH` header for private
  registries, and `CENVKIT_LOCAL_BIN` for air-gapped runners that ship the archive
  in the image. Fails closed on checksum mismatch.
- `scripts/ci/cenvkit-publish.sh` — mirror a release into a GitLab Generic Package
  Registry (works for any store with the same layout): verifies the local
  `checksums.txt`, uploads every archive plus the manifest, then proves each file
  is retrievable.
- `scripts/ci/cenvkit-install-selftest.sh` — offline self-test: builds the CLI,
  packages a release-shaped archive, serves it over `file://`, and asserts the
  installer accepts a matching sha256 and rejects a tampered one. Runs in CI.
- `docs/integrations/gitlab-offline.md` — the full recipe: mirror layout, publish
  and gate jobs, runner-architecture pins, the 401/403/404 matrix for the first
  publish, and secrets hygiene when materialising a `.secrets.env` from a CI
  `File` variable.
- `docs/agent-notes/named-chains.md` — named chains in depth: standalone sections,
  `last-wins`, empty-section behaviour (`COMPOSE_DISABLE_ENV_FILE=1`), tokens
  inside sections, and the precedence trap where a committed template can
  override a secret.

### Changed

- CI gained a shell gate: `sh -n` over the launcher and `scripts/ci/*`, plus the
  installer self-test on every push.
- README documents named chains and how to replace a compiled-env wrapper
  (`compile-env.sh` + `docker compose --env-file`) with
  `eval "$(cenvkit env --format shell)"` + `cenvkit compose`.
- `docs/cenvkit.md` states an explicit contract: `docker compose --env-file` is not
  modelled by `env-debug`/`gap-report`; list that file in `.cenvkit.envchain` if the
  diagnostics must see it.

## [0.6.1] — 2026-11-19

Hardening and release-engineering pass; no behavior change beyond the exit codes
and signal handling called out below.

### Fixed

- **`compose` forwards terminating signals** (INT/TERM/HUP/QUIT) to the child, so
  `docker compose` no longer survives as an orphan when cenvkit is stopped by a
  supervisor or a CI timeout. A signal-terminated child now maps to **128+signo**
  (it used to report 255 via `os.Exit(ExitCode())`, and skipped deferred cleanup).
- **`validate` propagates docker's exit code** instead of collapsing every failure
  to 1, and `--all` now annotates both tiers consistently (`dev …`/`prod …`).
- **`env --format shell` is atomic**: every key is validated before the first
  `export` line is written, so a rejected key can no longer leave a partial block
  on stdout for a caller to `eval`.
- **Test harness retries a transient build once** (cold module cache + parallel
  package builds), removing the flake that could fail the acceptance package and
  the source-launcher regression test on a first run.

### Changed

- **CI pins Docker Compose** (`v5.8.0` + sha256, verified before install) instead of
  downloading `releases/latest`; drift against upstream is still reported by a
  non-blocking `compose-latest` job.
- **CI builds the release before it is tagged**: a `release-snapshot` job runs the
  full goreleaser pipeline (all 6 targets + archives + checksums) on every CI run.
- Workflows gained `timeout-minutes` and `concurrency`; every `go test` in CI and in
  the Makefile carries `-count=1` so a cached "ok" cannot stand in for a run.
- Release archives now ship `LICENSE`, `README.md`, `CHANGELOG.md` and
  `THIRD_PARTY_NOTICES.md` (new file with the Apache-2.0 §4(d) NOTICE texts).
- Release notes: scoped conventional-commit subjects (`chore(release):`,
  `ci(github):`, …) are now filtered out; the list is newest-first.
- Docs: `env`/`run --print`/`env-debug --value|--trace` are documented as printing
  values verbatim (secrets included), and `.cenvkit.envchain` is documented as
  trusted project config whose entries may leave the project directory.

## [0.6.0] — 2026-11-19

First release published as **cenvkit** (the module was previously named
`compose-envkit`; earlier release notes lived in the previous repository).

### Fixed

- **Selector parity (H).** The resolved `CENVKIT_ENV` (shell > `.env` > `dev`) is
  now exported into the `docker compose` child environment and into the in-process
  debug/gap engine. A documented `COMPOSE_FILE=...${CENVKIT_ENV}...` therefore
  expands to the effective tier; previously it expanded to an empty string
  (`compose..yml`) whenever the selector fell back to its `dev` default, making
  `compose`, `env-debug` and `gap-report` fail on such projects.
- **`cenvkit init` no longer follows symlinks** (CWE-59). Only regular
  `example.*` files are seeded, and a pre-existing destination entry — including a
  dangling symlink — is never written through. Both directions could otherwise
  move data into the repository (`example.env` → credentials file) or write
  outside it (`.env` → dangling link).
- **`--no-expand` and `--overview` unquote correctly** when a quoted value is
  followed by an inline comment: `KEY="v" # note` now yields `v`, matching
  compose-go's parser instead of leaking the quote characters into the value.
- **Human output is sanitized.** Control characters (C0/C1, ESC, CR/LF) that come
  from env values, file paths or chain-section names can no longer drive the
  terminal through ANSI/OSC sequences; `--json` output is byte-for-byte unchanged.

### Changed

- Module path is now `github.com/InfernalRabbit/cenvkit`.
- `.gitignore` now covers non-dot env names (`secrets.env`, `.env.*`) and key
  material (`*.pem`, `*.key`, `id_rsa*`, …), not just dot-prefixed env files.
- Developer notes moved to `docs/agent-notes/`.

### Removed

- Historical design documents and the internal agent-team scaffolding, which were
  developer-only and not part of the shipped tool.
