[English](../../AGENTS.md) · **Русский**

# AGENTS.md — cenvkit

## Разработка и проверка

- Используйте версию Go из `go.mod` (сейчас 1.26.11). Локальный запуск исходников —
  `go run ./cmd/cenvkit <args>`; `make dev-build` собирает `.cenvkit.bin`.
  Отслеживаемый в git корневой `cenvkit` — это POSIX-sh лаунчер: никогда не
  перезаписывайте его бинарником.
- `make ci` прогоняет vet, форматирование, import-seam, сборку и тесты; lint
  выполняется только если установлен. CI требует golangci-lint v2.12.2, собранный
  проектным Go: `go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2`,
  затем `golangci-lint run ./...`. Линтер, собранный более старым Go, отвергает этот
  модуль; и наоборот, если `go` на машине НОВЕЕ, чем в go.mod, линтер, собранный
  проектным Go, не может typecheck-нуть его стандартную библиотеку. Поэтому
  `make lint`/`make ci` пинят `GOTOOLCHAIN` на версию из go.mod.
- Набор без Docker: `SMOKE_SKIP_DOCKER=1 go test ./... -count=1`.
  Точечно: `go test ./internal/engine -run '^TestFlatten_' -count=1`.
- Изменения, затрагивающие compose, требуют также `go test ./test/... -v -count=1`
  при доступном Docker Compose и снятом `SMOKE_SKIP_DOCKER`. Гейт проверяет
  `docker compose version`; эти конфиг-онли тесты не требуют демона. Пропущенные
  Docker-ассерты не доказывают паритет с Compose. CI ставит **пиннутый** релиз Compose
  (`COMPOSE_VERSION` + sha256 в `ci.yml`) — не полагайтесь на версию, встроенную в
  раннер, и бампайте пин осознанно (версия и контрольная сумма одним коммитом) после
  успешного прогона. Неблокирующий job `compose-latest` сообщает о дрейфе upstream.
- После последней правки Go перепроверяйте `make fmt-check`. При изменениях
  release-конфига запускайте `goreleaser check` (отдельный CI job).
- Поставляемый shell — POSIX `sh` (`cenvkit`, `scripts/ci/*.sh`): держите его чистым
  по `sh -n` и запускайте `sh scripts/ci/cenvkit-install-selftest.sh` при правке
  установщика (CI делает и то, и другое).
- Acceptance `TestMain` собирает собственный временный CLI; предсобранный бинарник
  не нужен. Фикстуры копируют `examples/monorepo` и сидят dot-env файлы из `example.*`.
  При изменении счётчика ассертов обновляйте `declaredAssertions` и количество в
  строке 2 `test/cenvkit-acceptance_test.go` вместе.
- `make demo` готовит временную копию blueprint и инициализирует её; используйте его
  для демонстрации gap-debugger, не заполняя отслеживаемый каталог примеров.

## Архитектура и семантические границы

- `cmd/cenvkit/main.go` собирает команды Cobra. `compose`/`env-files` вызывают
  `chain.Resolve` → `envfiles.Assemble(cr.Files, nil)`; service env-файлы никогда не
  должны попадать в run-цепочку, хотя `Assemble` принимает Layer 2.
- `compose` отключает разбор флагов Cobra, чтобы пробрасывать docker-флаги; новые
  флаги-обёртки нужно явно извлекать в этой команде, иначе они утекут в Docker Compose.
- **Только `internal/engine` может импортировать compose-go**, включая его
  dotenv/template подпакеты (`make seam` это проверяет). Держите семантику Compose
  здесь; осознанные бампы compose-go требуют acceptance-проверки.
- `engine.Flatten` — общий примитив раскрытия для `run`/`env` (через `internal/envmap`)
  и debug-интерполяции. Маленький dotenv-ридер в `chain` — только селектор/сид, а не
  авторитетный движок раскрытия. `engine.ProvInput.Env` принимает процессное окружение,
  никогда не смешанный сид `chain.Result.Vars`.
- `internal/provenance` владеет plain-Go отчётами/рендерингом; `engine` их строит.
  Отладка и gap-lint грузят include-aware модель в процессе, без Docker. Обнаружение
  идёт по `include:`/`COMPOSE_FILE`, а не рекурсивными глобами; `COMPOSE_DEPTH`
  принимается, но игнорируется.
- Сохраняйте разделение интерполяции и рантайма: `${VAR}` в YAML читает Layer-1
  проектную цепочку или shell, **не** `env_file:` сервиса. Входы интерполяции
  поднимайте в Layer 1; runtime-only гэпы ищите через `gap-report` или
  `env-debug --var V`. `gap-report` выходит 0 чисто / 1 гэпы / 2 нет
  compose-файла. `env-debug --show deps --var V` обходит рёбра интерполяции
  (`Report.Refs`, из сырых значений слоёв) в обе стороны; `--service` граф не сужает.
  `compose` не запускает gap-lint автоматически; вызывайте отдельно.
- Команды blueprint проводите через `cenvkit compose`: сырой Compose теряет сборку
  проектной цепочки. `--project-dir` по умолчанию cwd; подпроекты изолированы.

## Контракты конфигурации

- `.cenvkit.envchain` по умолчанию `.env` → `.${CENVKIT_ENV}.env` → `.secrets.env`;
  отсутствующие записи пропускаются, порядок — last-wins. Кастомные цепочки сохраняют
  объявленный порядок: секреты — последними. Управление секретами/маскирование/
  шифрование вне scope.
- Приоритет селектора: непустой shell `CENVKIT_ENV` > корневой `.env` > `dev`.
  `${ENV}` — алиас `${CENVKIT_ENV}`; токены host/env в путях санитизируются.
  Удалённые имена `COMPOSE_ENV`/`.docker-env-chain` не возрождать.
- `--chain <name>` выбирает самостоятельную секцию (без наследования из default),
  независимо от селектора окружения; неизвестное имя — exit 2.
- `run` требует `--` перед командой ребёнка. `run`/`env` — shell-wins; `env` печатает
  только ключи из цепочки. `--no-expand` отключает раскрытие, а не приоритет.
- `COMPOSE_ENV_FILES` разделяется запятыми; запятые в путях цепочки отклоняются.
  `COMPOSE_FILE` использует `COMPOSE_PATH_SEPARATOR` или разделитель списка путей ОС.

## Ссылки

- Справочник команд/поведения: `docs/cenvkit.md`; примеры: `docs/guide.md`
  (русские версии — в `docs/ru/`).
- Долговременные заметки по реализации — `docs/agent-notes/` (факты о compose-go,
  семантика окружений, грабли тестов/CI, известные ловушки). Это заметки, не спека:
  проверяйте каждое утверждение по текущему коду и тестам.
