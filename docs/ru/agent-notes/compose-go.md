[English](../../agent-notes/compose-go.md) · **Русский**

# compose-go — рабочие факты

Проверено на пине `github.com/compose-spec/compose-go/v2 v2.11.0` (`go.mod`) и
живых пробах. Относится к `internal/engine/**`.

## Границы и пин

- compose-go импортирует ТОЛЬКО `internal/engine`, включая подпакеты `cli`,
  `loader`, `dotenv`, `template`, `types`. Гейты: `make seam` и шаг CI
  `compose-go seam check` (`go list -f ...`, без `-deps`).
- Остальные пакеты видят plain-Go `Result`/`ProjectView` (`engine/engine.go`),
  поэтому `cmd` и `internal/provenance` от compose-go не зависят.
- Версия осознанно пинована (`cenvkit version` печатает её из build info,
  `composeGoVersion`): бампишь — прогоняй acceptance. upstream-first: сами
  считаем только выбор `COMPOSE_FILE` (нужен до загрузки) и `Flatten`.

## Загрузка проекта

```go
opts, err := cli.NewProjectOptions(configs,
    cli.WithWorkingDirectory(dir),
    cli.WithEnv(env),                  // seed интерполяции
    cli.WithDefaultProfiles(profiles...),
    cli.WithResolvedPaths(true),       // EnvFile.Path абсолютный
    cli.WithInterpolation(true),
    cli.WithoutEnvironmentResolution,  // «голый» ProjectOptionsFn, без скобок
)
proj, err := opts.LoadProject(ctx)
```

- `cli.ProjectFromOptions` — deprecated; используй
  `NewProjectOptions(...).LoadProject`.
- `Project.Services` — только АКТИВНЫЙ набор; профильно-выключенные уходят в
  `DisabledServices`. Это map → для детерминизма сортируй имена.
- `ServiceConfig.EnvFiles []EnvFile{Path, Required OptOut, Format}`; `OptOut`
  по умолчанию TRUE — отсутствующий обязательный `env_file:` = ошибка загрузки.
- `ServiceConfig.Environment` — `map[string]*string` (`nil` = «unset»).
- Порядок опций значим: `ProjectOptionsFn` применяются как переданы.

## Почему COMPOSE_FILE считается вручную

`cli.WithConfigFileEnv` не годится как единственный путь: (1) читает
`o.Environment["COMPOSE_FILE"]`, а тот пуст без `cli.WithEnv` ДО него;
(2) НЕ интерполирует `${VAR}` в значении; (3) относительные пути раскручивает
от cwd процесса, а не от `WithWorkingDirectory`.

Поэтому `internal/engine/discover.go`:

- нет `COMPOSE_FILE` → стандартное обнаружение
  `cli.NewProjectOptions(nil, WithWorkingDirectory(dir), WithDefaultConfigPath)`,
  берём `opts.ConfigPaths` (стандартные имена + поиск вверх);
- есть → режем сами по `COMPOSE_PATH_SEPARATOR`, иначе по
  `os.PathListSeparator` (никогда `,`), подставляем `${ENV}`→`${CENVKIT_ENV}`,
  интерполируем `template.SubstituteWithOptions(...)`, склеиваем с `dir` и
  отдаём как `configs`. Отсутствующие явные файлы не фильтруем — ошибку должен
  дать загрузчик.

`HasComposeFileEnv` — только проба наличия; и гейт, и загрузчик зовут ОДИН
`resolveComposeFiles`, чтобы не разъезжаться.

## dotenv: три разных чтения

| API | Где | Семантика |
|---|---|---|
| `dotenv.GetEnvFromFile(base, files)` | `flatten.go`, `Flatten` | только значения файлов; `base` — источник подстановки, в результат НЕ копируется; полная база видна каждому файлу; отсутствующий файл = ОШИБКА, не skip |
| `dotenv.ParseWithFormat(...)` | `envfile.go`, `parseEnvFile` | пофайловая атрибуция: lookup = external → этот файл → предыдущие; `""` = dotenv, `"raw"` = Docker CLI `kvfile` |
| `dotenv.ReadFile(path, lookupFn)` | `provenance.go` | отдаёт map; при `nil` lookup пишет в stderr предупреждения о неразрешённых `${VAR}` — передавай замыкание |

- Парсер compose-go отдаёт НЕупорядоченную map и раскрывает `${...}`; для
  `--overview` и `--no-expand` есть свой `engine.ParseOrderedLiteral`.
- `GetEnvFromFile` безусловно логирует `level=warning ... variable is not set`
  (logrus, отключить нельзя): значение верное, но stderr `run`/`env` шумит.

## Интерполяция в env_file (паритет с Compose)

Compose интерполирует **значения** `env_file`. Это отдельно от интерполяции YAML
и не противоречит правилу runtime-only (значения env_file никогда не питают
`${VAR}` в модели):

- с Compose v2.2.0 (compose-go v1.0.8: `resolveEnvironment` ->
  `dotenv.ParseWithLookup`); unquoted и double-quoted значения раскрываются,
  одинарные кавычки остаются литералом (спека: "Env_file format");
- `format: raw` (v2.30.0+) полностью отключает интерполяцию файла;
  `required: false` — v2.24.0+;
- внутри файла видны только строки ВЫШЕ (`B=${A}` до `A=...` даёт warning и
  пустое значение); по списку более поздние файлы видят более ранние, а для
  ключа побеждает последний файл;
