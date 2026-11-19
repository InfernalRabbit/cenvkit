**English** · [Русский](docs/ru/SECURITY.md)

# Security Policy

## Supported versions

cenvkit is pre-1.0. Only the latest minor release line receives security fixes.

| Version | Supported |
|---|---|
| 0.7.x (latest minor) | ✅ |
| < 0.7 | ❌ — upgrade to the latest release |

The current release is recorded in [`CHANGELOG.md`](CHANGELOG.md). If you are on
an older minor, reproduce the issue on the latest release before reporting it.

## Reporting a vulnerability

Do **not** open a public issue, discussion, or pull request for a suspected
vulnerability, and do not include exploit details in a public place.

Use GitHub's private vulnerability reporting:

1. Open the repository's **Security** tab.
2. Click **Report a vulnerability** (Security → Advisories).
3. Or go directly to
   <https://github.com/InfernalRabbit/cenvkit/security/advisories/new>.

If the repository shows no **Report a vulnerability** button, private reporting
is not available in your context; contact the maintainer through the GitHub
profile linked from the repository and ask for a private channel — without
posting details publicly.

Please include:

- `cenvkit version` output (both lines, if present);
- OS and architecture;
- the exact command line and the relevant project layout
  (`.cenvkit.envchain`, `COMPOSE_FILE`, compose YAML), with secrets redacted;
- expected vs actual behavior, with output captured using `--color=never`;
- a minimal reproduction — fixtures with dummy values are ideal;
- the impact you believe is reachable, and a suggested fix if you have one.

Never include real secrets, tokens, or third-party data.

## What to expect

This is a maintainer-run open-source project; the targets below are best effort,
not a contractual SLA.

| Stage | Target |
|---|---|
| Acknowledgment of the report | within 7 days |
| Initial triage and severity assessment | within 14 days |
| Fix or mitigation | depends on severity; status is kept in the advisory |
| Public disclosure | coordinated with the reporter, typically after a fix is released |

We will credit you in the advisory unless you ask us not to. There is no paid
bug bounty. Please allow a reasonable window — typically up to 90 days — before
public disclosure, and coordinate the timing with us.

## Scope

cenvkit is a **local CLI**. It reads the project env chain, merges and prints
values, and `exec`s `docker compose` or a child command. It runs no network
service and has no server-side component.

**In scope**

- File access outside the documented project-chain / compose model — for
  example, path handling that escapes the selected project directory, as
  opposed to an explicit `.cenvkit.envchain` entry, which the documentation
  treats as trusted configuration.
- Command or argument injection into the child process (`docker compose` or
  `cenvkit run --`) originating from env values or chain configuration.
- A bypass of the sanitization applied to `${ENV}`, `${HOST}`, and
  `${HOSTNAME}` tokens in chain paths.
- Crashes or unbounded resource use on malformed compose or env input,
  including the in-process compose-go load.
- Disclosure of secret values outside the documented verbatim output of
  `env`, `run --print`, and `env-debug --value|--trace` — for example through
  extra logs, error messages, or a rendered field where the documentation says
  the value is not shown.
- Weaknesses in the release and install path: checksum verification in
  [`scripts/ci/cenvkit-install.sh`](scripts/ci/cenvkit-install.sh), archive
  handling, and the published release artifacts.
- A dependency vulnerability with a reachable impact in cenvkit.

**Out of scope**

- **Secret management, masking, or encryption.** These are explicitly out of
  scope for cenvkit. `env`, `run --print`, and `env-debug --value|--trace`
  print values verbatim, including secrets, by design; that is not a
  vulnerability.
- **A `.cenvkit.envchain` you have not reviewed.** It is trusted project
  configuration, like a `Makefile`: entries may be absolute or point outside the
  project directory. Running cenvkit on an untrusted clone is covered by that
  documented trust model.
- **The `env_file:` interpolation gap itself.** Keeping service `env_file:`
  values runtime-only is deliberate Compose semantics; `gap-report` and
  `env-debug` surface the gap and do not change it.
- File permissions on your `.env`/`.secrets.env` files, or secrets already
  committed to your repository.
- Vulnerabilities in Docker Compose, Docker, or the Go toolchain — report those
  upstream.
- Social engineering, physical access, or issues that require an already
  compromised machine.

## Safe harbor

We consider good-faith security research authorized when it:

- stays within your own machine and projects, or a project you have permission
  to test;
- does not access, modify, or exfiltrate other people's data;
- does not degrade service or attack infrastructure you do not own.

We will not pursue legal action for research conducted under these conditions
and reported through the private channel above. Please give the maintainer a
chance to fix the issue before public disclosure.

## Related documents

- [`CONTRIBUTING.md`](CONTRIBUTING.md) — development setup, gates, and PR process.
- [`CODE_OF_CONDUCT.md`](CODE_OF_CONDUCT.md) — community standards.
- [`docs/guide.md`](docs/guide.md) — installation, the env chain, and behavior
  contracts.
