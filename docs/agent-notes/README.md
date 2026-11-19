**English** · [Русский](../ru/agent-notes/README.md)

# Agent notes — durable engineering knowledge

Compact notes on `cenvkit` internals for people who change the code. This is
what is not in `--help`: compose-go mechanics, environment layer boundaries, and
the traps of the gates and tooling.

## Why this exists

The repository is published without its internal development history. Technical
knowledge accumulated in working journals and historical specs has been moved
here in sanitized form — only what the code and tests confirm.

## What's here

| File | About | When to read |
|---|---|---|
| [compose-go.md](compose-go.md) | compose-go (pinned v2.17.0): API, option order, dotenv, templates, provenance, include/overlay | you change `internal/engine/**` or bump compose-go |
| [env-semantics.md](env-semantics.md) | Layer-1 vs Layer-2, last-wins, skip-missing, named chains, `CENVKIT_ENV`, fixture seeding | you touch `internal/chain`, `internal/envfiles`, `internal/envmap`, the selector |
| [named-chains.md](named-chains.md) | `[name]` sections and `--chain`: file format, semantics, precedence trap, inspecting a chain without printing secrets | you use or debug named chains |
| [testing-and-ci.md](testing-and-ci.md) | gates, gofmt-after-edit, tool versions, acceptance harness, assertion counter | you are about to claim "green", or you edit `test/**` or CI |
| [gotchas.md](gotchas.md) | stumbling blocks: macOS/BSD, colors, `t.Setenv`, renames, linter cache | something behaves inexplicably |

## Quick route

| Task | Where to look |
|---|---|
| `${VAR}` does not interpolate the way you expect | [env-semantics.md](env-semantics.md), then [compose-go.md](compose-go.md) |
| Editing compose model loading/walking | [compose-go.md](compose-go.md) |
| Adding a command or a flag | [testing-and-ci.md](testing-and-ci.md) + [gotchas.md](gotchas.md) |
| Acceptance or CI is red | [testing-and-ci.md](testing-and-ci.md) |
| Inexplicable output, color, cache | [gotchas.md](gotchas.md) |
| Changing the chain format or the selector | [env-semantics.md](env-semantics.md) |
| Bumping compose-go | [compose-go.md](compose-go.md) + the acceptance gate |

Code references are given from the repository root (`internal/engine/flatten.go`);
symbols like `Flatten` are function names, not line numbers.

## Hierarchy of sources of truth

1. **Code and tests** — normative truth. A note is stale if it disagrees with them.
2. `AGENTS.md` — the development and verification contract (gates, package boundaries).
3. `docs/cenvkit.md` (command reference) and `docs/guide.md` (user guide) —
   user-facing behavior is described there, not here.
4. `CHANGELOG.md` — what changed and when.
5. These notes — "why it is this way" and "where the catch is".

## How to maintain

- **Check against the code before writing.** A fact must be confirmed by a file or
  a test; if it is not, do not write it.
- **Reference files and symbols, not line numbers:**
  `internal/engine/flatten.go`, the `Flatten` function. Lines drift, names do not.
- **One topic, one file.** Do not duplicate: link from everywhere else.
- **Write the consequence, not the history.** "You cannot do that because X" is
  more useful than "we fixed that last cycle".
- **Sanitization is mandatory:** no absolute paths, private repository names,
  email addresses, internal links, or ticket titles. For examples use
  `<workspace>`, `<consumer-repo>`, `<repo>`.
- **Stay in sync:** when you change behavior, fix the note that contradicts it in
  the same change. Added a file? Add a row to the table.
- **Size:** 60–140 lines per file. When it grows, split out a subsection rather
  than adding paragraphs.

## What is deliberately absent

- Secrets, real environment values, private names and addresses.
- Process instructions ("how to run the project") — those live in `AGENTS.md`.
- Plans and unimplemented designs: only what the code does now.
- Paraphrases of user documentation: link to it instead.
