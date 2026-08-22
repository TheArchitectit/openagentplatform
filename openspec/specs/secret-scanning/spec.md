# Secret Scanning

> **Phase:** Infrastructure
> **STATUS: COMPLETE**
> **Source:** `scripts/scan-secrets.sh`, `.github/workflows/secret-validation.yml`, `.githooks/pre-commit`
> **App Path:** `scripts/scan-secrets.sh`

---

## Description

Secret scanning is the platform's defence against irreversible credential
exposure. OpenAgentPlatform is a **fully public repository**: anything committed
is world-readable forever and cannot be un-published, and automated harvesters
scrape public repos within minutes of a push. A leaked AWS key, database
password, JWT secret or private key is therefore an instant, permanent breach.

CI's gitleaks job catches secrets *after* a push — structurally too late. The
platform's answer is defence in depth at three positions:

1. **Pre-commit** — `scan-secrets.sh --staged` blocks the commit locally.
2. **Pre-push (release)** — `deploy.sh` runs the full-tree scan twice, before
   any commit and again immediately before `git push`.
3. **Post-push (CI)** — `secret-validation.yml` runs gitleaks over full history
   as the backstop.

`scan-secrets.sh` is intentionally self-contained — POSIX `grep`/`awk` only, no
external installation — so the publisher's machine needs no extra tooling for
the gate to work. It implements three layers of increasing generality: staged
credential files, known credential-format regexes, and an entropy sweep.

## User Story

**As** a developer committing to a public repository,
**I want** any credential caught on my machine before it ever reaches GitHub,
**so that** I never have to discover from a CI failure that I have already
irreversibly published a production secret.

---

## Requirements

### 1. Interface

1.1. The scanner MUST support these invocations:

| Invocation | Behaviour |
|-----------|-----------|
| `./scripts/scan-secrets.sh` | Scan the whole working tree |
| `./scripts/scan-secrets.sh --staged` | Scan staged changes only (pre-commit) |
| `./scripts/scan-secrets.sh --json` | Emit a machine-readable summary |

1.2. Exit codes MUST be: `0` = clean, `1` = blocking finding(s), `2` = usage
error. An unknown argument MUST exit `2`.

1.3. The scanner MUST resolve the repository root from `BASH_SOURCE` and `cd`
there, and MUST run under `set -euo pipefail`.

1.4. It MUST NOT depend on any tool beyond POSIX shell utilities and `git`.

### 2. Three-Layer Detection

2.1. **Layer 1 — staged credential files.** The scanner MUST inspect
`git diff --cached --name-only` and BLOCK on any staged file matching:

```
*.pem  *.key  *.p12  *.pfx  *credentials*.json
*service-account*.json  .env  .env.production  .env.local
```

2.2. **Layer 2 — known credential formats.** The scanner MUST sweep for these
21 rules, case-insensitively. Any unsuppressed match is a hard block:

| Rule | Rule |
|------|------|
| AWS Access Key | Azure Connection |
| AWS Secret Key | Azure SQL/Conn |
| GitHub Token | SQL DSN w/ pwd |
| GitHub OAuth | Datadog API |
| Slack Token | Heroku |
| Stripe Secret | npm Token |
| Stripe Publishable | PyPI Token |
| Google API Key | JWT / HS256 |
| SendGrid Key | Private Key |
| Twilio | Generic long key= |
| Slack Webhook | |

2.3. In `--staged` mode Layer 2 MUST scan the raw patch
(`git diff --cached --no-ext-diff --unified=0`); in full-tree mode it MUST use
`grep -rinE` with `--exclude-dir` for every entry of `SKIP_DIRS`
(`node_modules`, `dist`, `.venv`, `__pycache__`, `.git`, `vendor`, `bin`,
`coverage`).

2.4. Layer 2 regexes MUST be passed with `grep -e` so patterns beginning with
`-` (the `Private Key` PEM header rule) are not parsed as options.

2.5. **Layer 3 — entropy sweep.** The scanner MUST detect
`KEY=<32+ chars of base58/base64>` style assignments:

```
[A-Z0-9_]{3,}[[:space:]]*[=:][[:space:]]*["']?[A-Za-z0-9_+/=-]{32,}["']?
```

2.6. Layer 3 MUST be confined to env/config file types (`*.env*`, `*.yml`,
`*.yaml`, `*.toml`, `*.ini`, `*.conf`, `*.config`) and MUST NOT scan source
code. Rationale: limit blast radius — legitimate long constants in source would
otherwise flood the gate.

2.7. Layer 3 findings MUST be reported as `[REVIEW]` and MUST NOT block
(`issues + 0`). Rationale: entropy in config produces false positives on legit
generated values such as random seeds.

### 3. Suppression Contract

3.1. **SECRETS-SCAN-001 (file allowlist).** Only explicit allowlist entries may
suppress a finding. Each entry MUST be `file-substring|reason` with a mandatory
reason, and the list MUST be kept minimal:

