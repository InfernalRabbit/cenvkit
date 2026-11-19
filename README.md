**English** · [Русский](README.ru.md)

# cenvkit

A small **Go CLI** (`cenvkit`) built on Docker's own `compose-go` loader. It does
two things:

- **Debugs the one gap native Compose leaves open.** A value defined only in a
  service `env_file:` is invisible to compile-time `${VAR}` interpolation, so
  `ports: "${APP_PORT:-3000}:80"` silently falls back to `3000`. `cenvkit env-debug`
  detects + explains it with provenance (**daemon-free**), and **`cenvkit gap-report`**
  is a CI/pre-build lint. No other tool does this. (See
  [The gap](#the-gap-native-compose-doesnt-close).)
- **Populates the env-chain for any consumer.** `cenvkit compose` (→ `docker
  compose`), **`cenvkit run -- <cmd>`** (any process, no docker), **`cenvkit env`**
  (emit dotenv/json/shell) — one chain, one source of truth for compose *and* local
  dev. It composes with make/just; it doesn't replace them.

**`cenvkit` is a Go CLI** built on Docker's own compose loader
(`compose-spec/compose-go`). It is the only implementation — the original
POSIX-`sh` engine has been removed. The Go CLI needs only a Go toolchain (no
Python, no Node).

---

## Install — the Go CLI

`cenvkit` is built on Docker's own loader (`compose-spec/compose-go`, pinned
v2.17.0). `env-debug`/`gap-report` are daemon-free; `cenvkit compose`/`validate`
shell out to your **Docker Compose v2.24+** (for `include:` + `env_file:
required:`) and are tested in CI against a **pinned** Compose release (a separate,
non-blocking job reports drift against upstream latest). Two distribution modes:

```sh
# Installed (recommended)
go install github.com/InfernalRabbit/cenvkit/cmd/cenvkit@latest
# or ephemeral: go run github.com/InfernalRabbit/cenvkit/cmd/cenvkit@latest <args>
```

In your project:

```sh
cenvkit init               # seed .X from example.X (no-clobber), fan out one level
cenvkit gap-report         # CI lint: exit 1 if a ${VAR} is satisfied only by env_file:
cenvkit env-debug          # inspect the chain / provenance (daemon-free)
cenvkit compose config     # render config (interpolation = your Layer-1 chain)
cenvkit run -- npm test    # run any command with the merged chain env (no docker)
cenvkit env --format shell # emit the merged env (eval-able)
```

Or **vendor** it: commit the Go module + the POSIX `cenvkit` shim and run
`./cenvkit <args>` (needs a Go toolchain; for speed, `go build -o .cenvkit.bin
./cmd/cenvkit` — gitignored — and run that). Full command + behavior reference:
**[`docs/cenvkit.md`](docs/cenvkit.md)**.

---

## What it is, and why

Modern Docker Compose already handles most of what a hand-rolled wrapper used to
do — `COMPOSE_ENV_FILES` (last-wins project chain), `COMPOSE_FILE` overlays,
`DOCKER_DEFAULT_PLATFORM`. cenvkit wraps those for ergonomics, but it
exists for the **one** job that has no native equivalent.

### The gap native compose doesn't close

Docker Compose keeps two things deliberately separate:

| Layer | Populates | Used for compose-time `${VAR}` in the YAML? |
|---|---|---|
| Project-level env (`--env-file` / `COMPOSE_ENV_FILES`) | interpolation context | **yes** |
| A service's `env_file:` | the container's runtime env | **no** |

So if `APP_PORT` lives only in a service's `env_file:` and you write
`ports: "${APP_PORT:-3000}:80"`, compose interpolates `${APP_PORT}` **before**
that `env_file:` is ever read — you silently get the `:-3000` fallback. This is
an intentional, long-standing design split upstream
([docker/compose#3435](https://github.com/docker/compose/issues/3435), open
since 2016).

`cenvkit` does **not** paper over this by folding service `env_file:`s into
interpolation — doing so flattens every service's env into one global namespace (a
`${PORT}` collision footgun, since Compose interpolates the whole YAML against one
env map). Instead it keeps `env_file:` **runtime-only** and gives you a daemon-free
**gap-detector**: `env-debug` tells you when a `${VAR}` is satisfied only by a
service `env_file:` (so the run falls back), shows the runtime value, and
recommends the fix — **put values you reference as `${VAR}` into the Layer-1
chain.** Full reference: [`docs/cenvkit.md`](docs/cenvkit.md).

```sh
cenvkit env-files                          # the resolved COMPOSE_ENV_FILES (Layer-1 chain)
cenvkit env-debug --var APP_PORT   # in the chain, or an env_file gap?
cenvkit gap-report                         # CI/pre-build lint: non-zero if any ${VAR} is env_file-only
```

---

## Deliver the chain anywhere — `run` / `env`

The same Layer-1 chain that feeds compose can drive **any** consumer, so you keep
one source of truth instead of two tools:

```sh
cenvkit run -- npm run dev             # exec a process with the merged chain env (no docker)
cenvkit run --no-expand -- printenv    # leave ${VAR} literal
cenvkit env                            # emit the merged env (dotenv); --format json|shell
eval "$(cenvkit env --format shell)"   # load it into the current shell
```

`run` / `env` expand `${VAR}` / `${VAR:-def}` via compose-go's own dotenv engine, so
`cenvkit env --expand` is **byte-identical** to what `docker compose` interpolates
(and to `env-debug --show effective`). `-e <env>` overrides `CENVKIT_ENV` for one call;
shell-set vars win over the chain (compose parity).

If a wrapper calls `docker compose --env-file <path>`, pass the same path as
`cenvkit … --env-file <path>` (or list it in `.cenvkit.envchain`) so the debug and
gap views see the values the run actually interpolates.

> ⚠️ `env`, `run --print` and `env-debug --value|--var` print values **verbatim,
> including secrets**. That is the point — but don't pipe them into shared logs.

**Named chains** — give `.cenvkit.envchain` optional `[name]` sections and pick one
with `--chain <name>` (default = the header-less / `[default]` list); sections are
standalone (no inheritance) and orthogonal to `CENVKIT_ENV`.

**Secrets are out of scope** — cenvkit never masks/encrypts/manages them;
`.secrets.env` just loads last (last-wins), nothing written to disk. For real secret
management, wrap it: `sops exec-env -- cenvkit run -- <cmd>`.

---

## The env-chain

**The project chain is `COMPOSE_ENV_FILES`** — the interpolation context. Listed
in `.cenvkit.envchain` (or built-in defaults when that file is absent). The default
chain is:

```
.env             # non-secret defaults (committed via example.env)
.${ENV}.env      # per-environment overlay: .dev.env / .prod.env / …
.secrets.env     # secrets, gitignored — loaded LAST so it wins
```

`${CENVKIT_ENV}` (alias `${ENV}`) is resolved as **shell `CENVKIT_ENV`
> `.env`'s `CENVKIT_ENV` > `"dev"`**. Non-existent files are silently skipped.

**Service `env_file:` is runtime-only** — it configures the container, not
interpolation, and is **not** added to `COMPOSE_ENV_FILES`. cenvkit still loads the
real, include-aware model and enumerates those paths, but only to power `env-debug`
(the gap-detector and the `--files` runtime-only view).

```
  shell CENVKIT_ENV / .env / "dev"  ─┐
                                     ├─►  ${ENV} substitution
  .cenvkit.envchain (Layer 1)  ──────┴─►  COMPOSE_ENV_FILES  ──►  docker compose
                                                                  (interpolation)
  service env_file:  ──►  container runtime env only  +  env-debug gap-detector
```

See `cenvkit env-files` for the chain, and `cenvkit env-debug --show files` for the
runtime-only env_file paths.

### Named chains

One file can hold several chains; `--chain <name>` picks one (sections are
standalone — no inheritance from the default):

```
.secrets.env
.env
.${HOSTNAME}.env

[ci]
.env

[prod]
.secrets.env
.env
.prod.env
```

`cenvkit --chain ci gap-report` lints exactly what CI can see, instead of
reporting the absence of workstation files. Full semantics and pitfalls:
[`docs/agent-notes/named-chains.md`](docs/agent-notes/named-chains.md).

### Replacing a compiled-env wrapper

If you already have a `compile-env.sh` that glues `.env` + secrets + a machine
override into one file and runs `docker compose --env-file …`, cenvkit can take
over the delivery step without changing your compose files:

```sh
# before
bin/compile-env.sh
docker compose --env-file data/.compiled.env up

# after
eval "$(cenvkit env --format shell)"   # same merged values, into this shell
cenvkit compose up                     # sets COMPOSE_ENV_FILES, execs docker compose
```

Two things to carry over: put the machine override in the chain as
`.${HOSTNAME}.env`, and if any consumer still passes `--env-file` explicitly,
list that file in `.cenvkit.envchain` — cenvkit models `COMPOSE_ENV_FILES` and
`COMPOSE_FILE`, not arbitrary docker CLI flags.

---

## Run from root AND from a subproject

`cenvkit` resolves everything relative to its **project directory**: the current
directory by default, or `--project-dir <dir>` to point elsewhere. All env-chain
resolution and `env_file:` enumeration happen relative to that directory — so
running from the repo root and running from a subproject each resolve their own
files correctly.

```sh
cenvkit env-files                    # resolve from the current directory
cd web && cenvkit compose config     # resolve web/'s own chain + env_file:
cenvkit --project-dir web env-files  # same, without changing directory
```

### Monorepo — root orchestrates subprojects

The flip side of subproject *isolation* is the **unified stack**: a root compose
that `include:`s each subproject, run as one stack from the root. cenvkit loads the
real, include-aware model, so `env-debug` sees the whole project. Note the rule: a
`${WEB_PORT}` declared only in `web/.web.env` (a service `env_file:`) **falls back**
at the root — `env_file:` is runtime-only. `cenvkit env-debug --var WEB_PORT`
flags the gap; promote `WEB_PORT` to the Layer-1 chain if you need it interpolated.

Runnable blueprint: [`examples/monorepo/`](examples/monorepo/).

---

## The debug flow at a glance

`cenvkit env-debug` is **provenance-backed and daemon-free**: it loads the
compose model in-process (compose-go), with no `docker compose` shell-out, and
never hardcodes your variable or service names.

**Views cascade**: repeat `--show` and the sections render in the order given
(no `--show` → the Layer-1 list). Filters narrow every view with the data.

| `--show …` | What it shows |
|---|---|
| `list` (default) | the Layer-1 chain files, in load order (secrets last) |
| `files` | interpolation set (`COMPOSE_ENV_FILES`) + runtime-only service `env_file:` paths |
| `overview` | per-file layering (`+`/`~`/`·`, raw values) + per-service `env_file:` layers + `inline environment:`, with `⚠ gap` lines |
| `effective` | each service effective env, with the source of every value |
| `trace` | one variable winner/effects — or the **gap** (env_file-only, so the run falls back) |
| `deps` | dependency closure of `--var`: definitions, forward/reverse references, consumers |

`--var NAME` filters every view (and alone implies `trace`); `--service S` narrows
the per-service views (and alone implies `effective`). `--redact` masks values
before you paste output anywhere. `--json --show …` emits only those sections;
plain `--json` keeps the whole report.

Output is **colored** on a terminal (markers, headers, gaps) and **plain** when
piped / `--json` / `NO_COLOR` / CI — control with `--color=auto|always|never`.

Full reference: [`docs/cenvkit.md`](docs/cenvkit.md).

```sh
cenvkit env-debug                                  # the chain, in load order
cenvkit env-debug --var DATABASE_HOST              # who set/shadowed it
cenvkit env-debug --show files --show effective --service web
cenvkit env-debug --show deps --var LIVEKIT_KEY --redact  # definitions + every consumer
cenvkit env-debug --json --show effective --var APP_PORT
```
---

## Requirements

- **A Go toolchain** to install or build `cenvkit` (`go install` /
  `go run …@latest`, or `go build` in vendored mode). No Python, no Node.
- **Docker Compose** for the compose-touching commands (`cenvkit compose …`,
  `cenvkit validate`). The `env-debug` provenance modes load the model
  in-process via compose-go and need **no** running Docker daemon.
- **Cross-platform:** `cenvkit` is a pure-Go binary — it runs natively on Linux,
  macOS, and Windows.

---

## Layout

```
cenvkit/
├── cmd/cenvkit/         # the Go CLI entry (cobra)
├── internal/
│   ├── chain/           # Layer 1 — the .cenvkit.envchain project chain (pure Go)
│   ├── engine/          # service env_file: enumeration for env-debug (the only compose-go importer)
│   ├── envfiles/        # merge / order / dedup into COMPOSE_ENV_FILES
│   ├── provenance/      # env-debug provenance model + human/JSON render
│   └── bootstrap/       # cenvkit init
├── cenvkit              # POSIX shim (temp build in its module; preserves caller cwd)
├── go.mod / go.sum      # module + compose-go v2.17.0 pin
├── examples/monorepo/   # runnable root-include:s-subprojects blueprint (cenvkit-driven)
├── test/                # acceptance suite driving the cenvkit binary
└── docs/cenvkit.md      # the canonical command + behavior reference
```

---

## Documentation

- [`docs/guide.md`](docs/guide.md) — **the full user guide — start here**:
  install, the env-chain (Layer 1/2), every command with worked examples,
  monorepos, `env-debug` provenance, CI, the behavior contracts, and
  troubleshooting.
- [`docs/cenvkit.md`](docs/cenvkit.md) — the one-page reference (commands +
  behavior contracts at a glance).
- [`docs/agent-notes/`](docs/agent-notes/) — implementation notes: compose-go
  facts, env semantics, testing/CI gotchas, known traps, and
  [named chains](docs/agent-notes/named-chains.md).
- [`docs/integrations/gitlab-offline.md`](docs/integrations/gitlab-offline.md) —
  running the `gap-report` gate on runners without egress: mirror the pinned
  release, verify sha256, publish to GitLab's Generic Package Registry.
- [`scripts/ci/`](scripts/ci/) — `cenvkit-install.sh` (pinned install from a
  mirror or a pre-baked archive), `cenvkit-publish.sh` (mirror a release), and a
  self-test that CI runs.
- [`AGENTS.md`](AGENTS.md) — integration guide for AI agents.
- [`examples/monorepo/`](examples/monorepo/) — runnable, cenvkit-driven
  root-`include:`s-subprojects blueprint.

---

## License

MIT — see [`LICENSE`](LICENSE).
