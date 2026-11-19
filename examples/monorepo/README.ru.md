[English](README.md) · **Русский**

# Блюпринт монорепо — корень оркестрирует подпроекты

Полный запускаемый пример топологии **«корневой `include:` подпроектов»**:
корневой compose, который через `include:` втягивает два подпроекта (`web/`,
`api/`), плюс общая сеть. Он демонстрирует gap, который нативный compose
оставляет открытым, и то, как его выявляет `cenvkit env-debug`: `${WEB_PORT}`
живёт только в `web/.web.env` (сервисный `env_file:`) — этот файл
**runtime-only**, поэтому интерполяция YAML откатывается к `:-0`. `env-debug`
говорит вам точно, что происходит и что исправить.

Блюпринт управляется целиком через **`cenvkit`** (Go CLI) — здесь нет ни
Makefile, ни вендоренных скриптов-обёрток.

```
examples/monorepo/
├── docker-compose.yml         # root: include:s web/ api/ reports/ + a shared network (for the tools service)
├── docker-compose.dev.yml     # dev overlay  ┐ selected by COMPOSE_FILE's
├── docker-compose.prod.yml    # prod overlay ┘ ${CENVKIT_ENV} token
├── .cenvkit.envchain          # root Layer-1 chain (.env → .${ENV}.env → .${HOSTNAME}.env → .secrets.env)
├── example.env                # root non-secret defaults (cenvkit init → .env)
├── example.dev.env            # root dev tier (→ .dev.env);  IS_DEV=true
├── example.prod.env           # root prod tier (→ .prod.env); IS_DEV=false
├── web/
│   ├── docker-compose.yml     # service `web`, env_file: [./.web.env, ./.web.${CENVKIT_ENV}.env]
│   ├── .web.env               # WEB_PORT=18080  (defined ONLY here, runtime-only)
│   └── .web.dev.env / .web.prod.env   # per-service tier (WEB_DEBUG), by ${CENVKIT_ENV}
├── api/
│   ├── docker-compose.yml     # service `api`, env_file: [./.api.env], "${API_PORT:-0}:80"
│   └── .api.env               # API_PORT=19090  (defined ONLY here, runtime-only)
└── services/reports/          # nested deeper (legacy services/<svc>/ shape)
    ├── docker-compose.yml     # service `reports`, env_file: [./.reports.env]
    └── .reports.env           # REPORTS_PORT=15151  (defined ONLY here, runtime-only)
```

> Это **исходный блюпринт**. Настоящие `.env`-файлы засеваются из закоммиченных
> шаблонов `example.*` командой `cenvkit init` (no-clobber) — именно это и делает
> `test/smoke-monorepo.sh`.

---

## Два слоя (Layer 1 против сервисного env_file:)

| Слой | Что наполняет | Питает ли интерполяцию `${VAR}`? |
|---|---|---|
| Layer-1 chain (`COMPOSE_ENV_FILES`, `.cenvkit.envchain`) | корневой `.env`, `.dev.env`, `.secrets.env`, … | **да** |
| Сервисный `env_file:` (`web/.web.env`, `api/.api.env`, …) | runtime-окружение каждого контейнера | **нет** |

`WEB_PORT=18080` живёт только в `web/.web.env` — сервисном `env_file:`. При
интерполяции на этапе compose `${WEB_PORT}` отсутствует в Layer-1 chain, поэтому
он откатывается к `:-0`. Контейнер всё равно получает `WEB_PORT=18080` в runtime
(через сервисный `env_file:`). `cenvkit env-debug` показывает именно это.

---

## Запуск

### A. Единый стек, из корня

```sh
cd examples/monorepo

# 1. Seed the real env files from the example.* templates (no-clobber).
cenvkit init                 # → .env, .dev.env, .prod.env (never clobbers)

# 2. See the Layer-1 chain (these are the files that feed interpolation).
cenvkit env-files
#   …/examples/monorepo/.env
#   …/examples/monorepo/.dev.env

# 3. See BOTH groups — interpolation chain AND runtime-only service env_files:
cenvkit env-debug --files
#   interpolation (COMPOSE_ENV_FILES):
#     …/examples/monorepo/.env
#     …/examples/monorepo/.dev.env
#   runtime-only (service env_file: — NOT interpolated, container env only):
#     api:
#       …/examples/monorepo/api/.api.env
#     reports:
#       …/examples/monorepo/services/reports/.reports.env
#     web:
#       …/examples/monorepo/web/.web.env
#       …/examples/monorepo/web/.web.dev.env

# 4. Render the unified config. ${WEB_PORT} falls back to 0 (Layer-1 only).
cenvkit compose config | grep -E 'WEB_PORT|API_PORT|published'
#   WEB_PORT: "0"          ← :-0 fallback; WEB_PORT is not in the Layer-1 chain
#   API_PORT: "0"          ← same
#     published: "0"

# 5. Validate (docker compose config -q):
cenvkit validate
```

