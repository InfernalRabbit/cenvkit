**English** · [Русский](../ru/integrations/gitlab-offline.md)

# Offline / mirrored CI (GitLab shown)

Use this when runners cannot reach GitHub Releases and the Go module proxy, but
you still want the `gap-report` gate in CI. The pattern is: **mirror the pinned
release once, install from the mirror in every job, verify the checksum**.

Anything below works with any artifact store that serves
`<MIRROR>/<version>/<asset>`; the GitLab route uses Generic Package Registry.

## 1. Mirror layout

```
<MIRROR>/<version>/cenvkit_<version>_<os>_<arch>.tar.gz
<MIRROR>/<version>/checksums.txt
```

For GitLab the base is:

```
${CI_API_V4_URL}/projects/${CI_PROJECT_ID}/packages/generic/cenvkit
```

so `0.6.2` on Linux amd64 lands at
`.../packages/generic/cenvkit/0.6.2/cenvkit_0.6.2_linux_amd64.tar.gz`. That is
exactly the layout `scripts/ci/cenvkit-install.sh` expects.

## 2. Populate the mirror

`scripts/ci/cenvkit-publish.sh` uploads every `cenvkit_*` archive plus
`checksums.txt`, verifies the local manifest first, and then proves each file is
retrievable. Inside CI the auth header is `JOB-TOKEN: $CI_JOB_TOKEN`; from a
workstation use `PRIVATE-TOKEN: glpat-...` (token needs the `api` scope).

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

This is the **only** job that needs egress. For a fully closed contour, bake the
archives into the runner image and skip publishing.

## 3. Install and gate in CI

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

Notes:

- Compute `CENVKIT_MIRROR` in `script`, not in `variables:`, so you do not depend
  on whether your GitLab expands variables inside definitions.
- `gap-report` is meaningful only when the chain files it reads exist in the job.
  Without `.env` (or a materialised secrets file) it reports the *absence* of the
  chain, not real gaps — hence the guard.
- Exit codes: `0` clean, `1` gaps found, `2` no compose file found (a
  misconfiguration, not a pass).

## 4. Runner architecture

The checksum is per-asset, so pin the architecture your runner actually is.
`uname -m` inside the job (or `$CI_RUNNER_EXECUTABLE_ARCH`) tells you; keeping two
pins and selecting at runtime is the safe default.

## 5. Troubleshooting the first publish

| HTTP | Meaning | Where to look |
|---|---|---|
| 401 | token missing or expired | header must be `JOB-TOKEN: $CI_JOB_TOKEN` (CI) or `PRIVATE-TOKEN: ...`; not `Authorization` |
| 403 | token present, not allowed | CI/CD -> job token permissions; for a personal token it needs `api` |
| 404 | most common and misleading | GitLab returns 404 when you cannot see the project; check the **numeric** project id, the case-sensitive path, and that Packages are enabled |
| 413 | payload too large | registry size limit (our archives are ~2.5 MB, unlikely) |
| 500/502 | registry unavailable | retry; check the `packages` component status |

Sanity check before enabling the job (workstation):

```sh
curl -sS -o /dev/null -w 'project: %{http_code}\n' \
  -H "PRIVATE-TOKEN: $GITLAB_PAT" "$CI_API_V4_URL/projects/$CI_PROJECT_ID"
# 200 -> token and id are right; 404 -> one of them is wrong
```

## 6. Secrets hygiene in the gate job

GitLab `File`-type variables are **not** masked line by line, so the usual leak is
a log line, not the registry:

1. create the file with `umask 077` (0600 from the first byte, no chmod race);
2. `trap 'rm -f .secrets.env' EXIT HUP INT TERM`;
3. no `set -x`, no `CI_DEBUG_TRACE`; never `cat` the file;
4. `artifacts: exclude: [.secrets.env]` — otherwise it ships with the job;
5. never put it under `cache: paths: [.]`;
6. `git check-ignore -q .secrets.env` as a cheap guard against an edited
   `.gitignore`;
7. in this job use `gap-report` / `env-debug --files` only — `env`,
   `run --print` and `env-debug --trace` print values verbatim.

## 7. Bumping the pinned version

1. publish the new version (job `cenvkit:publish`) and confirm both HTTP codes;
2. update `CENVKIT_VERSION` and the per-arch checksums in the same commit;
3. run the gate once; only then make it blocking.

## 8. What is verified

The scripts themselves are covered by `scripts/ci/cenvkit-install-selftest.sh`
(builds a release-shaped archive, serves it over `file://`, asserts the installer
accepts a matching sha256 and rejects a wrong one) and run in CI on every push.
`cenvkit-publish.sh` is verified locally against a stubbed registry (2xx upload,
non-2xx failure, retrievability check). The GitLab endpoints themselves depend on
your instance — codes are the thing to watch on the first run.