| Pattern | Reason |
|---------|--------|
| `secrets/` | Secrets-manager implementation — identifiers only; values still scanned |
| `*.test.*`, `testdata/`, `*_test.go` | Test fixtures may contain fake creds |
| `SECRETS_MANAGEMENT.md`, `AGENT_GUARDRAILS.md` | Docs describing handling, no real values |
| `.guardrails/` | Guardrail rule definitions describe patterns |
| `docs/agentmcp/*.txt` | Design notes referencing the PEM header as a regex pattern |
| `deploy/docker-compose.yml` | `${VAR}` interpolation only, no literal credentials |

3.2. Allowlist matching MUST use glob semantics via `[[ "$path" == $glob ]]`,
MUST normalise a leading `./`, and MUST treat a trailing-slash entry as a
directory prefix matching everything beneath it.

3.3. **SECRETS-SCAN-002 (safe values).** Known non-secret documentation and
local-dev values MUST be suppressed for that line/rule only:

| Value | Nature |
|-------|--------|
| `AKIAIOSFODNN7EXAMPLE` | AWS documentation example key |
| `sk_live_abc…secretkey` | Inline fake in an API-manual example |
| `oap:oap@postgres` / `oap:oap@localhost` | Declared local-dev Postgres default |
| `user:pass@db`, `user:pass@localhost`, `user:password@host` | Generic placeholder DSNs |
| `etok_8f3a2b1c9d4e5f6a7b8c9d0e1f2a3b4c` | Fake enrollment token in docs |
| `dev-secret-change-me` | `.env.example` JWT dev placeholder |
| `oap-web-secret` | `.env.example` OIDC dev placeholder |

3.4. A file-level allowlist entry MUST NOT disable the value-level rules for
that file's contents where the layer still applies — the `secrets/` entry
explicitly keeps the value sweep active so a *real* credential there is still
caught.

3.5. Anything not covered by 3.1 or 3.3 that trips a rule MUST be a hard
failure. Adding a suppression is a reviewed change to the contract, never an
ad-hoc bypass.

### 4. CI Backstop (`secret-validation.yml`)

4.1. The workflow MUST run on push and PR to `[main, develop, 'sprint/**']`
with **no path filter**.

4.2. It MUST run three independent jobs:

| Job | Behaviour |
|-----|-----------|
| `scan-for-secrets` | gitleaks via `gitleaks/gitleaks-action@v2`, `fetch-depth: 0` |
| `check-env-files` | **Fails** on any `.env*` except `.env.example`/`.env.template` |
| `check-hardcoded-secrets` | **Warns only** on `password=`/`api_key=`/`secret=`/`token=`/`aws_access_key_id=`/`private_key=` patterns |

4.3. The gitleaks job MUST check out full history so historical commits are
scanned, and MUST receive `GITHUB_TOKEN` and `GITLEAKS_LICENSE`.

4.4. `check-env-files` MUST exit non-zero when a committed `.env` file is found;
`check-hardcoded-secrets` and the credential-file portion of `check-env-files`
are advisory and MUST NOT fail the build (they may match legitimate
environment-variable references and examples).

### 5. Pre-commit Integration

5.1. The pre-commit hook MUST run `./scripts/scan-secrets.sh --staged` as
**step 1 of 5**, before all other gates. Rationale: a secret is the only
unrecoverable failure — it must be caught before any other check spends time.

5.2. Any finding MUST set the hook's failure flag and block the commit.

### 6. Release Integration

6.1. `deploy.sh` MUST invoke the full-tree scan exactly twice — as gate #1 of 2
before any commit, and as gate #2 of 2 immediately before `git push`.

6.2. On failure at gate #1 the script MUST abort before any commit or push, and
MUST instruct the operator to remove the secret or add a SECRETS-SCAN-001
allowlist entry.

6.3. On failure at gate #2 the script MUST abort with nothing pushed and print
the un-tag recovery commands.

---

## Known Divergences

| # | Divergence | Impact |
|---|-----------|--------|
| 1 | Layer 3 collects file names in full-tree mode (`grep -lE`) but patch lines in `--staged` mode | `[REVIEW]` output granularity differs by mode |
| 2 | `--json` mode still executes the human-readable layer output before emitting JSON | JSON output is preceded by unstructured text; not cleanly parseable |
| 3 | CI `check-hardcoded-secrets` is warn-only while the local Generic-long-key rule blocks | A pattern blocked locally passes CI silently |
| 4 | Layer 1 always runs against `git diff --cached` even in full-tree mode | Unstaged credential files on disk are only caught by Layers 2–3 |

---

## Verification

```bash
# Clean tree should exit 0
./scripts/scan-secrets.sh; echo "exit=$?"

# Staged-only mode (what the hook runs)
./scripts/scan-secrets.sh --staged; echo "exit=$?"

# Confirm the allowlist contract and rule count
grep -n 'SECRETS-SCAN-00[12]' scripts/scan-secrets.sh
grep -c '^  "' scripts/scan-secrets.sh   # REGEX_RULES entries
```

---

## Related Specifications

- `deploy-pipeline` — the two release-time gates
- `pattern-scanning` — PREVENT-003 hardcoded-credential rule
- `ci-pipeline` — workflow triggers and permissions