- приоритет с Compose v2.35.0 (compose-go v2.5.0, "Fix interpolation priorities"):
  project env (shell, `.env`, `--env-file`, `COMPOSE_ENV_FILES`) > явный
  `environment:` > ранее прочитанные env_file-значения; до v2.35.0 порядок был
  обратным;
- литеральный `$` в unquoted/double-quoted значении раскрывается (`pa$word` ->
  `pa` + warning): одинарные кавычки, экранирование `$$` или `format: raw`;
- `parseEnvFile` моделирует тот же порядок (lookup: внешнее -> этот файл ->
  предыдущие), поэтому `--show effective` совпадает с `docker compose config`
  (`TestEnvDebug_EffectiveMatchesComposeEnvFileInterpolation`).

## Шаблоны и интерполяция

- `template.Mapping = func(string) (string, bool)`; `bool` = «переменная есть».
  `${V:-d}` при `!ok` даёт `d`, `${V}` без default — пустую строку.
- `template.SubstituteWithOptions(s, mapping, template.WithoutLogging)` — без
  `WithoutLogging` неразрешённые переменные печатаются в stderr.
- `template.ExtractVariables(dict, template.DefaultPattern)` (если понадобится)
  даёт только ИМЕНА без путей — пути по словарю обходи сам. Текущие `Effects`
  строятся через `SubstituteWithOptions` с записывающим замыканием.
- Подстановка считает «исполненные» обращения: переменные из невыбранных ветвей
  `${A:+...}` не учитываются. `Provenance` строит `Effects` именно так.

## B-lite provenance: raw-load + обход

- **RAW-загрузка:** `loader.LoadConfigFiles(ctx, configs, projectDir)` → задать
  `details.Environment` → `LoadModelWithContext` с `SkipInterpolation`,
  `SkipValidation`, `SkipConsistencyCheck`, `SkipResolveEnvironment`.
  `LoadConfigFiles` env не принимает.
- **Ловушка нормализации:** интерполирующая загрузка разворачивает короткие
  формы (`ports` → `{published,target}`, `environment` map → список
  `"KEY=VALUE"`) — читать тот же путь из интерполированного словаря нельзя.
  `walkDict` обходит сырой словарь (dotted + `[i]`), лист резолвится на месте.
- **Форма пути зависит от мержа:** один файл → `environment.API_PORT` (`0`);
  сервис, чей `environment:` переоткрыт overlay-файлом → `environment[1]` со
  значением `"WEB_PORT=0"`. Новый ключ, сортирующийся раньше, СДВИГАЕТ индекс
  (acceptance это пинит) — смотри фактический `--json`.
- **Область:** `splitServiceField` берёт только `services.<name>.<field>`;
  `${VAR}` в `networks`/`volumes`/`configs`/`secrets`/`x-*` намеренно не
  попадает в `Effects` (сужение B-lite, не баг).
- **C:** объявленные `svc.EnvFiles` читаются по порядку (missing пропускается),
  затем inline `svc.Environment` (`nil` — удалить). Список объявленных файлов
  хранится ОТДЕЛЬНО от per-key `Entries`: inline-оверрайд может съесть все
  ключи файла, но файл обязан остаться в `ServiceEnv.EnvFiles` (иначе врёт
  `--files`).
- **Gap:** `referenced && !InChain && len(RuntimeDefs) > 0`; эффекты и
  runtime-defs сортируются для детерминированного JSON.

## D1: лояльное перечисление

`cli.WithoutEnvironmentResolution` — обёртка над
`loader.Options.SkipResolveEnvironment`. Без неё `LoadProject` падает на
отсутствующем `required:` env_file; с ней пути `svc.EnvFiles` сохраняются, а
несуществующие мы отбрасываем сами (`os.Stat`).

- `SkipValidation` НЕ помогает; `WithDiscardEnvFile` — не тот рычаг (ОЧИЩАЕТ
  `EnvFiles`). Под рычагом env_file не читается, `svc.Environment` остаётся
  inline-only — это и делает возможной атрибуцию C.
- Пути `env_file:` интерполируются и под рычагом (из `cli.WithEnv`, т.е. из
  Layer-1), но НЕ из значений чужих сервисов.

## include и overlay

- `include:` импортирует сервис, и главный файл НЕ вправе его переопределять.
  Строгий Compose: `services.X conflicts with imported resource`; терпимый
  движок молча мержит → «зелено локально, красно в CI».
- `-f`-overlay (список `COMPOSE_FILE`) — другой механизм: мержит сервис в ОДНО
  определение, переоткрывать сервис в overlay законно. Обнаружение — по
  `include:`/`COMPOSE_FILE`, не рекурсивным glob; `COMPOSE_DEPTH` игнорируется.
  `env_file: required:` и `include:` требуют Docker Compose v2.24+.

## Чеклист подводных камней

- `${VAR}` внутри YAML flow-sequence ломает парсер («did not find expected
  ',' or ']'»): путь `env_file:` с токенами пиши блочно (`- path: ...`).
- Отсутствующий `env_file:` — ошибка `LoadProject`; лояльно только наше
  перечисление, реальный `docker compose` идёт без рычага.
- Значения service-`env_file:` в интерполяцию не попадают никогда: не смешивай
  контексты (это баг, ради которого инструмент существует).