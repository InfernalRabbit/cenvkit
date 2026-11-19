**English** · [Русский](../ru/agent-notes/compose-go.md)

# compose-go — working facts

Verified against the pin `github.com/compose-spec/compose-go/v2 v2.11.0` (`go.mod`)
and live probes. Applies to `internal/engine/**`.

## Boundaries and the pin

- Only `internal/engine` imports compose-go, including its `cli`, `loader`,
  `dotenv`, `template`, and `types` subpackages. Gates: `make seam` and the CI
  step `compose-go seam check` (`go list -f ...`, without `-deps`).
- The other packages see the plain-Go `Result`/`ProjectView`
  (`engine/engine.go`), so `cmd` and `internal/provenance` do not depend on
  compose-go.
- The version is deliberately pinned (`cenvkit version` prints it from build info,
  `composeGoVersion`): bump it — rerun acceptance. upstream-first: the only
  things we compute ourselves are the `COMPOSE_FILE` selection (needed before
  loading) and `Flatten`.

## Loading a project

```go
opts, err := cli.NewProjectOptions(configs,
    cli.WithWorkingDirectory(dir),
    cli.WithEnv(env),                  // interpolation seed
    cli.WithDefaultProfiles(profiles...),
    cli.WithResolvedPaths(true),       // EnvFile.Path is absolute
    cli.WithInterpolation(true),
    cli.WithoutEnvironmentResolution,  // "bare" ProjectOptionsFn, no parentheses
)
proj, err := opts.LoadProject(ctx)
```

- `cli.ProjectFromOptions` is deprecated; use
  `NewProjectOptions(...).LoadProject`.
- `Project.Services` is only the ACTIVE set; profile-disabled services go to
  `DisabledServices`. It is a map → sort names for determinism.
- `ServiceConfig.EnvFiles []EnvFile{Path, Required OptOut, Format}`; `OptOut`
  defaults to TRUE — a missing required `env_file:` is a load error.
- `ServiceConfig.Environment` is `map[string]*string` (`nil` = "unset").
- Option order matters: `ProjectOptionsFn` are applied as passed.

## Why COMPOSE_FILE is resolved by hand

`cli.WithConfigFileEnv` is not usable as the only path: (1) it reads
`o.Environment["COMPOSE_FILE"]`, which is empty without `cli.WithEnv` BEFORE it;
(2) it does NOT interpolate `${VAR}` in the value; (3) it resolves relative
paths from the process cwd, not from `WithWorkingDirectory`.

That is why `internal/engine/discover.go`:

- no `COMPOSE_FILE` → standard discovery
  `cli.NewProjectOptions(nil, WithWorkingDirectory(dir), WithDefaultConfigPath)`,
  take `opts.ConfigPaths` (standard names + upward search);
- present → we split it ourselves on `COMPOSE_PATH_SEPARATOR`, otherwise on
  `os.PathListSeparator` (never `,`), substitute `${ENV}`→`${CENVKIT_ENV}`,
  interpolate with `template.SubstituteWithOptions(...)`, join with `dir` and
  pass the result as `configs`. Missing explicit files are not filtered — the
  loader must report the error.

`HasComposeFileEnv` is only a presence probe; both the gate and the loader call
the SAME `resolveComposeFiles` so they cannot drift apart.

## dotenv: three different reads

| API | Where | Semantics |
|---|---|---|
| `dotenv.GetEnvFromFile(base, files)` | `flatten.go`, `Flatten` | file values only; `base` is the substitution source and is NOT copied into the result; every file sees the full base; a missing file is an ERROR, not a skip |
| `dotenv.ParseWithFormat(...)` | `envfile.go`, `parseEnvFile` | per-file attribution: lookup = external → this file → previous ones; `""` = dotenv, `"raw"` = Docker CLI `kvfile` |
| `dotenv.ReadFile(path, lookupFn)` | `provenance.go` | returns a map; with a `nil` lookup it writes warnings about unresolved `${VAR}` to stderr — pass a closure |

- The compose-go parser returns an UNORDERED map and expands `${...}`; for
  `--show overview` and `--no-expand` there is our own `engine.ParseOrderedLiteral`.
- `GetEnvFromFile` unconditionally logs `level=warning ... variable is not set`
  (logrus, cannot be disabled): the value is right, but `run`/`env` stderr is
  noisy.

