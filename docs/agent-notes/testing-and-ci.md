# Тесты, гейты и CI

## Локальный гейт (DoD)

```sh
go build ./...
go vet ./...
gofmt -l .                                       # должно быть пусто
go test ./... -count=1
SMOKE_SKIP_DOCKER=1 go test ./... -count=1       # docker-free подмножество
go test ./test/... -v -count=1                   # с доступным docker compose
golangci-lint run ./...                          # см. «версионные ловушки»
goreleaser check                                 # при правке release-конфига
```

`make ci` (`Makefile`) = `vet fmt-check seam build test` (+lint, если бинарь
найден). При этом `make lint`/`make ci` МОЛЧА пропускают golangci-lint, если
его нет в PATH — «полный зелёный» на такой машине недостоверен.

**Зелёный без docker-пути не считается полным:** `SMOKE_SKIP_DOCKER=1`
пропускает docker-gated ассерты, а они проверяют реальную парность с compose.
Пропущенный ассерт ничего не доказывает.

## gofmt — после ПОСЛЕДНЕЙ правки

`go test` форматирование не проверяет. Классический промах: гейт прогнан,
сделана ещё одна правка — и `gofmt -l .` уже не пуст. Перед «зелено»
перезапускай `make fmt-check` (или `gofmt -l .`) на финальном дереве.

## Версионные ловушки

### golangci-lint

- Собирай линтер ПРОЕКТНЫМ Go:
  `go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2`.
  Предсобранный бинарь со старым Go отказывается линтить модуль с новым
  `go` в `go.mod` («the Go language version used to build golangci-lint is
  lower than the targeted Go version»). CI делает именно `go install`.
- Локально дефолтный `go` может быть НОВЕЕ проектного → линтер падает на
  typecheck stdlib. Запускай с `GOTOOLCHAIN=go<версия из go.mod>` (сейчас 1.26.3).
- Линтер ПЕЧАТАЕТ НЕ ВСЁ: `max-same-issues`/`max-issues-per-linter` режут
  список. «11 issues» может означать ~40 сайтов — доводи до истинного `0`.
- Конфиг v2-формата (`.golangci.yml`): `linters.enable` (errcheck, govet,
  ineffassign, staticcheck, unused) + `formatters.enable` (gofmt).
- Грязный кэш даёт ложные срабатывания (например, пачку `SA5011` в тестах).
  Лечится `golangci-lint cache clean`, а не правкой кода.

### Docker Compose

- Локальный и CI-движок — РАЗНЫЕ версии: CI ставит latest с релизов, локально
  то, что принесла установка. Терпимость к `include:` + переопределению
  импортированного сервиса различается: терпимый движок молча мержит, строгий
  падает (`services.X conflicts with imported resource`).
- Следствие: зелёный локальный acceptance не доказывает зелёный CI. Для
  изменений, трогающих compose, смотри `docker compose version` в логе и
  проверяй на строгом движке (пинованный контейнер), а не только на своём.
- Acceptance-тесты конфиг-онли (демон не нужен), но `dockerAvailable()` требует
  успешного `docker compose version`.

## Кэш `go test`

Go переиспользует кэш результатов `go test`, когда входы не менялись, а CI
гоняет тесты без `-count=1`. `dockerAvailable()` — внешняя команда, в ключ
кэша она не входит, поэтому кэшированный PASS способен скрыть, что docker-gated
ассерты вообще не выполнялись. Локально всегда `-count=1`.

## Acceptance-харнесс (`test/`)

- `TestMain` собирает СВОЙ бинарь (`go build -o <tmp>/cenvkit ../cmd/cenvkit`):
  предсобранный не нужен. Сборка идёт в общий кэш, поэтому на холодной машине
  возможен флейк при параллельном `go test ./...`; воспроизводился не всегда —
  держи в уме при диагностике «моргающих» падений.
- `stageMonorepo` копирует `examples/monorepo` в `t.TempDir()` и сидит
  корневые dotfiles из `example.*`. Исходную фикстуру не мутируем никогда.
- `dockerAvailable()`: `SMOKE_SKIP_DOCKER=1` → false (короткое замыкание),
  иначе однократная проба `docker compose version` (`sync.Once`). Так suite
  корректно скипается на хосте без docker.
- Хелперы: `runCenvkit` (CombinedOutput), `runCenvkitSplit` (stdout/stderr/exit
  раздельно — для негативных проверок), `envWithout(...)` (окружение процесса
  минус ключи — для дочернего бинаря).

## Счётчик ассертов

- `const declaredAssertions = <N> + reviewRegressionAssertions` — единственный
  источник истины. Заголовок файла (строка 2, «Current assertion count: N.»)
  проверяется `TestAssertionCountHeader`: меняешь одно — меняй второе.
- Считаются АССЕРТЫ, а не `func Test`: функций всегда меньше, чем ассертов
  (один сценарий даёт несколько). Это не расхождение само по себе.
- Историческая болезнь — устаревшие числа в других комментариях того же файла.
  После правки ищи по всему файлу:
  `grep -nE '[0-9]+ (total|assertion|count)|count: [0-9]+' test/cenvkit-acceptance_test.go`
  и сверяй каждое (осторожно с `0o644` — это не счётчик).

## CI (`.github/workflows/ci.yml`)

| Job | Что делает |
|---|---|
| `lint` | `go vet`, gate по `gofmt -l`, seam-проверка compose-go, `golangci-lint` через `go install` |
| `goreleaser-check` | `goreleaser check` |
| `test` (ubuntu + macos) | `go build`, `go test ./...` с `SMOKE_SKIP_DOCKER=1` |
| `acceptance-docker` | ставит latest Compose, печатает версию, `go test ./test/... -v` |
| `acceptance-no-docker` | `go test ./test/... -v` с `SMOKE_SKIP_DOCKER=1` |

Seam-проверка: `go list -f '{{.ImportPath}} {{join .Imports " "}}' ./...` БЕЗ
`-deps` (с `-deps` в левой колонке оказываются подпакеты самого compose-go →
вечный false positive). Исключение — строка ровно `<module>/internal/engine `;
при утечке `exit 1`. `make seam` — зеркало этой проверки.

## Ловушки прогона

- Подпроцесс упал — печатай `CombinedOutput()`: обёрнутая ошибка «exit status 1»
  прячет настоящую причину (так однажды пропустили падение compose).
- Правишь тест, который кто-то правит параллельно: перечитай ТЕКУЩИЙ файл и
  прогони `-count=1`, иначе фиксишь по устаревшему ассерту.
- Меняется контракт поведения — просматривай ВСЕ `*_test.go`, а не только
  названный каталог: пакетные тесты часто шире, чем кажется по заданию.
- Не «зеленей» на `go test` без `gofmt` и без docker-пути: оба промаха уже
  случались и оба доезжали до CI.