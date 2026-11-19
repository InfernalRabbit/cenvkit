**English** · [Русский](ru/cenvkit.md)

# cenvkit — the Go CLI

`cenvkit` is a small Go CLI built on
**Docker's own compose loader** (`github.com/compose-spec/compose-go/v2`, pinned
`v2.17.0`). It assembles `COMPOSE_ENV_FILES` from your **project env chain** and
`exec`s `docker compose`, and ships a **daemon-free debugger** that explains where
every variable comes from — and flags the one place Compose silently bites you.

> This is the **one-page reference**. For the full end-to-end guide (setup,
> monorepos, CI, troubleshooting, worked examples), see [`guide.md`](guide.md).

## What it does, and the gap it surfaces

Docker Compose keeps two layers deliberately separate:

| Layer | Populates | Visible to compose-time `${VAR}` in the YAML? |
|---|---|---|
| Project env (`COMPOSE_ENV_FILES` / `--env-file` / `.env`) | the interpolation context | **yes** |
| A service's `env_file:` | the container's runtime env | **no** |

`cenvkit`:

1. **Assembles `COMPOSE_ENV_FILES` from your project env chain (Layer 1)** —
   tokenized, ordered, secrets-last — so `docker compose` interpolates `${VAR}`
   against it. This is the **run path** (`cenvkit compose`/`env-files`).
2. **Keeps a service `env_file:` runtime-only** — exactly Docker's native
   semantics, per-service, isolated. cenvkit does **not** fold it into
   interpolation.
3. **`env-debug` detects and explains the gap**: when a `${VAR}` in your YAML is
   satisfied *only* by a service `env_file:`, the real run falls back to the
   `:-default`. The debugger tells you exactly that — and how each service's
   container env actually resolves — entirely in-process (no daemon).

