# Changelog

All notable changes to cenvkit are documented here. This project adheres to
[Semantic Versioning](https://semver.org/spec/v2.0.0.html) and to
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

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
