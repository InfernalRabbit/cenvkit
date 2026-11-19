# Changelog

All notable changes to cenvkit are documented here. This project adheres to
[Semantic Versioning](https://semver.org/spec/v2.0.0.html) and to
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

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
