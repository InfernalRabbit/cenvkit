**English** · [Русский](../ru/agent-notes/env-semantics.md)

# Environment semantics: Layer-1, Layer-2, the selector

## Two layers and who owns them

| | Layer-1 (project chain) | Layer-2 (service `env_file:`) |
|---|---|---|
| Source | `.cenvkit.envchain` (or the built-in default) | `env_file:` in a compose service |
| Scope | the whole project: `${VAR}` in YAML + `COMPOSE_ENV_FILES` | only its own service's container |
| In `COMPOSE_ENV_FILES` | yes, in chain order | **no, never** |
| In `${VAR}` interpolation | yes | no |
| Code | `internal/chain/chain.go` | `internal/engine/**` |

The run path assembles `envfiles.Assemble(cr.Files, nil)` — the second argument is
always `nil` (`cmd/cenvkit/main.go`, `assemble`). Layer-2 exists only inside
`env-debug`/`gap-report` as gap evidence.

## Why Layer-2 is not merged into the run chain

Compose interpolates the whole YAML against ONE env map and only then distributes
it over the services. Fold the `env_file:` of all active services into one
`COMPOSE_ENV_FILES` and a shared key (`${PORT}`) becomes a single global value
for everyone, that is, a collision. Per-service scope is out of reach in a thin
upstream-first tool, so the chosen mode is: runtime-only at run time, debug-only
at enumeration time. Hence the definition of a gap: `${VAR}` is defined in a
service's `env_file:` but not in Layer-1 → on a real run it falls back to
`:-default`/an empty value.

## `.cenvkit.envchain` format

- No file → the built-in chain `.env`, `.${CENVKIT_ENV}.env`, `.secrets.env`.
- Lines before the first `[name]` header (and a file with no headers) are the
  implicit `[default]` section.
- Each `[name]` is a COMPLETE standalone list of files; there is no inheritance
  from `[default]`. An explicit `[default]` later in the file EXTENDS the
  implicit one (both parts are concatenated — deliberately).
- Tokens: `${ENV}` ≡ `${CENVKIT_ENV}`, `${HOST}` ≡ `${HOSTNAME}`; values are
  sanitized to `[A-Za-z0-9._-]` — this kills injections into the path.
- Any other `${VAR}` stays a literal: the file simply will not be found and will
  be skipped. There is no full template engine in the chain — do not count on
  `${FOO:-bar}` in a path.
- A resolved path containing a comma is an error (the comma separates
  `COMPOSE_ENV_FILES`).
- Relative paths are joined to the project directory, not to cwd.
- An unknown `--chain` → exit 2 and the list of available names (without
  `default`).

## Order, last-wins, skip-missing

- Line order = load order; the last file wins (last-wins).
- `.secrets.env` last is a contract INSIDE Layer-1; `env-debug --list` checks it,
  not `env-files`. There are no cross-layer guarantees: a service's `env_file:`
  can override a chain variable inside the container. Therefore do not reuse
  secret names in service files.
- Missing files are skipped: `chain.Resolve` (os.Stat), `engine.Resolve`
  (os.Stat), `envfiles.Assemble` (accepts an already prepared list).
- `engine.Flatten` is the exception: `GetEnvFromFile` FAILS on a missing file, so
  it is only ever passed the list from `chain.Resolve`.
- Dedup: the first path keeps its position, repeats are dropped
  (`envfiles.Assemble`).

## The `CENVKIT_ENV` selector

Precedence: nonempty `CENVKIT_ENV` from the shell > `CENVKIT_ENV=` in the root
`.env` > `dev`. Source for the `--overview` header:
`shell` | `.env` | `default` (`chain.Result.ComposeEnvSource`).

- The value is sanitized; an empty value in the shell does not win.
- `chain.Resolve` adds `CENVKIT_ENV=<resolved>` to `Result.Vars` if it is not
  there yet.
- `-e <env>` on `run`/`env` appends `CENVKIT_ENV=<v>` AFTER `os.Environ()` and
  thereby re-resolves the chain (last-wins in `osEnvMap`). `validate --all`
  walks dev→prod the same way.
- `CENVKIT_ENV` is NOT `COMPOSE_ENV_FILES` (a real compose variable). We do not
  bring back removed historical names.
- `--chain <name>` is orthogonal to the selector: the section is selected first,
  and inside it `${CENVKIT_ENV}` still selects the tier.

## Shell-wins and what `Result.Vars` is

- `Result.Vars` = file variables (in chain order) + the OS environment on top; the
  shell wins. It is a SEED for the engine, NOT a ready process environment.
- `envmap.Resolve` builds `Full` = process env + chain values added only if unset
  (shell wins), and `ChainKeys` — only the keys the chain contributed.
  `cenvkit env` prints exactly `ChainKeys` and does not dump the inherited
  environment into CI logs.
- `--no-expand` disables only `${VAR}` expansion, not precedence: the shell-wins
  overlay works in both modes.

## `interpEnv` in engine

`engine.Provenance` builds a single interpolation context: `Flatten(shellMap,
layer1Files, true)`, then the shell (`in.Env`) on top. Everything that
interpolates (attribution A, overlay, the mapping for B-lite, `cli.WithEnv`,
`details.Environment`) reads ONLY this context. `in.Env` is exclusively the
process environment: `chain.Result.Vars` must not be passed there (it holds
unexpanded file values). Mixing contexts brings back the bug the tool exists for.

## `CENVKIT_ENV` in the child environment

`composeEnvironment` (`cmd/cenvkit/main.go`) gives the children `os.Environ()` +
`COMPOSE_ENV_FILES=...` (+ `COMPOSE_DISABLE_ENV_FILE=1` if the chain is empty).
`Result.Vars` never reaches the child environment. Consequence: `${CENVKIT_ENV}`
inside `COMPOSE_FILE` resolves only if the variable is actually present in the
shell/chain; the "default dev" itself is not set. When you add a new chain
consumer, pass the resolved `CENVKIT_ENV` explicitly.

## Fixtures: seeding

`examples/monorepo` is the parity reference; it must not be edited.

- The root `.env`, `.dev.env`, `.prod.env`, `.secrets.env` are gitignored; the
  repository only has `example.*`. A bare copy yields an empty Layer-1 — this is
  correct skip-missing, not a bug.
- Service dotfiles (`web/.web.env`, `api/.api.env`,
  `services/reports/.reports.env`) are COMMITTED and come with the copy. There is
  no need to seed them; do not write stubs over them.
- Acceptance: a copy in `t.TempDir()` + seeding `example.*` → `.*`
  (`stageMonorepo`). A new named section requires a committed
  `example.<x>.env` and a line in the seeding, otherwise it is green locally and
  not on a clean checkout. Check: `git ls-files` +
  `rm <dotfile> && go test ./test/...`.

## Debug views: what to use

| Flag | What it shows |
|---|---|
| `env-debug` / `--list` | Layer-1 files (secrets last) |
| `--files` | `COMPOSE_ENV_FILES` (Layer-1) + runtime-only `env_file:` per service |
| `--trace --var V` | the winner, shadowed sources, `Effects`, the gap and the fallback |
| `--value --var V` | the winning value from the interpolation context |
| `--effective [--service S]` | the service's effective environment with sources |
| `--overview` | layered breakdown: raw values, `+`/`~`/`·` markers |
| `--json` | the same, machine-readable, always without ANSI |

`env-files` prints only Layer-1 (the run path). `gap-report` — only gaps:
exit 0 clean / 1 gaps / 2 no compose file.
