[English](README.md) · **Русский**

# cenvkit

Небольшой **Go CLI** (`cenvkit`), построенный на собственном загрузчике Docker
`compose-go`. Он делает две вещи:

- **Отлаживает единственный gap, который нативный Compose оставляет открытым.**
  Значение, определённое только в сервисном `env_file:`, невидимо для интерполяции
  `${VAR}` на этапе компиляции, поэтому `ports: "${APP_PORT:-3000}:80"` молча
  откатывается к `3000`. `cenvkit env-debug` находит и объясняет это с помощью
  provenance (**без демона**), а **`cenvkit gap-report`** — это линт для CI и
  pre-build. Ни один другой инструмент этого не делает. (См.
  [Gap, который нативный Compose не закрывает](#gap-который-нативный-compose-не-закрывает).)
- **Наполняет env-chain для любого потребителя.** `cenvkit compose` (→ `docker compose`),
  **`cenvkit run -- <cmd>`** (любой процесс, без docker), **`cenvkit env`**
  (вывод dotenv/json/shell) — одна chain, один источник истины для compose *и*
  локальной разработки. Он сочетается с make/just; он их не заменяет.

**`cenvkit` — это Go CLI**, построенный на собственном загрузчике compose от
Docker (`compose-spec/compose-go`). Это единственная реализация — исходный движок
на POSIX-`sh` удалён. Go CLI требует только тулчейн Go (без Python, без Node).

---

## Установка — Go CLI

`cenvkit` построен на собственном загрузчике Docker (`compose-spec/compose-go`,
закреплён v2.17.0). `env-debug`/`gap-report` работают без демона;
`cenvkit compose`/`validate` вызывают через shell ваш **Docker Compose v2.24+**
(нужен для `include:` + `env_file: required:`) и тестируются в CI против
**запиненного** релиза Compose (отдельный неблокирующий job сообщает о дрейфе
относительно upstream latest). Два режима поставки:

```sh
# Installed (recommended)
go install github.com/InfernalRabbit/cenvkit/cmd/cenvkit@latest
# or ephemeral: go run github.com/InfernalRabbit/cenvkit/cmd/cenvkit@latest <args>
```

В вашем проекте:

```sh
cenvkit init               # seed .X from example.X (no-clobber), fan out one level
cenvkit gap-report         # CI lint: exit 1 if a ${VAR} is satisfied only by env_file:
cenvkit env-debug          # inspect the chain / provenance (daemon-free)
cenvkit compose config     # render config (interpolation = your Layer-1 chain)
cenvkit run -- npm test    # run any command with the merged chain env (no docker)
cenvkit env --format shell # emit the merged env (eval-able)
```

Или **вендорьте** его: закоммитьте Go-модуль и POSIX-шим `cenvkit` и запускайте
`./cenvkit <args>` (нужен тулчейн Go; для скорости —
`go build -o .cenvkit.bin ./cmd/cenvkit` (файл в gitignore) — и запускайте его).
Полный справочник по командам и поведению:
**[`docs/ru/cenvkit.md`](docs/ru/cenvkit.md)**.

---

## Что это и зачем

Современный Docker Compose уже закрывает большую часть того, что раньше делал
самописный враппер, — `COMPOSE_ENV_FILES` (проектная chain, last-wins), оверлеи
`COMPOSE_FILE`, `DOCKER_DEFAULT_PLATFORM`. cenvkit оборачивает их для удобства, но
существует ради **одной** задачи, у которой нет нативного эквивалента.

### Gap, который нативный compose не закрывает

Docker Compose намеренно держит две вещи раздельно:

| Слой | Что наполняет | Виден ли для `${VAR}` в YAML на этапе compose? |
|---|---|---|
| Проектное окружение (`--env-file` / `COMPOSE_ENV_FILES`) | контекст интерполяции | **да** |
| `env_file:` сервиса | runtime-окружение контейнера | **нет** |

Поэтому если `APP_PORT` живёт только в `env_file:` сервиса, а вы пишете
`ports: "${APP_PORT:-3000}:80"`, compose интерполирует `${APP_PORT}` **до** того,
как этот `env_file:` будет прочитан, — и вы молча получаете fallback `:-3000`.
Это намеренное, давнее разделение в upstream
([docker/compose#3435](https://github.com/docker/compose/issues/3435), открыт с
2016 года).

`cenvkit` **не** замазывает это, вливая `env_file:` сервисов в интерполяцию — так
все env сервисов сглаживаются в одно глобальное пространство имён (ловушка с
коллизией `${PORT}`, поскольку Compose интерполирует весь YAML против одной
env-карты). Вместо этого он оставляет `env_file:` **только runtime** и даёт вам
работающий без демона **детектор gap**: `env-debug` сообщает, когда `${VAR}`
удовлетворяется только сервисным `env_file:` (то есть запуск откатится к
fallback), показывает runtime-значение и рекомендует исправление — **кладите
значения, на которые ссылаетесь как `${VAR}`, в Layer-1 chain.** Полный
справочник: [`docs/ru/cenvkit.md`](docs/ru/cenvkit.md).

```sh
cenvkit env-files                          # the resolved COMPOSE_ENV_FILES (Layer-1 chain)
cenvkit env-debug --trace --var APP_PORT   # in the chain, or an env_file gap?
cenvkit gap-report                         # CI/pre-build lint: non-zero if any ${VAR} is env_file-only
```

---

## Доставляйте chain куда угодно — `run` / `env`

Та же Layer-1 chain, что питает compose, может управлять **любым** потребителем,
поэтому у вас остаётся один источник истины вместо двух инструментов:

```sh
cenvkit run -- npm run dev             # exec a process with the merged chain env (no docker)
cenvkit run --no-expand -- printenv    # leave ${VAR} literal
cenvkit env                            # emit the merged env (dotenv); --format json|shell
eval "$(cenvkit env --format shell)"   # load it into the current shell
```

`run` / `env` раскрывают `${VAR}` / `${VAR:-def}` через собственный dotenv-движок
compose-go, поэтому `cenvkit env --expand` **байт-в-байт** совпадает с тем, что
интерполирует `docker compose` (и с `env-debug --effective`). `-e <env>`
переопределяет `CENVKIT_ENV` на один вызов; переменные, заданные в shell,
побеждают chain (паритет с compose).

Если обёртка вызывает `docker compose --env-file <path>`, передайте тот же путь как
`cenvkit … --env-file <path>` (или внесите его в `.cenvkit.envchain`), чтобы
диагностика видела те значения, которые реально интерполируются при запуске.

> ⚠️ `env`, `run --print` и `env-debug --value|--trace` печатают значения
> **дословно, включая секреты**. В этом и смысл — но не направляйте их в общие
> логи.

**Именованные chain** — задайте в `.cenvkit.envchain` необязательные секции
`[name]` и выбирайте нужную через `--chain <name>` (по умолчанию — список без
заголовка / `[default]`); секции автономны (без наследования) и ортогональны
`CENVKIT_ENV`.

**Секреты — вне области ответственности** — cenvkit никогда не маскирует, не
шифрует и не управляет секретами; `.secrets.env` просто загружается последним
(last-wins), ничего не пишется на диск. Для настоящего управления секретами
оберните его: `sops exec-env -- cenvkit run -- <cmd>`.

---

## env-chain (цепочка окружения)

**Проектная chain — это `COMPOSE_ENV_FILES`** — контекст интерполяции.
Перечисляется в `.cenvkit.envchain` (или во встроенных значениях по умолчанию,
если файла нет). Chain по умолчанию:

```
.env             # non-secret defaults (committed via example.env)
.${ENV}.env      # per-environment overlay: .dev.env / .prod.env / …
.secrets.env     # secrets, gitignored — loaded LAST so it wins
```

`${CENVKIT_ENV}` (алиас `${ENV}`) разрешается как **shell `CENVKIT_ENV`
> `CENVKIT_ENV` из `.env` > `"dev"`**. Несуществующие файлы молча пропускаются.

**Сервисный `env_file:` — только runtime** — он настраивает контейнер, а не
интерполяцию, и **не** добавляется в `COMPOSE_ENV_FILES`. cenvkit всё равно
загружает настоящую include-aware модель и перечисляет эти пути, но только для
питания `env-debug` (детектора gap и runtime-представления `--files`).

```
  shell CENVKIT_ENV / .env / "dev"  ─┐
                                     ├─►  ${ENV} substitution
  .cenvkit.envchain (Layer 1)  ──────┴─►  COMPOSE_ENV_FILES  ──►  docker compose
                                                                  (interpolation)
  service env_file:  ──►  container runtime env only  +  env-debug gap-detector
```

Смотрите `cenvkit env-files` — там chain, и `cenvkit env-debug --files` — там пути
runtime-only env_file.

### Именованные chain

Один файл может содержать несколько chain; `--chain <name>` выбирает одну (секции
автономны — наследования от default нет):

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

`cenvkit --chain ci gap-report` линтит ровно то, что видит CI, вместо того чтобы
сообщать об отсутствии файлов рабочей станции. Полная семантика и подводные камни:
[`docs/ru/agent-notes/named-chains.md`](docs/ru/agent-notes/named-chains.md).

### Замена враппера, компилирующего env

Если у вас уже есть `compile-env.sh`, который склеивает `.env` + секреты +
машинный override в один файл и запускает `docker compose --env-file …`, cenvkit
может взять на себя шаг доставки, не меняя ваши compose-файлы:

```sh
# before
bin/compile-env.sh
docker compose --env-file data/.compiled.env up

# after
eval "$(cenvkit env --format shell)"   # same merged values, into this shell
cenvkit compose up                     # sets COMPOSE_ENV_FILES, execs docker compose
```

Две вещи нужно перенести: положите машинный override в chain как
`.${HOSTNAME}.env`, и если какой-то потребитель всё ещё явно передаёт
`--env-file`, перечислите этот файл в `.cenvkit.envchain` — cenvkit моделирует
`COMPOSE_ENV_FILES` и `COMPOSE_FILE`, а не произвольные флаги docker CLI.

---

## Запуск и из корня, и из подпроекта

`cenvkit` разрешает всё относительно своего **проектного каталога**: текущего
каталога по умолчанию, либо `--project-dir <dir>`, чтобы указать другой. Всё
разрешение env-chain и перечисление `env_file:` происходят относительно этого
каталога — поэтому запуск из корня репозитория и запуск из подпроекта корректно
разрешают свои файлы.

```sh
cenvkit env-files                    # resolve from the current directory
cd web && cenvkit compose config     # resolve web/'s own chain + env_file:
cenvkit --project-dir web env-files  # same, without changing directory
```

### Монорепо — корень оркестрирует подпроекты

Обратная сторона *изоляции* подпроектов — **единый стек**: корневой compose,
который через `include:` включает каждый подпроект и запускается как один стек из
корня. cenvkit загружает настоящую include-aware модель, поэтому `env-debug` видит
весь проект. Заметьте правило: `${WEB_PORT}`, объявленный только в `web/.web.env`
(сервисный `env_file:`), **откатывается к fallback** в корне — `env_file:` работает
только в runtime. `cenvkit env-debug --trace --var WEB_PORT` помечает gap;
продвиньте `WEB_PORT` в Layer-1 chain, если он нужен для интерполяции.

Запускаемый блюпринт: [`examples/monorepo/`](examples/monorepo/).

---

## Поток отладки с одного взгляда

`cenvkit env-debug` **опирается на provenance и работает без демона**: он
загружает compose-модель в процессе (compose-go), без вызова `docker compose`
через shell, и никогда не хардкодит имена ваших переменных или сервисов. Добавьте
`--json` к любому режиму, чтобы получить структурированный отчёт (для
инструментов/CI).

| Режим | Что показывает |
|---|---|
| `--list` (по умолчанию) | файлы Layer-1 chain в порядке загрузки (секреты последними) |
| `--files` | две группы: интерполяция (`COMPOSE_ENV_FILES`, Layer 1) + runtime-only (пути сервисных `env_file:`) |
| `--overview` | пофайловый обход слоёв (маркеры `+`/`~`/`·`, сырые значения) + слои `env_file:` по сервисам + `inline environment:`, со строками `⚠ gap` |
| `--effective [--service S]` | эффективное окружение каждого сервиса с источником каждого значения (`env_file:` или inline `environment:`) |
| `--trace --var NAME` | победитель `NAME` в chain + где `${NAME}` вступил в силу — или **gap** (`NAME` есть только в сервисном `env_file:`, поэтому запуск откатывается к fallback) |
| `--value --var NAME` | победившее значение `NAME`, одна строка (для скриптов) |

Вывод **цветной** на терминале (маркеры, заголовки, gap) и **простой** при
передаче через пайп / `--json` / `NO_COLOR` / CI — управляется
`--color=auto|always|never`.

Полный справочник: [`docs/ru/cenvkit.md`](docs/ru/cenvkit.md).

```sh
cenvkit env-debug                              # the chain, in load order
cenvkit env-debug --trace --var DATABASE_HOST  # who set/shadowed DATABASE_HOST
cenvkit env-debug --effective --service web     # final values compose will use for web
cenvkit env-debug --trace --var APP_PORT --json # the whole resolution, as JSON
```

---

## Требования

- **Тулчейн Go** для установки или сборки `cenvkit` (`go install` /
  `go run …@latest`, либо `go build` в вендоренном режиме). Без Python, без Node.
- **Docker Compose** для команд, касающихся compose (`cenvkit compose …`,
  `cenvkit validate`). Режимы provenance в `env-debug` загружают модель в процессе
  через compose-go и **не** требуют запущенного демона Docker.
- **Кроссплатформенность:** `cenvkit` — чистый Go-бинарник, он нативно работает на
  Linux, macOS и Windows.

---

## Структура

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

## Документация

- [`docs/ru/guide.md`](docs/ru/guide.md) — **полное руководство пользователя —
  начните здесь**: установка, env-chain (Layer 1/2), каждая команда с рабочими
  примерами, монорепо, provenance в `env-debug`, CI, контракты поведения и
  устранение неполадок.
- [`docs/ru/cenvkit.md`](docs/ru/cenvkit.md) — одностраничный справочник (команды +
  контракты поведения кратко).
- [`docs/ru/agent-notes/`](docs/ru/agent-notes/) — заметки по реализации: факты о
  compose-go, семантика env, подводные камни тестирования/CI, известные ловушки и
  [именованные chain](docs/ru/agent-notes/named-chains.md).
- [`docs/ru/integrations/gitlab-offline.md`](docs/ru/integrations/gitlab-offline.md) —
  запуск гейта `gap-report` на раннерах без egress: зеркалируйте закреплённый
  релиз, проверьте sha256, опубликуйте в GitLab Generic Package Registry.
- [`scripts/ci/`](scripts/ci/) — `cenvkit-install.sh` (закреплённая установка из
  зеркала или из готового архива), `cenvkit-publish.sh` (зеркалирование релиза) и
  самотест, который выполняет CI.
- [`AGENTS.md`](AGENTS.md) — руководство по интеграции для AI-агентов.
- [`examples/monorepo/`](examples/monorepo/) — запускаемый блюпринт с корневым
  `include:` подпроектов, управляемый cenvkit.

---

## Лицензия

MIT — см. [`LICENSE`](LICENSE).
