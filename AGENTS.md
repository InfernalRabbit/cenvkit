# AGENTS.md — cenvkit

## Development and verification

- Use the Go version in `go.mod` (currently 1.26.3). Run local source with
  `go run ./cmd/cenvkit <args>`; `make dev-build` produces `.cenvkit.bin`.
  The tracked root `cenvkit` is a POSIX-sh launcher: never overwrite it with a binary.
- `make ci` runs vet, formatting, import-seam, build, and tests; lint runs only
  when installed. CI requires golangci-lint v2.12.2, built with the project's Go:
  `go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2`,
  then `golangci-lint run ./...`. An older-Go prebuilt linter rejects this module;
  conversely, if the machine's default `go` is NEWER than go.mod's, a linter built
  with the project Go cannot typecheck that stdlib. `make lint`/`make ci` pin
  `GOTOOLCHAIN` to the go.mod version for exactly that reason.
- Docker-free suite: `SMOKE_SKIP_DOCKER=1 go test ./... -count=1`.
  Focused package/test: `go test ./internal/engine -run '^TestFlatten_' -count=1`.
- Compose-related changes also require `go test ./test/... -v -count=1` with
  Docker Compose available and `SMOKE_SKIP_DOCKER` unset. The gate probes
  `docker compose version`; these config-only tests do not require a daemon.
  Skipped Docker assertions do not establish Compose parity. CI installs a
  **pinned** Compose release (`COMPOSE_VERSION` + sha256 in `ci.yml`) — do not assume
  the runner's bundled version is used, and bump the pin deliberately (version and
  checksum in one commit) after the suite passes. A non-blocking `compose-latest`
  job reports drift against upstream.
- Recheck `make fmt-check` after the final Go edit. For release-config changes,
  run `goreleaser check` (also a separate CI job).
- Shipped shell is POSIX `sh` (`cenvkit` launcher, `scripts/ci/*.sh`): keep it
  `sh -n`-clean and run `sh scripts/ci/cenvkit-install-selftest.sh` when touching
  the installer (CI does both).
- Acceptance `TestMain` builds its own temporary CLI; no prebuilt binary needed.
  Fixtures copy `examples/monorepo` and seed dot-env files from `example.*`.
  When changing the counted assertions, update `declaredAssertions` and the
  line-2 count in `test/cenvkit-acceptance_test.go` together.
- `make demo` stages and initializes a temporary blueprint copy; use it for the
  gap-debugger showcase rather than populating the tracked example directory.

## Architecture and semantic boundaries

- `cmd/cenvkit/main.go` wires Cobra commands. `compose`/`env-files` call
  `chain.Resolve` → `envfiles.Assemble(cr.Files, nil)`; service env files must
  never be appended to the run chain, even though `Assemble` accepts Layer 2.
- `compose` disables Cobra flag parsing to pass Docker flags through; new wrapper
  flags need explicit extraction in that command or they leak to Docker Compose.
- **Only `internal/engine` may import compose-go**, including its dotenv/template
  subpackages (`make seam` enforces this). Keep upstream Compose semantics here;
  deliberate compose-go version bumps require acceptance verification.
- `engine.Flatten` is the shared expansion primitive for `run`/`env` (through
  `internal/envmap`) and debug interpolation. `chain`'s small dotenv reader is
  only a selector/seed reader, not the authoritative expansion engine.
  `engine.ProvInput.Env` takes process env, never the mixed `chain.Result.Vars` seed.
- `internal/provenance` owns plain-Go reports/rendering; `engine` builds them.
  Debugging and gap lint load the include-aware model in-process, without Docker.
  Discovery follows `include:`/`COMPOSE_FILE`, not recursive globs;
  `COMPOSE_DEPTH` is accepted but ignored.
- Preserve the interpolation/runtime split: YAML `${VAR}` reads the Layer-1
  project chain or shell, **not** a service's `env_file:`. Promote interpolation
  inputs to Layer 1; detect runtime-only gaps with `gap-report` or
  `env-debug --trace --var V`. `gap-report` exits 0 clean / 1 gaps / 2 no compose file.
  `compose` does not automatically run gap lint; invoke it separately when needed.
- Keep blueprint commands routed through `cenvkit compose`: raw Compose loses
  project-chain assembly. `--project-dir` defaults to cwd; subprojects are isolated.

## Configuration contracts

- `.cenvkit.envchain` defaults to `.env` → `.${CENVKIT_ENV}.env` → `.secrets.env`;
  missing entries are skipped, order is last-wins. Custom chains preserve their
  declared order: put secrets last. Secret management/masking/encryption is out of scope.
- Selector precedence: nonempty shell `CENVKIT_ENV` > root `.env` > `dev`.
  `${ENV}` aliases `${CENVKIT_ENV}`; host/env path tokens are sanitized.
  Removed `COMPOSE_ENV`/`.docker-env-chain` names must not be revived.
- `--chain <name>` selects a standalone section (no default inheritance),
  independently of the environment selector; an unknown name exits 2.
- `run` requires `--` before the child command. `run`/`env` are shell-wins;
  `env` emits only chain-derived keys. `--no-expand` disables expansion, not precedence.
- `COMPOSE_ENV_FILES` is comma-separated; commas in chain paths are rejected.
  `COMPOSE_FILE` uses `COMPOSE_PATH_SEPARATOR` or the OS path-list separator.

## References

- Command/behavior reference: `docs/cenvkit.md`; worked examples: `docs/guide.md`.
- Durable implementation notes live in `docs/agent-notes/` (compose-go facts, env
  semantics, testing/CI gotchas, known traps). They are notes, not a spec: verify
  every claim against the current code and tests before relying on it.