> **History.** Earlier cenvkit (and the legacy sh kit) folded every service
> `env_file:` into `COMPOSE_ENV_FILES` to *close* the gap
> ([docker/compose#3435](https://github.com/docker/compose/issues/3435)). That is
> **reversed** (Layer-2-debug-only, 2026-06-17): flattening every service's
> env_file into one global interpolation namespace collapsed shared keys (a
> `${PORT}` collision footgun) and was the wrong default. `env_file:` is now
> runtime-only; the gap is **surfaced** by the debugger, not silently patched at
> run time. To make a var feed interpolation, put it in the Layer-1 chain.

Built on the real loader (not the legacy `awk`/`sed`): no glob over-discovery (the
`include:` graph is authoritative), `${SVC_DIR}` and nested `${A:-${B:-c}}`
resolve, no `sed`-injection vector (pure Go strings; host/env tokens whitelisted
to `[A-Za-z0-9._-]`).

> The legacy POSIX-`sh` kit (`bin/`, `lib/`, `mk/`) has been **removed**.
> `cenvkit` is the only implementation.

## How it works

1. **Layer 1 — the chain** (`internal/chain`, pure Go). Reads `.cenvkit.envchain`
   (or the default `.env → .${CENVKIT_ENV}.env → .secrets.env`), substitutes
   `${ENV}`/`${CENVKIT_ENV}`/`${HOST}`/`${HOSTNAME}` (sanitized), keeps the files
   that exist. **This — and only this — is `COMPOSE_ENV_FILES` for the run**: the
   interpolation context.
2. **Service `env_file:` enumeration** (`internal/engine`, the only package
   importing compose-go). Loads the real project (`COMPOSE_FILE`, `include:`,
   profiles, seeded with Layer-1) and enumerates the active services' resolved
   `env_file:` paths. **This is used only by `env-debug`** (the gap-detector and
   the `--files` runtime-only view) — it is **not** appended to the run
   `COMPOSE_ENV_FILES`.

## Install

**Installed** (recommended):

```sh
go install github.com/InfernalRabbit/cenvkit/cmd/cenvkit@latest
# ephemeral, no install:
go run github.com/InfernalRabbit/cenvkit/cmd/cenvkit@latest <args>
```

**Vendored** (commit the Go module + the POSIX `cenvkit` shim; requires a Go
toolchain — no committed binaries, no network):

```sh
./cenvkit <args>          # temp build in the tool's module; runs in your current directory
```

For lower per-invocation latency in vendored mode, build once into a gitignored
local binary and run that:

```sh
go build -o .cenvkit.bin ./cmd/cenvkit   # .cenvkit.bin is gitignored
./.cenvkit.bin compose up
```

## Commands

| Command | What it does |
|---|---|
| `cenvkit compose <args>` | assemble the Layer-1 chain, `exec docker compose <args>` (the core) |
| `cenvkit env-files` | print the resolved `COMPOSE_ENV_FILES` (**Layer 1 only**), one path per line |
| `cenvkit env-debug [--show VIEW]… [--var K] [--service S] [--json] [--redact]` | daemon-free debugger / gap-detector; `--show` repeats and cascades |
| `cenvkit gap-report [--json]` | CI/pre-build lint: exit **1** if any `${VAR}` is satisfied only by a service `env_file:`, **0** clean, **2** if no compose file is found (daemon-free) |
| `cenvkit run [-e ENV] [--expand\|--no-expand] [--print] -- <cmd> [args]` | exec a process with the merged chain env (no docker); propagates the child's exit code |
| `cenvkit env [-e ENV] [--expand\|--no-expand] [--format dotenv\|json\|shell]` | emit the merged chain env (chain-derived keys, sorted) for CI / scripts / `eval` |
| `cenvkit validate [--all]` | `docker compose config -q`; `--all` validates dev AND prod |
| `cenvkit init` | seed `.X` from `example.X` (**no-clobber**), fan out one directory level |
| `cenvkit version` | print the version |

Global: `--project-dir <dir>` (default: current directory) · `--chain <name>` (a
named `[name]` section of `.cenvkit.envchain`; default = the header-less /
`[default]` list) · `--color auto|always|never` (default `auto`).

**Color.** Human output is colored when stdout is a terminal and plain when piped
/ redirected. `--color=never` disables, `--color=always` forces (even when piped).
`NO_COLOR` (disable) and `CLICOLOR_FORCE`/`FORCE_COLOR` (force) are honored; CI is
treated as non-TTY (plain). **`--json` output is never colored.**

`env-debug` is **daemon-free** — it loads the model in-process via compose-go, with
NO `docker compose` shell-out.

**Views cascade.** `--show` is repeatable and the sections render in the order you
give them (no `--show` => the Layer-1 list). Nothing is silently ignored: an
impossible combination exits 2 with a message naming what is missing.

| `--show …` | What it prints |
|---|---|
| `list` | Layer-1 chain files (the default) |
| `files` | two groups: interpolation (Layer 1) and runtime-only (service `env_file:` paths) |
| `overview` | per-file layering: `+` new / `~` override (`old -> new`) / `·` repeat, raw values, `⚠ gap:` lines |
| `effective` | each service final container env with the source of every value |
| `trace` | one variable story (requires `--var`) |

`--value --var V` is not a view: it prints the bare interpolation value and cannot
be combined with anything else.

**Filters work everywhere the data exists.**

| | `list` | `files` | `overview` | `effective` | `trace` | `json` |
|---|---|---|---|---|---|---|
| `--var KEY` | files that declare it | files that declare it | its lines only | its entry only | the variable | `vars` + entries |
| `--service S` | - | that service | that service | that service | its effects | that block |

Defaults: `--var V` alone implies `--show trace`; `--service S` alone implies
`--show effective`; `--json` alone keeps the historical whole-report document,
while `--json --show …` emits only the requested sections.

Conflicts (exit 2, never a silent fallback): `--value` with any other view or with
`--json`; `--trace` / `--show trace` without `--var`; `--service` with a view set
that carries no per-service data.

A filter that matches nothing in a **view** exits **1** with a message on stderr and
empty stdout — a typo must not read as success in CI. Queries are exempt: `--value`
prints an empty value and `--trace` answers `not set` for an unset variable, both
with exit 0. The deprecated single-view flags (`--list`,
`--files`, `--overview`, `--effective`, `--trace`) stay as aliases and warn on the
human path only, so machine output remains parseable.

`--redact` masks every value as `<redacted>` while keeping keys, files, layers and
the `CENVKIT_ENV` header (a tier is metadata, not a secret) — use it when pasting
output into tickets or chat.
`--trace` effects are reported for **service fields only** (`services.<name>.<field>`);
`${VAR}` in top-level `networks:`/`volumes:`/`x-*` is out of scope (the var still
appears in chain attribution if it is a chain key). The `--effective` view never
reports a resolution the real run won't produce. Provenance uses compose-go's own
`dotenv` + `template` packages (docker-compose parity).

## `run` / `env` / `gap-report` — deliver & lint

The same Layer-1 chain that feeds `compose` drives any consumer:

- **`run -- <cmd>`** flattens the chain to a merged `KEY=VALUE` env and execs the
  process with it — no docker. `--expand` (default) resolves `${VAR}` / `${VAR:-def}`
  via compose-go's dotenv engine (values match `docker compose`); `--no-expand` keeps
  them literal. The shell env wins over the chain (compose parity). Exit codes: the
  child's, or **127** missing binary / **126** non-executable / **128+signo** on a
  signal. `--print` dumps the env and skips the exec. `--` is required.
- **`env`** emits that same merged env (chain-derived keys only, sorted) as
  `--format dotenv` (default) / `json` / `shell` — e.g. `eval "$(cenvkit env --format shell)"`.
  `cenvkit env --expand` == `env-debug --effective` == `docker compose config` (one
  expansion engine).
- **`gap-report`** is the CI/pre-build lint over the same gap `env-debug` detects:
  exit **1** if any `${VAR}` is env_file-only (the var/service/field/fallback; `--json`
  for tooling), **0** clean, **2** if no compose file is found. Daemon-free — run it
  before `docker build`.

`-e <env>` overrides `CENVKIT_ENV` for one invocation.

**`--env-file <path>`** (repeatable; `env-debug`, `gap-report`, `env`, `run`) layers an
extra env file **after** the project chain, mirroring `docker compose --env-file`: the
file takes part in interpolation, so a variable it supplies is not reported as a gap,
and a missing file is an error (Compose errors too). `cenvkit compose` does not claim
the flag — it still forwards docker's own `--env-file` untouched.

> **These commands print values verbatim, including secrets.** `env`, `run --print`
> and `env-debug --value|--trace` are deliberately unredacted (the value *is* the
> answer), so do not redirect them into shared CI logs, tickets or chat. cenvkit
> does not manage, mask or encrypt secrets — see the note in Configuration.

- **`env_file:` is runtime-only.** A `${VAR}` defined *only* in a service
  `env_file:` is **not** resolved at the run — `docker compose` interpolates the
  `:-default` (native fallback). `env-debug --trace` flags it as a gap. To make a
  var feed `${VAR}` interpolation, put it in the **Layer-1 chain**.
- **Run path = Layer 1 only.** `COMPOSE_ENV_FILES` (what `env-files`/`compose`
  set) never contains a service `env_file:` path.
- **Missing `env_file:`.** A missing *required* `env_file:` is lenient at
  enumeration (so `env-debug` never aborts) and **upstream-fatal at the real
  `docker compose` run** (which re-enforces `required:`). cenvkit does not
  reimplement upstream's `required:` semantics.
- **Secrets-last.** A guarantee **within the Layer-1 chain** (`.secrets.env` is
  emitted after the other Layer-1 files; last-wins). Because a service `env_file:`
  no longer feeds interpolation, it can **no longer clobber a chain/secret var at
  interpolation time**. (At runtime each `env_file:` applies only to its own
  container, natively.)
- **`COMPOSE_FILE` overlays** like `docker-compose.yml:docker-compose.${CENVKIT_ENV}.yml`
  work: cenvkit interpolates `${CENVKIT_ENV}`/`${ENV}` and splits on
  `COMPOSE_PATH_SEPARATOR` (else the OS path-list separator; never `,`).
- **`COMPOSE_DEPTH`** is accepted-but-ignored (the `include:` graph makes
  depth-bounded glob discovery obsolete). **No over-discovery** — a compose file
  not in the `include:` graph / `COMPOSE_FILE` is not loaded; `.git` is irrelevant.
- **`docker compose --env-file` is modelled on request.** cenvkit reasons about
  `COMPOSE_ENV_FILES`, `COMPOSE_FILE` and the project chain; a file passed to docker
  via `--env-file` is unknown to the diagnostics **unless** the same path is given
  to `cenvkit … --env-file` (or listed in `.cenvkit.envchain`). `cenvkit compose`
  forwards docker's own flag untouched.

## Configuration

- `.cenvkit.envchain` — the Layer-1 chain (one path template per line; `#`
  comments and blank lines ignored). May carry optional **`[name]` sections**.
  Treat it as **trusted project config**, like a `Makefile`: entries may be
  absolute or leave the project directory (`../shared.env`), and nothing constrains
  them to the project root — the same latitude Compose gives `env_file:`. Running
  cenvkit on a clone you have not reviewed therefore lets that file read paths on
  your machine (their values are printed by `env`). Review it first, or pin
  `--project-dir` at a tree you control.
- **Named chains** — `--chain <name>` selects a `[name]` section (default = the
  header-less / `[default]` list). Standalone (no inheritance), orthogonal to
  `CENVKIT_ENV`; an unknown name exits 2 listing the available ones. An empty
  section yields an empty chain, which disables native `.env` fallback for
  `compose`/`validate`. Guide: [`agent-notes/named-chains.md`](agent-notes/named-chains.md).
- An empty selected chain disables native `.env` fallback for `compose`/`validate`;
  it does not inherit excluded files. Explicit Docker `--env-file` still works.
- `CENVKIT_ENV` — selects the env tier (shell > `.env`'s `CENVKIT_ENV=` > `dev`).
- `COMPOSE_FILE`, `COMPOSE_PROFILES`, `HOSTNAME`/`HOST` — honored as above.
- **Secrets are out of scope** — cenvkit never masks/encrypts/manages them;
  `.secrets.env` just loads last (last-wins), nothing written to disk. Wrap with an
  external manager: `sops exec-env -- cenvkit run -- <cmd>`.

## Architecture / contributing

- `cmd/cenvkit` — cobra CLI. `internal/chain` — Layer 1 (the run
  `COMPOSE_ENV_FILES`). `internal/engine` — service `env_file:` enumeration for
  `env-debug` (the **only** package importing compose-go; a CI seam check enforces
  this). `internal/envfiles` — merge/order/dedup. `internal/provenance` —
  env-debug model + human/JSON render incl. the gap-detector (pure Go; `engine`
  imports it). `internal/bootstrap` — `cenvkit init`.
- Upstream-first: compose-go is the source of truth for compose semantics; the
  version is pinned and bumped deliberately (re-run acceptance on a bump — see the
  spec §7 checklist).
- Acceptance is the ported `examples/monorepo` smoke suite (`test/`); the
  Layer-1/Layer-2 model it pins is described in
  [`docs/agent-notes/env-semantics.md`](agent-notes/env-semantics.md).
