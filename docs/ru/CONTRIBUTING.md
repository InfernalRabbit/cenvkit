[English](../../CONTRIBUTING.md) · **Русский**

# Как внести вклад в cenvkit

Спасибо, что нашли время помочь проекту. cenvkit — небольшой CLI на Go,
который собирает проектную цепочку окружений для Docker Compose и объясняет
разрыв между значениями `env_file:` и подстановкой ${VAR} на этапе
compose.

- Справочник по командам и поведению: [`docs/cenvkit.md`](cenvkit.md)
- Полное руководство пользователя: [`docs/guide.md`](../guide.md)
- Архитектура и семантические границы: [`AGENTS.md`](../../AGENTS.md) —
  прочитайте перед изменением кода.

## Прежде чем начать

- Перед созданием issue поищите существующие issue и документацию.
- Для крупного изменения (новая команда или флаг, контракт поведения,
  обновление зависимости) сначала откройте issue или черновик PR.
- **Одна логическая правка = один PR.** Не смешивайте в одном pull request
  рефакторинг, новую функциональность и форматирование.
- Вопросы безопасности не место в публичном issue — см. [`SECURITY.md`](SECURITY.md).
- Участие регулируется [`CODE_OF_CONDUCT.md`](CODE_OF_CONDUCT.md).

## Требования

- **Go** — ровно та версия, что объявлена в [`go.mod`](../../go.mod) и
  закреплена как `GO_VERSION` в
  [`.github/workflows/ci.yml`](../../.github/workflows/ci.yml); не меняйте
  директиву `go` попутно.
- **POSIX `sh`** — отслеживаемый лаунчер `cenvkit` и все скрипты
  `scripts/ci/*.sh` написаны на POSIX sh, а не на bash. Держите их
  чистыми для `sh -n`.
- **Docker Compose** — только для команд, работающих с compose, и
  acceptance-набора (`cenvkit compose`, `cenvkit validate`,
  `go test ./test/...`). Acceptance-тесты работают только с конфигурацией
  и не требуют демона, но проверяют `docker compose version`.
- **golangci-lint v2.12.2**, собранный проектным Go (см. ниже). Предсобранный
  бинарь, собранный более старым Go, отказывается работать с этим модулем.
- Python и Node не нужны.

## Локальная разработка

```sh
git clone https://github.com/InfernalRabbit/cenvkit.git
cd cenvkit

go build ./...
go run ./cmd/cenvkit version
make dev-build          # local binary at .cenvkit.bin (gitignored)
```

Запускайте CLI из исходников через `go run ./cmd/cenvkit <args>` или
`./.cenvkit.bin <args>`. Отслеживаемый корневой файл `cenvkit` —
это POSIX-sh лаунчер: никогда не перезаписывайте его собранным бинарём.

## Локальный гейт

Изменение готово, когда на финальном дереве проходят все проверки. Запускайте
проверку форматирования **после** последней правки — `go test` не
проверяет форматирование:

```sh
gofmt -l .                  # must print nothing
go vet ./...
make seam                   # only internal/engine may import compose-go
go test ./... -count=1
make ci                     # vet + fmt-check + seam + build + test + lint (if installed)
```

`-count=1` указан намеренно: он отключает кеш результатов тестов,
чтобы прежний `ok` не мог заменить реальный прогон. `make ci`
запускает `golangci-lint` только если бинарь есть в `PATH`;
«зелёный» прогон без него не является полной проверкой линтером.

Запускайте линтер локально так же, как CI: линтер собран проектным Go, а
`GOTOOLCHAIN` закреплён на версии из `go.mod`:

```sh
go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2
GOTOOLCHAIN=go$(go list -m -f '{{.GoVersion}}') golangci-lint run ./...
```

`make lint` выполняет то же закрепление (он читает версию из
`go.mod`). Закрепление важно в обе стороны: линтер со старым Go не
может линтить этот модуль, а линтер, собранный проектным Go, не может проверять
типы более новой stdlib.

Прогон без Docker (docker-gated проверки пропускаются сами):

```sh
SMOKE_SKIP_DOCKER=1 go test ./... -count=1
```

### Acceptance-набор (обязателен для изменений Compose)

Изменения, затрагивающие `internal/engine`, сборку цепочки для
`compose`/`env-files` или обработку
`include:`/`env_file:`, также требуют acceptance-набора — с
доступным Docker Compose и без `SMOKE_SKIP_DOCKER`:

```sh
docker compose version
go test ./test/... -v -count=1
```

Пропущенная Docker-проверка не подтверждает паритет с Compose. CI запускает
этот набор против **закреплённого** релиза Compose (версия и sha256 в
[`.github/workflows/ci.yml`](../../.github/workflows/ci.yml)); не
предполагайте, что используется версия, встроенная в раннер.

