**English** · [Русский](../ru/agent-notes/gotchas.md)

# Gotchas

Each item is "symptom → cause → what to do". Verified on this tree and with live
probes.

## macOS / BSD utilities

- **`sed -i` without an argument.** BSD requires an explicit backup suffix:
  `sed -i '' 's/a/b/g' file`. The GNU form `-i` on macOS eats the next part of
  the command and prints a mysterious error.
- **`grep -lZ ... | xargs -0 sed` collapses names.** sed receives one glued
  argument and edits zero files; the `&&` chain still looks successful if
  `go mod edit` ran earlier. A line-by-line loop is more reliable:

  ```sh
  find . -name '*.go' -exec grep -l 'OLD' {} + | while IFS= read -r f; do
    sed -i '' 's#OLD#NEW#g' "$f"
  done
  ```

  Check: `grep -rn 'OLD' --include='*.go' . go.mod` (empty = clean).
- Keep bulk replacements within your lane (`--include='*.go'`), otherwise you hit
  docs/yaml that someone else is editing.

## Renames

- **`COMPOSE_ENV` → `CENVKIT_ENV` without a word boundary breaks
  `COMPOSE_ENV_FILES`.** `COMPOSE_ENV` is a prefix of a real Docker Compose
  variable, so a naive `s/COMPOSE_ENV/CENVKIT_ENV/` produces a nonexistent
  `CENVKIT_ENV_FILES`. The build stays green if the corruption lands in strings
  and comments. Use `s/COMPOSE_ENV\b/.../`, and after the loop check:
  `grep -rn CENVKIT_ENV_FILES internal/ cmd/ test/ examples/` → empty.
- After any rename, grep the OLD name across the whole tree, including `docs/`
  and `CHANGELOG.md`.

## Tests and environment

- **`t.Setenv("K", "")` does NOT remove the variable.** It stays in
  `os.Environ()` as `K=`, and the engine counts presence: `InChain=true` → the
  gap is masked. In-process: `os.Unsetenv` + restore in `t.Cleanup`. Child
  binary: `envWithout(keys...)`.
- **`cenvkit env` (dotenv) quotes values**: `CI="true"`. An assertion for
  `CI=true` fails even with the right value.
- **`run` looks up the child binary by the PATH of the merged environment**, not
  by the wrapper's PATH (`lookPathInEnv`, `cmd/cenvkit/lookup.go`): a test with
  `PATH=` in `.env` must find its binary and preserve its exit code.
- **`go build ./cmd/cenvkit` creates a `cenvkit` binary** and overwrites the
  same-named POSIX launcher `cenvkit` in the root. Build with `make dev-build`
  (`.cenvkit.bin`, `Makefile`) or `-o` to another place; the binary is not
  committed.
- **The `./cenvkit` launcher builds into a temporary directory and runs in the
  caller's cwd** (`go -C <repo> build`). That is why it also works next to
  someone else's `go.mod`; a regression test covers this.
- From a directory outside the module `go run ./cmd/cenvkit` does not work (no
  `go.mod` up the tree): build the binary once and run it.

## Terminal and colors

- **Per-renderer profiles only.** `lipgloss.NewRenderer(w)` +
  `SetColorProfile(...)`; the global `lipgloss.SetColorProfile` mutates the
  default renderer and leaks into the JSON branch. `internal/style/style.go` is
  the only lipgloss importer.
- **`AdaptiveColor` hangs the CLI**: background resolution issues a blocking OSC
  query to the terminal. A fixed ANSI-16 palette is used, readable on both light
  and dark backgrounds.
- Auto-detection via `NewRenderer` accounts for `NO_COLOR`, `CLICOLOR_FORCE`,
  TTY, and `CI` by itself; `--color=never` → Ascii, `--color=always` → ANSI256.
  `--json` is always without ANSI (highest priority).
- Errors are gated on `os.Stderr` by a separate styler, and `--color` for them is
  read by scanning `os.Args` (`colorFlagFromArgs`, `cmd/cenvkit/main.go`):
  `PersistentPreRunE` does not run if flag parsing already failed.
- **The `Value` style is deliberately empty**: ANSI in values breaks the tabbed
  alignment of `--effective`. If you add color, fix the alignment in the same
  change.
- **ANSI/OSC from data is not stripped** in human output
  (`internal/provenance/render.go`): values, paths, and section names are printed
  as-is (`--json` escapes). When you add a new output, sanitize at the render
  boundary.

## Linter and cache

- A false batch of `SA5011` (or similar typecheck errors) with a dirty cache:
  `golangci-lint cache clean` first, retry, and only then edit the code.
- A "quiet" green: `make lint`/`make ci` skip lint if the binary is absent. Check
  `command -v golangci-lint` before saying "full gate".
- `golangci-lint` under the wrong Go fails on the stdlib — see
  [testing-and-ci.md](testing-and-ci.md), the version traps section.

## compose / engine

- **${VAR} inside a YAML flow-sequence** (`env_file: [./${X}/.b.env]`) — a YAML
  parser error even before interpolation. Write the block form:
  `- path: ./${X}/.b.env` + `required: false`.
- **A missing `env_file:`** is a `LoadProject` error (unless
  `cli.WithoutEnvironmentResolution` is enabled): enumeration must filter paths
  (`os.Stat`) and/or enable that lever.
- **`--no-expand`** does not expand `${VAR}` but keeps shell-wins precedence: it
  is about expansion, not precedence.
- `service env_file:` values never take part in interpolation: if `${VAR}` "is
  in the service file", it still falls back on a run. Do not "cure" this by
  editing the renderer — it is a contract.

## CLI wrappers

- `compose` enables `DisableFlagParsing`, so persistent flags (`--project-dir`,
  `--chain`, `--color`) are extracted manually (`extractWrapperFlags`) before
  they are passed to docker. Forget it — and docker rejects an unknown flag.
- Run the child `docker compose` with `Dir = projectDir`, not from the process
  cwd.
- `run` requires `--`; `ArgsLenAtDash() < 0` → exit 2. Codes: 126 (not
  executable), 127 (not found), 128+signal.
- `gap-report`: 0 — clean, 1 — gaps, 2 — no compose file (this is NOT "clean").
- A new wrapper flag without extraction lands in docker compose as an argument —
  check the `cenvkit compose ... --help` scenario after adding it.