## Templates and interpolation

- `template.Mapping = func(string) (string, bool)`; `bool` = "the variable
  exists". `${V:-d}` yields `d` when `!ok`, `${V}` without a default yields an
  empty string.
- `template.SubstituteWithOptions(s, mapping, template.WithoutLogging)` — without
  `WithoutLogging`, unresolved variables are printed to stderr.
- `template.ExtractVariables(dict, template.DefaultPattern)` (if needed) gives
  NAMES only, without paths — walk paths over the dict yourself. The current
  `Effects` are built through `SubstituteWithOptions` with a recording closure.
- Substitution counts the "executed" references: variables from unselected
  branches of `${A:+...}` are not counted. `Provenance` builds `Effects`
  exactly that way.

## B-lite provenance: raw-load + walk

- **RAW load:** `loader.LoadConfigFiles(ctx, configs, projectDir)` → set
  `details.Environment` → `LoadModelWithContext` with `SkipInterpolation`,
  `SkipValidation`, `SkipConsistencyCheck`, `SkipResolveEnvironment`.
  `LoadConfigFiles` does not accept env.
- **Normalization trap:** interpolating loading expands the short forms
  (`ports` → `{published,target}`, `environment` map → a list of
  `"KEY=VALUE"`) — you cannot read the same path from the interpolated dict.
  `walkDict` walks the raw dict (dotted + `[i]`), resolving the leaf in place.
- **The path shape depends on merging:** a single file → `environment.API_PORT`
  (`0`); a service whose `environment:` is reopened by an overlay file →
  `environment[1]` with the value `"WEB_PORT=0"`. A new key that sorts earlier
  SHIFTS the index (acceptance pins this) — check the actual `--json`.
- **Scope:** `splitServiceField` takes only `services.<name>.<field>`; a
  `${VAR}` in `networks`/`volumes`/`configs`/`secrets`/`x-*` deliberately
  does not make it into `Effects` (a B-lite narrowing, not a bug).
- **C:** the declared `svc.EnvFiles` are read in order (missing is skipped), then
  the inline `svc.Environment` (`nil` — remove). The list of declared files is
  kept SEPARATELY from the per-key `Entries`: an inline override can swallow all
  of a file's keys, but the file must remain in `ServiceEnv.EnvFiles` (otherwise
  `--files` lies).
- **Gap:** `referenced && !InChain && len(RuntimeDefs) > 0`; effects and
  runtime-defs are sorted for deterministic JSON.

## D1: lenient enumeration

`cli.WithoutEnvironmentResolution` is a wrapper over
`loader.Options.SkipResolveEnvironment`. Without it `LoadProject` fails on a
missing `required:` env_file; with it, the `svc.EnvFiles` paths are preserved and
we drop the nonexistent ones ourselves (`os.Stat`).

- `SkipValidation` does NOT help; `WithDiscardEnvFile` is the wrong lever (it
  CLEARS `EnvFiles`). Under the lever env_file is not read, `svc.Environment`
  stays inline-only — that is what makes attribution C possible.
- `env_file:` paths are interpolated under the lever too (from `cli.WithEnv`,
  i.e. from Layer-1), but NOT from other services' values.

## include and overlay

- `include:` imports a service, and the main file is NOT allowed to override it.
  Strict Compose: `services.X conflicts with imported resource`; a tolerant
  engine silently merges → "green locally, red in CI".
- A `-f` overlay (the `COMPOSE_FILE` list) is a different mechanism: it merges a
  service into ONE definition, and reopening a service in an overlay is legal.
  Discovery follows `include:`/`COMPOSE_FILE`, not recursive globs;
  `COMPOSE_DEPTH` is ignored. `env_file: required:` and `include:` require
  Docker Compose v2.24+.

## Pitfalls checklist

- A `${VAR}` inside a YAML flow-sequence breaks the parser ("did not find
  expected ',' or ']'"): write an `env_file:` path with tokens in block form
  (`- path: ...`).
- A missing `env_file:` is a `LoadProject` error; only our enumeration is lenient,
  the real `docker compose` runs without the lever.
- Values from a service `env_file:` never reach interpolation: do not mix
  contexts (that is the bug the tool exists for).
