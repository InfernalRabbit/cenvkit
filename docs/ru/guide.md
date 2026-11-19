[English](../guide.md) · **Русский**

# cenvkit — руководство пользователя

Полное сквозное руководство по `cenvkit`: что он делает, как его установить, как
настроить проект, все команды, монорепо, **gap-детектор** в env-debug, CI,
контракты поведения, на которые можно полагаться, и устранение неполадок.

Краткий справочник — [`cenvkit.md`](cenvkit.md). Заметки по реализации и модель
Layer-1/Layer-2 — [`agent-notes/`](agent-notes/).

---

## 1. Что это и какой gap он выявляет

`cenvkit` — небольшой Go CLI, построенный на **собственном загрузчике compose от
Docker** (`github.com/compose-spec/compose-go/v2`). Он собирает
`COMPOSE_ENV_FILES` из вашей **проектной env chain** и затем делает `exec` в
`docker compose` — и поставляет отладчик, работающий без демона, который
объясняет, где разрешается каждая переменная.

**Два слоя, намеренно раздельные (это Docker, а не cenvkit):**

| Слой | Что наполняет | Виден ли для `${VAR}` в YAML на этапе compose? |
|---|---|---|
| Проектная env chain (`COMPOSE_ENV_FILES` / `--env-file` / `.env`) | контекст интерполяции | **да** |
| `env_file:` сервиса | runtime-окружение контейнера | **нет** |