`TestMain` acceptance-набора собирает собственный временный CLI,
поэтому предсобранный бинарь не нужен; фикстуры копируют
[`examples/monorepo/`](../../examples/monorepo/) и заполняют dot-env
файлы из `example.*`. Если вы меняете подсчитываемые проверки,
обновите `declaredAssertions` и заголовок «Current assertion count» во
второй строке
[`test/cenvkit-acceptance_test.go`](../../test/cenvkit-acceptance_test.go)
вместе.

Если вы трогаете лаунчер, установщик или другой скрипт `scripts/ci/*.sh`,
сохраняйте шелл-гейт зелёным:

```sh
for f in cenvkit scripts/ci/*.sh; do sh -n "$f"; done
sh scripts/ci/cenvkit-install-selftest.sh
```

Изменения конфигурации релиза (`.goreleaser.yaml`, release-джобы в
[`.github/workflows/ci.yml`](../../.github/workflows/ci.yml)) также
требуют `goreleaser check`; это отдельная джоба CI.

## Сообщения коммитов

Используйте [Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/):

```text
<type>(<optional scope>): <summary>
```

- Типы: `feat`, `fix`, `docs`, `test`,
  `refactor`, `perf`, `build`, `ci`,
  `chore`.
- Scope, где полезно: `chain`, `engine`, `envfiles`,
  `provenance`, `bootstrap`, `cli`, `docs`,
  `ci`.
- Сводка в повелительном наклонении, со строчной буквы, без точки в конце.
- Ломающие изменения помечайте `!` и объясняйте в футере
  `BREAKING CHANGE:`.
- В теле объясняйте **почему**; diff уже показывает **что**.

Примеры (только формат):

```text
feat(cli): add --json to the example command
fix(engine): keep service env_file paths out of the run chain
docs: clarify the interpolation/runtime split
test: cover an empty named chain
```

## Pull request'ы

1. Сделайте форк репозитория и ветку от `main`; держите ветку
   сфокусированной.
2. Внесите изменение и прогоните локальный гейт выше.
3. Заполните
   [`.github/PULL_REQUEST_TEMPLATE.md`](../../.github/PULL_REQUEST_TEMPLATE.md).
4. PR, меняющий поведение, включает:
   - тест, который падает до изменения и проходит после (юнит-тест и/или
     acceptance-проверка);
   - запись в `CHANGELOG.md` в разделе `[Unreleased]`
     (`Added` / `Changed` / `Fixed` / `Removed`);
   - обновление документации в `docs/cenvkit.md` и/или
     `docs/guide.md`, если меняются команда, флаг, exit-код или
     контракт поведения.
5. PR, связанные с Compose: укажите, какую acceptance-команду вы запускали, и
   приложите результат.
6. CI должен быть зелёным. Docker-acceptance-джоба запускается против
   закреплённого релиза Compose; неблокирующая джоба `compose-latest`
   лишь сообщает о дрейфе относительно upstream.
7. Не включайте секреты, значения `.secrets.env`, токены или реальные
   имена хостов; в фикстурах используйте фиктивные значения.

Мейнтейнер может попросить изменения, разделить смешанный PR или выполнить
squash ветки при мерже. Пожалуйста, закрывайте или разрешайте треды ревью.

## Документация и переводы

- [`docs/cenvkit.md`](cenvkit.md) — одностраничный справочник по командам и
  поведению; [`docs/guide.md`](guide.md) — полное руководство пользователя.
- [`docs/agent-notes/`](agent-notes/) содержит долговременные заметки
  по реализации (факты о compose-go, семантика окружений, ловушки тестирования
  и CI, известные грабли). Это заметки, а не спецификация — проверяйте
  утверждения по текущему коду и тестам.
- [`docs/integrations/gitlab-offline.md`](integrations/gitlab-offline.md)
  описывает офлайн-использование в CI.
- Русские зеркала лежат в `docs/ru/`; документы сообщества —
  [CONTRIBUTING](CONTRIBUTING.md), [SECURITY](SECURITY.md) и
  [CODE_OF_CONDUCT](CODE_OF_CONDUCT.md). Держите
  переключатель языка в первой строке, порядок разделов и заголовки
  согласованными с английским файлом, а команды, флаги, пути, имена переменных,
  exit-коды и код-блоки копируйте байт-в-байт, не переводя.

## Issues и сообщения об уязвимостях

- Баг: используйте форму «Bug report»
  ([`.github/ISSUE_TEMPLATE/bug_report.yml`](../../.github/ISSUE_TEMPLATE/bug_report.yml)).
  Укажите `cenvkit version`, ОС/arch, точный вызов, ожидаемое и
  фактическое поведение и вывод, снятый с `--color=never`.
- Функциональность: используйте форму «Feature request»
  ([`.github/ISSUE_TEMPLATE/feature_request.yml`](../../.github/ISSUE_TEMPLATE/feature_request.yml)).
- Уязвимость: сообщайте приватно, никогда в публичном issue — см.
  [`SECURITY.md`](SECURITY.md).

## Лицензия

Внося вклад, вы соглашаетесь, что ваши изменения лицензируются по
[лицензии MIT](../../LICENSE), той же, что и проект.
