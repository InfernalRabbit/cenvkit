**English** · [Русский](../ru/agent-notes/named-chains.md)

# Named chains (`[name]` sections and `--chain`)

One `.cenvkit.envchain` can define several **named chains**. Each section is a
complete list of layer-1 files; `--chain <name>` picks one per invocation.

## File format

```
# lines before the first header belong to [default]
.secrets.env
.env
.${HOSTNAME}.env

[ci]
.env

[prod]
.secrets.env
.env
.prod.env

[db-external]
.secrets.env
.env
.docker-db-external.env
```

- `#` comments and blank lines are ignored.
- An explicit `[default]` header is equivalent to the implicit one.
- Section names are used verbatim; `--chain` with no value (or `default`) selects
  the default chain.

## Semantics (all verified)

| Property | Behaviour |
|---|---|
| Inheritance | **None.** Each section is standalone; list every file you need. |
| Order | last-wins. Later files override earlier ones by key. |
| Missing files | silently skipped (so a generated/optional file is safe to list). |
| Unknown `--chain` | exit **2**, message lists the available sections (default omitted). |
| Empty section | empty layer-1 chain; `compose`/validate then set `COMPOSE_DISABLE_ENV_FILE=1` so Docker does not fall back to a native `.env`. |
| Tokens | `${ENV}`, `${CENVKIT_ENV}`, `${HOST}`, `${HOSTNAME}` are substituted inside section entries too. |
| Commands | `--chain` is persistent: `compose`, `env-files`, `validate`, `env-debug`, `gap-report`, `run`, `env`. `version`/`init` accept and ignore it. |

`--chain` selects **which files** form the chain. `--project-dir` selects **which
directory** is treated as the project. They compose: `cenvkit --project-dir web
--chain ci env-debug --show files`.

## Why bother

A CI runner rarely has the same environment files as a workstation. Pointing the
gate at a hermetic section keeps `gap-report` honest instead of reporting the
absence of files:

```sh
cenvkit --chain ci gap-report          # what CI can actually see
cenvkit --chain prod compose config    # validate the deploy chain
```

Deploy chains are also a clean place to put a generated env file last, so debug
views match the real run:

```
[prod]
.secrets.env
.env
data/.compiled.env      # generated; missing => skipped
```

## Precedence trap

`last-wins` means a **committed template wins over a secret** if both define the
same key and the template comes later:

```
.secrets.env   # API_KEY=real
.env           # API_KEY=CHANGE_ME  -> wins, because it is later
```

For deploy sections put secrets **last**. For a local chain where `.env` only
*references* a value defined in `.secrets.env`, the reverse order
(`.secrets.env` first) is what makes `${VAR}` interpolation resolve — but then a
duplicated key in `.env` would silently override the secret. Pick one order and
keep keys unique across tiers.

## Inspecting a chain without printing secrets

```sh
cenvkit --chain ci env --format json | jq -r 'keys[]'   # key names only
cenvkit --chain ci env-debug --show files                   # file paths only
cenvkit --chain ci env-debug --json | jq '.files'      # resolved layer-1 list
```

`env --format`/`env-debug --var`/`env-debug --show overview` print values verbatim —
keep them out of shared logs.