Поэтому если `APP_PORT` живёт только в `env_file:` сервиса, а ваш YAML говорит
`ports: "${APP_PORT:-3000}:80"`, compose интерполирует `${APP_PORT}` **до** того,
как прочитает этот `env_file:`, — вы молча получаете fallback `:-3000`. (Upstream
[docker/compose#3435](https://github.com/docker/compose/issues/3435), открыт с
2016 года.)

**Что cenvkit с этим делает:**

1. Он собирает `COMPOSE_ENV_FILES` из вашей **проектной chain** (Layer 1) —
   правильного места для значений, которые *должны* питать интерполяцию `${VAR}`
   по всему проекту.
2. Он оставляет `env_file:` каждого сервиса **только runtime** — в точности
   нативное поведение Docker (по сервисам, изолированно). cenvkit **не** вливает
   его в интерполяцию.
3. Его `env-debug` **обнаруживает и объясняет gap**: когда `${VAR}` удовлетворяется
   только сервисным `env_file:`, он сообщает, что запуск откатится к fallback,
   показывает runtime-значение и говорит, как это исправить.

> **Почему бы просто не закрыть gap, влив env_files в интерполяцию?** Более ранние
> версии делали именно это. Но сервисный `env_file:` — *посервисный*, и сглаживание
> файла каждого сервиса в один плоский `COMPOSE_ENV_FILES` схлопывает общий ключ
> (`${PORT}`) в одно **глобальное** значение интерполяции для всех сервисов —
> настоящая ловушка с коллизией, потому что Compose интерполирует весь YAML против
> одной глобальной env-карты. Поэтому правило такое: **значения, предназначенные
> для интерполяции `${VAR}`, принадлежат Layer-1 chain; `env_file:` — для
> контейнера.** cenvkit выявляет gap вместо того, чтобы молча его замазывать.

---

## 2. Установка

**Установленный (рекомендуется).** Нужен тулчейн Go:

```sh
go install github.com/InfernalRabbit/cenvkit/cmd/cenvkit@latest
cenvkit version
```

**Эфемерный** (без установки — в стиле npx):

```sh
go run github.com/InfernalRabbit/cenvkit/cmd/cenvkit@latest env-files
```

**Вендоренный** (закоммитьте модуль + POSIX-шим `cenvkit` в свой репозиторий;
нужен тулчейн Go, без закоммиченных бинарников, без сети):

```sh
./cenvkit <args>          # temp build in the tool's module; runs in your current directory
```

Для меньшей задержки на вызов в вендоренном режиме соберите один раз локальный
бинарник в gitignore:

```sh
go build -o .cenvkit.bin ./cmd/cenvkit   # .cenvkit.bin is gitignored
./.cenvkit.bin compose up
```

**Требования к среде:** `docker compose` (≥ 2.24) для команд, касающихся compose
(`compose`, `validate`). Режимы `env-debug` **работают без демона** — им не нужен
Docker.

---

## 3. Быстрый старт

Из каталога вашего проекта:

```sh
# 1. Seed env files from the committed example.* templates (never clobbers).
cenvkit init
#    -> creates .env, .dev.env, .secrets.env, ... from example.env, example.dev.env, ...

# 2. Fill in values (real secrets go in .secrets.env, which stays gitignored).
#    Put anything you reference as ${VAR} in your compose YAML into the chain
#    (.env / .dev.env / .secrets.env) — NOT into a service env_file.
$EDITOR .env .secrets.env

# 3. See the resolved chain (one path per line, in load order).
cenvkit env-files

# 4. Render the compose config.
cenvkit compose config

# 5. Anything you'd run with `docker compose`, run with `cenvkit compose`.
cenvkit compose up -d
cenvkit compose logs -f web

# 6. When a value surprises you, ask why (and catch the env_file gap).
cenvkit env-debug --var APP_PORT
```

Золотое правило: **в интегрированном проекте идите через `cenvkit compose`**, чтобы
Layer-1 chain (токены, порядок, секреты последними, оверлей `COMPOSE_FILE`)
применялась согласованно. И: **если значение нужно на этапе интерполяции
`${VAR}`, положите его в chain** — сервисный `env_file:` настраивает только этот
контейнер.

---

## 4. env-chain против сервисных env_files

Есть две разные вещи, и cenvkit держит их раздельно.

### 4.1 Проектная chain (Layer 1) — это и есть `COMPOSE_ENV_FILES`

Перечисляется в `.cenvkit.envchain` (один шаблон пути на строку; комментарии `#` и
пустые строки игнорируются). Если файла нет, встроенное значение по умолчанию
такое:

```
.env             # non-secret defaults (committed via example.env)
.${CENVKIT_ENV}.env   # per-environment overlay: .dev.env / .prod.env / …
.secrets.env     # secrets, gitignored — listed LAST so it wins (last-wins)
```

Типичная явная chain с машинным слоем:

```
.env
.${ENV}.env
.${HOSTNAME}.env
.secrets.env
```

**Это — и только это — становится `COMPOSE_ENV_FILES`**, контекстом, который
`docker compose` использует для интерполяции `${VAR}` в вашем YAML.

**Токены**, подставляемые в каждую запись: `${ENV}` и `${CENVKIT_ENV}` →
разрешённое окружение; `${HOST}` и `${HOSTNAME}` → имя хоста машины. Оба
**санитизируются** до `[A-Za-z0-9._-]` (имя хоста с `|`/`&`/`,` не может внедрить
путь или разбить список файлов). Несуществующие файлы молча пропускаются.

- **Разрешение `CENVKIT_ENV`:** shell `CENVKIT_ENV` > строка `CENVKIT_ENV=` в
  `.env` > `"dev"`.
- **`HOST`/`HOSTNAME`:** экспортированный `HOSTNAME` > команда `hostname`.

```sh
$ cenvkit env-files
/app/.env
/app/.dev.env
```

### 4.2 Сервисный `env_file:` — только runtime

`env_file:` сервиса настраивает **контейнер этого сервиса** во время выполнения
(по сервисам, изолированно) — нативный Docker. cenvkit **не** добавляет его в
`COMPOSE_ENV_FILES`, поэтому он **не** питает интерполяцию `${VAR}`. Если `${VAR}`
в вашем YAML удовлетворяется только сервисным `env_file:`, запуск откатывается к
`:-default` (см. §7, как это обнаружить).

cenvkit всё равно *знает* о сервисных env_files (он загружает настоящую
include-aware модель) — но только для питания `env-debug` (gap-детектора и
runtime-представления `--files`), никогда для запуска.

---

## 5. Команды

Глобальные флаги: `--project-dir <dir>` — каталог проекта (по умолчанию текущий
каталог; всё разрешается относительно него). `--color auto|always|never` (по
умолчанию `auto`) — см. §11.

| Команда | Что делает |
|---|---|
| `cenvkit compose <args…>` | собрать Layer-1 chain, задать `COMPOSE_ENV_FILES`, `exec docker compose <args…>` |
| `cenvkit env-files` | напечатать разрешённый `COMPOSE_ENV_FILES` (**только Layer 1**), по одному абсолютному пути на строку |
| `cenvkit env-debug […] [--json]` | исследовать chain + сервисное env с provenance / обнаружением gap (в процессе, без демона) — см. §7 |
| `cenvkit validate [--all]` | `docker compose config -q`; `--all` валидирует и dev, и prod |
| `cenvkit init` | засеять `.X` из `example.X` (**no-clobber**), развернуть на один уровень каталогов |
| `cenvkit version` | напечатать версию |

### `cenvkit compose`

Ядро. Собирает `COMPOSE_ENV_FILES` (Layer 1) и делает exec в `docker compose`,
пробрасывая все аргументы; код выхода распространяется.

```sh
cenvkit compose config
cenvkit compose up -d
cenvkit compose --project-dir services/web ps   # run against a subproject
```

### `cenvkit env-files`

```sh
$ cenvkit env-files
/app/.env
/app/.dev.env
```

Точные файлы (и порядок), которые питают интерполяцию. Заметьте: **пути сервисных
env_file здесь не появляются** — они только runtime (смотрите их через
`env-debug --show files`).

### `cenvkit validate`

```sh
cenvkit validate          # validate the currently-resolved CENVKIT_ENV
cenvkit validate --all    # validate dev AND prod (re-resolves the chain per env)
```

Ненулевой код выхода при невалидной конфигурации. `--all` заново разрешает
Layer-1 chain для каждого окружения (чтобы уровни `.dev.env`/`.prod.env`
подхватывались правильно).

### `cenvkit init`

Засевает `.<name>` из каждого `example.<name>` в каталоге проекта **без
перезаписи** существующего файла (поэтому он никогда не уничтожает заполненный
`.secrets.env`), затем разворачивается на один уровень в непосредственные
подпроекты. Без `sudo`, без `chmod`, никогда не пишет секреты на диск сверх
копирования шаблона из примера.

```sh
$ cenvkit init
# example.env       -> .env        (created)
# example.dev.env   -> .dev.env    (created)
# .secrets.env      -> left untouched (already exists)
# web/example.env   -> web/.env    (created, fan-out)
```

### `cenvkit run`

Выполнить любую команду с окружением объединённой chain — локальное «без-docker»
плечо, чтобы та же chain, что питает compose, управляла вашими тестами, скриптами
и инструментами:

```sh
cenvkit run -- npm run dev           # the chain env, injected into the process
cenvkit run -e prod -- ./migrate     # with the prod tier
cenvkit run --no-expand -- printenv  # leave ${VAR} literal
cenvkit run --print                  # dump what it WOULD inject (dotenv), no exec
```

`--expand` (по умолчанию) раскрывает `${VAR}` / `${VAR:-def}` через dotenv-движок
compose-go — идентично тому, что интерполирует `docker compose`. Окружение shell
побеждает chain (паритет с compose). Коды выхода пробрасываются: код дочернего
процесса, либо **127** (бинарник не найден) / **126** (не исполняемый) /
**128+signo** (сигнал). `--` обязателен перед командой.

### `cenvkit env`

Вывести окружение объединённой chain для CI, скриптов или `eval` — только ключи из
chain, отсортированы:

```sh
cenvkit env                           # dotenv (KEY=VALUE)
cenvkit env --format json             # a JSON object
eval "$(cenvkit env --format shell)"  # load into the current shell (export …)
```

`cenvkit env --expand` байт-в-байт совпадает с `env-debug --show effective` и с
`docker compose config` (один движок раскрытия). `-e <env>` / `--no-expand`
работают как в `run`.

### `cenvkit gap-report`

CI / pre-build линт по тому же gap, который обнаруживает `env-debug` — без демона,
поэтому запускается перед `docker build`:

```sh
cenvkit gap-report          # exit 1 = a ${VAR} is env_file-only; 0 = clean; 2 = no compose file
cenvkit gap-report --json   # {"gaps":[{var,service,field,fallback,fix}],"count":N}
```

Встройте его в CI, чтобы ломать сборку, когда `${VAR}` молча откатился бы к своему
`:-default`, потому что значение живёт только в сервисном `env_file:`.

### Именованные chain

`.cenvkit.envchain` может содержать необязательные секции `[name]`;
`--chain <name>` (в каждой команде) выбирает одну — по умолчанию используется
список без заголовка / `[default]`:

```
[default]
.env
.${CENVKIT_ENV}.env
.secrets.env

[ci]
.env
.ci.env
```

Секции автономны (без наследования от `[default]`) и ортогональны `CENVKIT_ENV`
(`--chain ci` × `CENVKIT_ENV=prod` независимы). Неизвестный `--chain` завершает
работу кодом 2 со списком доступных имён.

### Секреты — вне области ответственности

cenvkit **не** управляет секретами, не маскирует и не шифрует их. `.secrets.env`
просто загружается **последним** в chain (last-wins); на диск ничего не пишется.
Для настоящего управления секретами оберните cenvkit внешним инструментом:
`sops exec-env -- cenvkit run -- <cmd>`.

---

## 6. Монорепо

cenvkit загружает **настоящую include-aware** compose-модель, поэтому понимает
весь проект — но помните правило из §4: интерполяцию питает только **проектная
chain**; сервисные `env_file:` работают только runtime.

### Корень оркестрирует подпроекты (единый стек)

Корневой `docker-compose.yml`, который через `include:` включает каждый подпроект:

```yaml
# docker-compose.yml
include:
  - ./web
  - ./api
  - ./services/reports
```

Если `web/docker-compose.yml` содержит `ports: "${WEB_PORT:-0}:80"`, а `WEB_PORT`
живёт только в `web/.web.env` (сервисный `env_file:`), то при запуске
**`${WEB_PORT}` откатывается к `0`** — `env_file:` работает только в runtime.
Контейнер всё равно получает `WEB_PORT` из своего env_file, но интерполяция YAML
его не видит.

**Два правильных варианта:**

1. **Вы хотите, чтобы `${WEB_PORT}` интерполировался** (например, это публикуемый
   порт): положите его в **Layer-1 chain** — корневой `.env`, chain-файл
   подпроекта или `.secrets.env`. Тогда он питает интерполяцию везде.
2. **Он действительно нужен только runtime** (просто env-переменная, которую
   читает контейнер): оставьте его в сервисном `env_file:` и не ссылайтесь на
   него как `${...}` в YAML.

Используйте `env-debug`, чтобы увидеть, какой это случай (§7):

```sh
$ cenvkit env-debug --show files | sed -n '1,12p'
interpolation (COMPOSE_ENV_FILES):
  /app/.env
  /app/.dev.env
runtime-only (service env_file: — NOT interpolated, container env only):
  api:
    /app/api/.api.env
  web:
    /app/web/.web.env
    /app/web/.web.dev.env
```

### Изолированный подпроект

Запуск изнутри подпроекта (или с указанием на него) разрешает **его собственную**
chain и набор сервисных env_file, не видя соседей:

```sh
cd web && cenvkit compose config      # web/'s own chain
cenvkit --project-dir web env-files   # same, without cd
```

### dev / prod одной ручкой

Управляйте оверлеями одним `CENVKIT_ENV`. Селектор `COMPOSE_FILE` вида
`docker-compose.yml:docker-compose.${CENVKIT_ENV}.yml` интерполируется cenvkit
(см. §9), а уровни chain для окружений (`.dev.env`/`.prod.env`) подхватываются:

```sh
cenvkit compose config                      # dev (default)
CENVKIT_ENV=prod cenvkit compose config     # prod overlay + .prod.env tiers
```

---

## 7. `env-debug` — provenance, фильтры и gap-детектор

`env-debug` отвечает на вопрос *«почему у этой переменной такое значение, где оно
вступает в силу и что реально получит контейнер?»* — целиком **в процессе (без
демона Docker)**.

**Виды выводятся каскадом.** `--show` можно повторять; секции идут в заданном
порядке (без `--show` — список Layer-1). Фильтры сужают каждый вид, у которого
есть данные.

| `--show …` | Что показывает |
|---|---|
| `list` (по умолчанию) | файлы Layer-1 chain в порядке загрузки (секреты последними) |
| `files` | две группы: **интерполяция** (`COMPOSE_ENV_FILES`, Layer 1) и **runtime-only** (пути `env_file:` по сервисам) |
| `overview` | **слои** окружения: пофайловый обход (`+`/`~`/`·`, сырые значения) + слои `env_file:` по сервисам + `inline environment:`, со строками `⚠ gap` |
| `effective` | **финальное окружение контейнера** каждого сервиса с источником каждого значения |
| `trace` | история одной переменной: победитель, затенённые файлы, эффекты — или **gap**, если она только в env_file |
| `deps` | **замыкание зависимостей** `--var`: определения, прямые/обратные ссылки, сервисы-потребители |

`--value --var V` — некаскадный примитив: печатает голое значение интерполяции
(пусто, если `V` только в env_file) и не комбинируется с другими видами.

| Фильтр | `list` | `files` | `overview` | `effective` | `trace` | `deps` | `json` |
|---|---|---|---|---|---|---|---|
| `--var KEY` | файлы, где объявлен | то же | только его строки | только его запись | сама переменная | обязателен | `vars` + записи |
| `--service S` | - | этот сервис | этот сервис | этот сервис | его эффекты | - | его блок |

Умолчания: `--var V` без вида = `--show trace`; `--service S` без вида =
`--show effective`; `--json` без `--show` сохраняет полный документ, а
`--json --show …` печатает только запрошенные секции. Невозможные комбинации дают
**exit 2**; вид, чей фильтр ничего не нашёл, — **exit 1** (запросы `--trace`/
`--value` по-прежнему exit 0 и отвечают `not set`/пусто).

### `trace` — переменная цепочки (без gap)

```sh
$ cenvkit env-debug --var COMPOSE_PROJECT_NAME
COMPOSE_PROJECT_NAME=monorepo
  winner:     /app/.env (layer1)
```

### `trace` — gap из env_file

```sh
$ cenvkit env-debug --var WEB_PORT
WEB_PORT
  interpolation: NOT in the Layer-1 chain -> ${WEB_PORT} falls back at run time
  runtime:       /app/web/.web.env -> WEB_PORT=18080  (service `web` container env only)
  ⚠ gap: ${WEB_PORT} used in service web environment[1] resolves to "0" at the run, NOT the env_file value (defined only in a service env_file).
  ⚠ gap: ${WEB_PORT} used in service web ports[0] resolves to "0:80" at the run, NOT the env_file value (defined only in a service env_file).
  fix:   add WEB_PORT to the Layer-1 chain (e.g. .env), or use it runtime-only.
```

Читается так: `WEB_PORT` **нет** в цепочке интерполяции, поэтому каждый
`${WEB_PORT}` в YAML откатывается к fallback при запуске; он *есть* в
`web/.web.env` (значение 18080), но это попадает только в контейнер `web`;
исправление — поднять его в Layer 1 (или принять, что он runtime-only).

### `deps` — определения и все потребители

`--show deps --var V` отвечает на обратный вопрос, недоступный фильтру: где `V`
определён, что нужно его значению, какие ключи ссылаются на него (транзитивно) и
какие сервисы его получают.

```sh
$ cenvkit env-debug --show deps --var LIVEKIT_KEY --redact
LIVEKIT_KEY
  defined in:
    .secrets.env (layer1)
    services/livekit-api/.livekit-api.env (env_file, service livekit-api)
    services/token-exchange/.token-exchange.env (env_file, service token-exchange)
  depends on:
    LKA_LIVEKIT_KEY (depth 1)
    TOKEN_EXCHANGE_LIVEKIT_KEY (depth 1)
  needed by:
    .env:LKA_LIVEKIT_KEY (depth 1)
    .env:TOKEN_EXCHANGE_LIVEKIT_KEY (depth 1)
    livekit/.livekit.env:KEY (depth 1, service livekit)
  consumed by:
    service livekit-api via services/livekit-api/.livekit-api.env
    service token-exchange via services/token-exchange/.token-exchange.env
```

Рёбра берутся из сырых значений каждого слоя: `${VAR}`, `${VAR:-def}`, `$VAR` и
вложенные default (`${A:-${B}}`). Незакрытая `${A` остаётся литералом, ребро не
выдумывается. `--service` граф не сужает.

### `overview` — все слои разом

Когда нужна полная картина — вклад каждого файла и что он затеняет —
`--show overview` обходит файлы с маркерами `+` новый / `~` переопределение
(`old → new`) / `·` повтор, показывая **сырые** значения (`${...}` не раскрыты).
Добавьте `--var`, чтобы сузить обход до одного ключа.

```sh
$ cenvkit env-debug --show overview
env overview — myproject (mode: overview)
  CENVKIT_ENV = dev (from .env)
  Project dir = /app

Interpolation chain (COMPOSE_ENV_FILES)
  + new   ~ override   · repeat

  /app/.env
      + COMPOSE_PROJECT_NAME = monorepo
      + SITE_URL = example.com
  /app/.dev.env
      + IS_DEV = true
      ~ SITE_URL = example.com → dev.example.com

Runtime-only — service env_file: (NOT interpolated)
  web:
    /app/web/.web.env
        + WEB_PORT = 18080
    /app/web/.web.dev.env
        + WEB_DEBUG = true
    inline environment:
        ~ WEB_PORT = 18080 → 0
    ⚠ gap: WEB_PORT — used as ${WEB_PORT} in service web (environment[1], ports[0])
      but NOT in the Layer-1 chain → run falls back.
```

Читается так: цепочка задаёт `SITE_URL` в `.env` и **переопределяет** его в
`.dev.env` (маркер `~` — last-wins внутри Layer-1); контейнер `web` получает
`WEB_PORT=18080` из своего `env_file:`, но inline `environment:` переопределяет его
на fallback `${WEB_PORT:-0}` (`0`) — и поскольку `WEB_PORT` нет в цепочке, каждый
`${WEB_PORT}` в YAML откатывается при запуске (gap).

### `effective` — финальное окружение сервиса с источниками

```sh
$ cenvkit env-debug --show effective --service web
service web:
  IS_DEV=true       <- (inline environment:) (environment)
  STACK_TIER=dev    <- (inline environment:) (environment)
  WEB_DEBUG=true    <- /app/web/.web.dev.env (env_file)
  WEB_PORT=0        <- (inline environment:) (environment)
```

Это **правда о контейнере**: `WEB_DEBUG` приходит из env_file; `WEB_PORT=0` —
значение inline `environment:` (интерполировано против Layer 1 → fallback `:-0`),
которое **переопределяет** `18080` из `web/.web.env`. Inline `environment:` всегда
побеждает `env_file:`, а inline `${VAR}` интерполируется против цепочки — поэтому
`env-debug` показывает то, что контейнер *реально* получит, а не значение, которого
запуск не даст.

### `--redact` и `--json`

`--redact` заменяет значения на `<redacted>`, сохраняя ключи, файлы, слои и
заголовок `CENVKIT_ENV` — используйте перед копированием вывода куда-либо. `--json`
содержит `files` (Layer-1 `COMPOSE_ENV_FILES`), `chain_files`, по переменным
`{value, in_chain, winner, overridden, runtime_defs, effects[], gap}`, по сервисам
`services[]` (записи с источниками + объявленные `env_files`) и, с `--show deps`,
секцию `deps`. Без `--show` это полный документ; с `--show` — только запрошенные
секции. Эффекты сообщаются **только для полей сервисов**
(`services.<name>.<field>`).
---

## 8. CI

Собственный CI cenvkit (хороший шаблон) выполняет на каждый push/PR:

```yaml
# .github/workflows/ci.yml (essentials)
env:
  GO_VERSION: "1.26.11"        # exactly the version in go.mod
  COMPOSE_VERSION: "v5.8.0"    # pinned; bump version + sha256 in one commit
jobs:
  lint:
    steps:
      - run: go vet ./...
      - run: gofmt -l . | (! grep .)            # fail if anything is unformatted
      - run: |                                   # compose-go seam: only internal/engine may import it
          MOD=$(go list -m)
          go list -f '{{.ImportPath}} {{join .Imports " "}}' ./... \
            | grep -v "^$MOD/internal/engine " | grep 'compose-spec/compose-go' \
            && { echo "compose-go leaked"; exit 1; } || echo "seam OK"
      - run: |                                   # linter built with the project's Go
          go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2
          "$(go env GOPATH)/bin/golangci-lint" run ./...
      - run: |                                   # shipped shell: syntax + installer self-test
          for f in cenvkit scripts/ci/*.sh; do sh -n "$f"; done
          sh scripts/ci/cenvkit-install-selftest.sh
  test:
    strategy: { matrix: { os: [ubuntu-latest, macos-latest] } }
    steps:
      - run: go build ./...
      - run: go test ./... -count=1
  acceptance:
    steps:
      - run: |                                   # pinned Compose, checksum verified before use
          curl -fsSL "https://github.com/docker/compose/releases/download/$COMPOSE_VERSION/docker-compose-linux-x86_64" -o /tmp/dc
          echo "<sha256 of that asset>" | sha256sum -c -
          install -m 0755 /tmp/dc ~/.docker/cli-plugins/docker-compose
      - run: docker compose version
      - run: go test ./test/... -v -count=1       # docker-gated acceptance
```

The real workflow additionally runs `goreleaser check` and a `release-snapshot` dry
run (all six targets built before anything is tagged), plus a non-blocking job that
runs the same acceptance against upstream-latest Compose to report drift.

В вашем собственном проекте полезный гейт — `cenvkit validate` (или
`validate --all`). Поскольку `env-debug` работает без демона, вы также можете
утверждать конкретные разрешения — и отлавливать gap в env_file — без Docker:

```sh
test "$(cenvkit env-debug --value --var SITE_URL)" = "example.com"
# Fail CI if a var your YAML references is only in a service env_file (a gap):
cenvkit env-debug --var APP_PORT --json | grep -q '"gap": *true' \
  && { echo "APP_PORT is env_file-only — promote it to the chain"; exit 1; } || true
```

---

## 9. Контракты поведения (на что можно полагаться)

- **`env_file:` — только runtime.** `${VAR}`, определённый только в сервисном
  `env_file:`, **не** разрешается при запуске (нативный fallback `:-default`);
  контейнер всё равно получает переменную. `env-debug` помечает gap. Положите
  переменную в **Layer-1 chain**, чтобы она питала интерполяцию `${VAR}`.
- **Run-путь = только Layer 1.** `cenvkit env-files` / `cenvkit compose` задают
  `COMPOSE_ENV_FILES` только из Layer-1 chain; путь сервисного `env_file:` туда
  никогда не попадает.
- **Отсутствующий `env_file:`.** Отсутствующий *обязательный* `env_file:`
  лоялен при перечислении (поэтому `env-debug` никогда не падает) и **фатален в
  upstream при реальном запуске `docker compose`** (который заново применяет
  `required:`). cenvkit никогда не перереализует семантику `required:` из upstream.
- **Приоритет переменных.** Приоритетом владеет `docker compose` — last-wins по
  `COMPOSE_ENV_FILES`. cenvkit управляет только **порядком файлов** Layer 1:
  порядок chain, `.secrets.env` последним. Поскольку `env_file:` больше не входит
  в `COMPOSE_ENV_FILES`, сервисный `env_file:` больше не может переопределить
  переменную chain/секрет во время интерполяции. (Во время выполнения inline
  `environment:` по-прежнему переопределяет `env_file:` внутри сервиса — нативно.)
- **Оверлеи `COMPOSE_FILE`.** Селекторы вида
  `docker-compose.yml:docker-compose.${CENVKIT_ENV}.yml` работают: cenvkit
  интерполирует `${CENVKIT_ENV}`/`${ENV}` и разбивает по
  `COMPOSE_PATH_SEPARATOR` (иначе разделитель списка путей ОС — никогда `,`).
- **`COMPOSE_DEPTH`** **принимается, но игнорируется** — include-граф делает
  старое обнаружение через глоб с ограничением глубины устаревшим; переменная
  допускается (без ошибки).
- **Никакого избыточного обнаружения.** compose-файл не из `include:`-графа /
  `COMPOSE_FILE` никогда не загружается; наличие `.git` для обнаружения не важно.
- **Детерминизм.** Разрешённый список файлов, группы `--files` и JSON-вывод
  упорядочены детерминированно (по имени сервиса, затем по объявленному порядку),
  поэтому стабильны между запусками и безопасны для diff/снимков.
- **Точность отладчика.** `env-debug` никогда не сообщает разрешение, которого не
  даст реальный запуск — `--trace`/`--effective` разрешаются против того же
  контекста интерполяции Layer 1, что использует запуск.

---

## 10. Устранение неполадок

**`${VAR}` разрешается в свой fallback `:-default`.**
Запустите `cenvkit env-debug --var VAR`. Если он показывает **gap**
(`NOT in the Layer-1 chain`), значение живёт только в сервисном `env_file:` — это
runtime-only и никогда не питает интерполяцию. Исправление: добавьте `VAR` в
Layer-1 chain (например, `.env`) или перестаньте ссылаться на неё как `${VAR}` в
YAML.

**Значение в контейнере не такое, какое я задал в env_file.**
`cenvkit env-debug --show effective --service S` показывает финальное окружение
контейнера с источником каждого ключа. Помните: inline `environment:` переопределяет
`env_file:`, а inline `${VAR}` интерполируется против chain (поэтому inline
`"${X:-0}"` может переопределить `X` из env_file на fallback).

**`cenvkit env-files` не перечисляет мой сервисный `env_file:`.**
Так задумано — `env-files` работает только с Layer-1 (`COMPOSE_ENV_FILES`).
Сервисные env_files только runtime; смотрите их через `cenvkit env-debug --show files`
(группа runtime-only).

**Мой секрет переопределили.**
`.secrets.env` идёт последним *внутри Layer-1 chain* (там он побеждает). Сервисный
`env_file:` больше не влияет на интерполяцию, поэтому не может затереть секрет на
этапе `${VAR}`. Если значение всё ещё неверное, `env-debug --var VAR`
покажет победителя в chain.

**`cenvkit compose` / `validate` падает с «Cannot connect to the Docker daemon».**
Этим командам нужен `docker compose`. `env-debug` — нет; используйте его для
инспекции без Docker.

**Ошибка `required` для env_file при `cenvkit compose up`, хотя `env-files` в порядке.**
Это по замыслу: перечисление лояльно (пропускает отсутствующий файл), но
реальный запуск `docker compose` применяет `required:`. Создайте файл или задайте
`required: false`.

**`cenvkit init` не перезаписал мой `.env`.**
Так задумано — `init` никогда не затирает существующий файл. Удалите его, если
нужен свежий посев.

**Лишний `compose.yaml` не подхватывается в монорепо.**
cenvkit идёт по `include:`-графу, а не по глобу имён файлов. Добавьте подпроект в
корневой `include:` (или `COMPOSE_FILE`).

---

## 11. Цвет

Человекочитаемый вывод цветной — маркеры (`+` зелёный / `~` жёлтый / `·` тусклый),
заголовки секций (циан), имена сервисов (маджента), `⚠ gap` (красный), ok/fail в
`validate` (зелёный/красный), ошибки (красный). Представления
chain/overview/effective/trace все его используют.

Цвет **автоматический и безопасный**:

- **Включён**, когда stdout — терминал; **выключен** при пайпе или перенаправлении
  (поэтому `cenvkit env-files | …` и захваченный вывод остаются чистыми).
- `--color=never` отключает; `--color=always` форсирует (даже через пайп).
- `NO_COLOR` (любое значение) отключает; `CLICOLOR_FORCE`/`FORCE_COLOR` форсируют.
  CI трактуется как non-TTY, поэтому логи CI автоматически простые.
- **`--json` никогда не цветной**, независимо от `--color` — машинный вывод
  остаётся чистым для инструментов.

```sh
cenvkit env-debug --show overview                 # colored on a terminal
cenvkit env-debug --show overview --color=never   # force plain
cenvkit env-debug --show overview --color=always | less -R   # keep color through a pager
NO_COLOR=1 cenvkit env-debug --show overview      # plain via the env convention
```

---

Краткий справочник на одну страницу — [`cenvkit.md`](cenvkit.md).
