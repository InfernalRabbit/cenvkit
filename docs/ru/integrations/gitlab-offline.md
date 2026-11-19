[English](../../integrations/gitlab-offline.md) · **Русский**

# Офлайн / зеркалированный CI (на примере GitLab)

Используйте это, когда раннеры не могут достучаться до GitHub Releases и Go module
proxy, но вы всё ещё хотите иметь гейт `gap-report` в CI. Схема такая:
**один раз зеркалируйте закреплённый релиз, устанавливайте из зеркала в каждом
джобе, проверяйте контрольную сумму**.

Всё ниже работает с любым хранилищем артефактов, которое отдаёт
`<MIRROR>/<version>/<asset>`; маршрут через GitLab использует Generic Package
Registry.

## 1. Структура зеркала

```
<MIRROR>/<version>/cenvkit_<version>_<os>_<arch>.tar.gz
<MIRROR>/<version>/checksums.txt
```

Для GitLab базовый URL такой:

```
${CI_API_V4_URL}/projects/${CI_PROJECT_ID}/packages/generic/cenvkit
```

поэтому `0.6.2` на Linux amd64 окажется в
`.../packages/generic/cenvkit/0.6.2/cenvkit_0.6.2_linux_amd64.tar.gz`. Это ровно та
структура, которую ожидает `scripts/ci/cenvkit-install.sh`.

## 2. Наполните зеркало

`scripts/ci/cenvkit-publish.sh` загружает каждый архив `cenvkit_*` плюс
`checksums.txt`, сначала проверяет локальный манифест, а затем доказывает, что
каждый файл можно получить. В CI заголовок авторизации —
`JOB-TOKEN: $CI_JOB_TOKEN`; с рабочей станции используйте
`PRIVATE-TOKEN: glpat-...` (токену нужен scope `api`).

```yaml
cenvkit:publish:
  stage: deploy
  image: alpine:3.20
  rules:
    - if: '$CENVKIT_PUBLISH'
      when: manual
  variables:
    CENVKIT_VERSION: "0.6.2"
  before_script:
    - apk add --no-cache curl tar unzip >/dev/null
  script:
    - |
      set -e
      mkdir -p dist && cd dist
      for f in checksums.txt \
               cenvkit_${CENVKIT_VERSION}_linux_amd64.tar.gz \
               cenvkit_${CENVKIT_VERSION}_linux_arm64.tar.gz \
               cenvkit_${CENVKIT_VERSION}_darwin_amd64.tar.gz \
               cenvkit_${CENVKIT_VERSION}_darwin_arm64.tar.gz \
               cenvkit_${CENVKIT_VERSION}_windows_amd64.zip \
               cenvkit_${CENVKIT_VERSION}_windows_arm64.zip ; do
        curl -fsSL -o "$f" \
          "https://github.com/InfernalRabbit/cenvkit/releases/download/v${CENVKIT_VERSION}/$f"
      done
    - cd ..
    - CENVKIT_API_TOKEN="JOB-TOKEN: $CI_JOB_TOKEN" sh scripts/ci/cenvkit-publish.sh dist
```

Это **единственный** джоб, которому нужен egress. Для полностью закрытого
контура запеките архивы в образ раннера и пропустите публикацию.

## 3. Установка и гейт в CI

```yaml
env-lint:
  stage: test
  image: alpine:3.20
  allow_failure: true           # make it blocking once the signal is trusted
  rules:
    - if: '$CENVKIT_ENV_LINT'   # opt in per project; skip until the mirror exists
  variables:
    CENVKIT_VERSION: "0.6.2"
    # One pin per architecture you actually run (see checksums.txt of the release).
    CENVKIT_SHA256_AMD64: "<sha256 of cenvkit_0.6.2_linux_amd64.tar.gz>"
    CENVKIT_SHA256_ARM64: "<sha256 of cenvkit_0.6.2_linux_arm64.tar.gz>"
  before_script:
    - apk add --no-cache curl tar unzip >/dev/null
  script:
    - export CENVKIT_MIRROR="${CI_API_V4_URL}/projects/${CI_PROJECT_ID}/packages/generic/cenvkit"
    - export CENVKIT_MIRROR_AUTH="JOB-TOKEN: $CI_JOB_TOKEN"
    - |
      case "$(uname -m)" in
        x86_64|amd64)  export CENVKIT_SHA256="$CENVKIT_SHA256_AMD64" ;;
        arm64|aarch64) export CENVKIT_SHA256="$CENVKIT_SHA256_ARM64" ;;
        *) echo "env-lint: unsupported arch $(uname -m)"; exit 2 ;;
      esac
      sh scripts/ci/cenvkit-install.sh /usr/local/bin
    - |
      if [ -f .env ]; then
        cenvkit gap-report --chain ci
      else
        echo "env-lint: no .env in this job - skipping"
      fi
```