Нативный compose из корня даёт при fallback точно такой же результат — gap — это
нативное поведение Docker, а не то, что добавляет cenvkit:

```sh
docker compose config | grep -E 'WEB_PORT|API_PORT|published'
#   WEB_PORT: "0"        ← same :-0 fallback
#   API_PORT: "0"
```

### B. Изолированно, по подпроектам (каждый работает сам по себе)

Подпроект запускается независимо — укажите `cenvkit` на его каталог. Всё
разрешается относительно этого каталога проекта:

```sh
# From the subproject directory:
cd examples/monorepo/web
cenvkit env-files
#   …/web/.env    (if it exists after init in the subproject)

# …or without changing directory, with --project-dir:
cenvkit --project-dir examples/monorepo/web env-files
```

Запуск из корня (A) и изолированный запуск (B) полностью независимы: каждый вызов
разрешает *свою собственную* chain и пути `env_file:` относительно своего каталога
проекта.

---

## Детектор gap: env-debug

`cenvkit env-debug` работает без демона — Docker не нужен.

```sh
# Trace WEB_PORT: see the gap (runtime-only, falls back at interpolation time).
cenvkit env-debug --trace --var WEB_PORT
# WEB_PORT
#   interpolation: NOT in the Layer-1 chain -> ${WEB_PORT} falls back at run time
#   runtime:       …/web/.web.env -> WEB_PORT=18080  (service `web` container env only)
#   ⚠ gap: ${WEB_PORT} used in service web environment[1] resolves to "0" at the run, NOT the env_file value (defined only in a service env_file).
#   ⚠ gap: ${WEB_PORT} used in service web ports[0] resolves to "0:80" at the run, NOT the env_file value (defined only in a service env_file).
#   fix:   add WEB_PORT to the Layer-1 chain (e.g. .env), or use it runtime-only.

# Show the final container env for the web service (with sources):
cenvkit env-debug --effective --service web
# service web:
#   IS_DEV=true	<- (inline environment:) (environment)
#   STACK_TIER=dev	<- (inline environment:) (environment)
#   WEB_DEBUG=true	<- …/web/.web.dev.env (env_file)
#   WEB_PORT=0	<- (inline environment:) (environment)
```

Вывод `--effective` показывает **правду о контейнере**: `WEB_PORT=0` приходит из
inline `environment: WEB_PORT: "${WEB_PORT:-0}"`; это значение интерполируется
против Layer-1 chain, где `WEB_PORT` нет, поэтому побеждает fallback `:-0`. Это
переопределяет `18080` из `web/.web.env` (inline `environment:` всегда побеждает
`env_file:`).

**Чтобы `${WEB_PORT}` интерполировался** (например, чтобы публиковать настоящий
порт): добавьте `WEB_PORT=18080` в Layer-1 chain (корневой `.env` или `web/.env`).
Тогда и `env-files`, и `env-debug --trace` покажут его как переменную chain без gap.

---

## dev / prod и переопределения для конкретной машины

Одна ручка `CENVKIT_ENV` (shell > `.env` > `dev`) управляет всем разделением:

```sh
cenvkit init                 # seeds .dev.env / .prod.env from the templates

CENVKIT_ENV=prod cenvkit compose config | grep STACK_TIER
#   STACK_TIER: prod      ← docker-compose.prod.yml overlay (COMPOSE_FILE selector)
```

Правки под конкретную машину (уровень логов, локальные пути) идут в необязательный
`.${HOSTNAME}.env`, который chain уже перечисляет — он побеждает общий `.env`, но
остаётся **ниже** `.secrets.env`. Никогда не кладите секрет в host-файл.

Необязательные сервисы прячутся за профилем (`COMPOSE_PROFILES` передаётся в
compose без изменений). В блюпринте поставляется сервис `tools`, по умолчанию
выключенный:

```sh
cenvkit compose config --services                       # web, api
COMPOSE_PROFILES=tools cenvkit compose config --services # web, api, tools
```

---

## Проверка

`test/smoke-monorepo.sh` прогоняет весь этот блюпринт end-to-end против бинарника
`cenvkit` — подтверждая, что `env-files` возвращает только Layer-1, что
`${WEB_PORT}` откатывается к `0` (runtime-only gap) и что `env-debug --trace`
корректно сообщает о gap:

```sh
sh test/smoke-monorepo.sh
```