Примечания:

- Вычисляйте `CENVKIT_MIRROR` в `script`, а не в `variables:`, чтобы не зависеть
  от того, раскрывает ли ваш GitLab переменные внутри определений.
- `gap-report` имеет смысл только тогда, когда chain-файлы, которые он читает,
  существуют в джобе. Без `.env` (или материализованного файла секретов) он
  сообщает об *отсутствии* chain, а не о реальных gap — отсюда защитная проверка.
- Коды выхода: `0` — чисто, `1` — найдены gap, `2` — compose-файл не найден (это
  ошибка конфигурации, а не успех).

## 4. Архитектура раннера

Контрольная сумма привязана к конкретному ассету, поэтому закрепите ту
архитектуру, на которой реально работает ваш раннер. `uname -m` внутри джоба (или
`$CI_RUNNER_EXECUTABLE_ARCH`) подскажет; хранить два пина и выбирать во время
выполнения — безопасное значение по умолчанию.

## 5. Устранение неполадок при первой публикации

| HTTP | Что означает | Где смотреть |
|---|---|---|
| 401 | токен отсутствует или истёк | заголовок должен быть `JOB-TOKEN: $CI_JOB_TOKEN` (CI) или `PRIVATE-TOKEN: ...`; не `Authorization` |
| 403 | токен есть, но не разрешён | CI/CD -> права job-токена; для персонального токена нужен `api` |
| 404 | самый частый и самый обманчивый | GitLab возвращает 404, когда вы не видите проект; проверьте **числовой** id проекта, путь с учётом регистра и что Packages включены |
| 413 | payload слишком велик | лимит размера реестра (наши архивы ~2.5 MB, маловероятно) |
| 500/502 | реестр недоступен | повторите; проверьте состояние компонента `packages` |

Проверка перед включением джоба (рабочая станция):

```sh
curl -sS -o /dev/null -w 'project: %{http_code}\n' \
  -H "PRIVATE-TOKEN: $GITLAB_PAT" "$CI_API_V4_URL/projects/$CI_PROJECT_ID"
# 200 -> token and id are right; 404 -> one of them is wrong
```

## 6. Гигиена секретов в гейт-джобе

Переменные типа `File` в GitLab **не** маскируются построчно, поэтому обычная
утечка — это строка в логе, а не реестр:

1. создайте файл с `umask 077` (0600 с первого байта, без гонки chmod);
2. `trap 'rm -f .secrets.env' EXIT HUP INT TERM`;
3. никакого `set -x`, никакого `CI_DEBUG_TRACE`; никогда не делайте `cat` файла;
4. `artifacts: exclude: [.secrets.env]` — иначе он уедет вместе с джобом;
5. никогда не кладите его в `cache: paths: [.]`;
6. `git check-ignore -q .secrets.env` как дешёвая защита от отредактированного
   `.gitignore`;
7. в этом джобе используйте только `gap-report` / `env-debug --show files` — `env`,
   `run --print` и `env-debug --trace` печатают значения дословно.

## 7. Поднятие закреплённой версии

1. опубликуйте новую версию (джоб `cenvkit:publish`) и подтвердите оба HTTP-кода;
2. обновите `CENVKIT_VERSION` и контрольные суммы по архитектурам в одном коммите;
3. прогоните гейт один раз; только потом делайте его блокирующим.

## 8. Что проверено

Сами скрипты покрыты `scripts/ci/cenvkit-install-selftest.sh` (собирает архив
формы релиза, раздаёт его по `file://`, утверждает, что установщик принимает
совпадающий sha256 и отвергает неверный) и запускаются в CI на каждый push.
`cenvkit-publish.sh` проверен локально против заглушки реестра (2xx-загрузка,
отказ при non-2xx, проверка доступности). Сами эндпоинты GitLab зависят от вашего
инстанса — коды и есть то, за чем нужно следить при первом запуске.
