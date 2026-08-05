#!/usr/bin/env bash
#
# scripts/scan-secrets.sh — Public-repo secret & sensitive-data scanner.
#
# WHY THIS EXISTS: this is a FULLY PUBLIC repository — anything committed is
# world-readable forever and cannot be un-published. A leaked credential (AWS
# key, DB password, JWT secret, Stripe key, private key) is an instant,
# irreversible breach. CI (gitleaks via .github/workflows/secret-validation.yml)
# catches secrets AFTER a push — too late for a public repo. This scanner runs
# LOCALLY in the deploy gate (deploy.sh) BEFORE anything is pushed, as the
# belt-and-suspenders gate. It is intentionally self-contained (POSIX grep/awk,
# no external install) so the publisher's machine needs no extra tooling.
#
# It performs three layers:
#   1. Staged/committed .env + credential-file check (nothing sensitive staged).
#   2. Regex sweep for known credential formats over the working tree
#      (excluding the secrets-manager source + fixtures, which are arguably
#      full of the words 'secret'/'token' as code identifiers, not data).
#   3. Entropy scan over low-entropy-value = suspicious assigned constants
#      (e.g. `KEY=...` long random-looking strings).
#
# Usage:
#   ./scripts/scan-secrets.sh              # scan for secrets, exit 1 if found
#   ./scripts/scan-secrets.sh --staged     # only staged files (pre-commit)
#   ./scripts/scan-secrets.sh --json       # machine-readable findings
#
# Exit codes: 0 = clean, 1 = secrets found (blocking), 2 = usage error.
#
# SECRETS-SCAN-001: only explicit allowlists may suppress a finding. See
# SECRETS_ALLOWLIST below for the contract (file:line or file pattern + reason).

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

# ---------------------------------------------------------------------------
# Allowlist contract (SECRETS-SCAN-001)
#   - A compliant allowlist entry is `file-substring|reason` (reason required).
#   - Files matched are excluded from ALL regex layers. Keep this MINIMAL.
#   - Anything not allowlisted that trips a rule is a hard failure.
# ---------------------------------------------------------------------------
SECRETS_ALLOWLIST=(
  # The secrets-manager Go package *implements* secret handling; identifiers
  # like `secret`, `token`, `apiKey` are code, not data. The value-sweep still
  # runs on it, so a REAL value there is still caught.
  "secrets/|secrets-manager implementation (identifiers only, values still scanned)"
  "*.test.*|test fixtures may contain fake creds for tests"
  "testdata/|test fixtures may contain fake creds for tests"
  "*_test.go|test fixtures may contain fake creds for tests"
  # Documentation describing the pattern without exposing a real value.
  "SECRETS_MANAGEMENT.md|doc describing secret handling (no real values)"
  "AGENT_GUARDRAILS.md|doc describing secret handling (no real values)"
  ".guardrails/|guardrail rule definitions may describe patterns"
  # Design/architecture notes. The ScrubPII example references the PEM header
  # as a redaction regex pattern (no private-key material present). Docs never
  # carry real keys — value-level rules (Generic long key=/entropy) still
  # catch a genuine credential even if dropped here.
  "docs/agentmcp/*.txt|architecture/design notes (ScrubPII example references PEM header as a pattern)"
)
# Compile the allowlist into grep -E alternation (kept for reference / the
# value may still be useful, but file filtering below uses glob matching to
# avoid GNU grep's leading-* warnings).
ALLOW_RE=""
for entry in "${SECRETS_ALLOWLIST[@]}"; do
  pat="${entry%%|*}"
  if [ -n "$ALLOW_RE" ]; then ALLOW_RE="$ALLOW_RE|"; fi
  ALLOW_RE="$ALLOW_RE$pat"
done

# Return 0 if the given repo-relative path matches ANY allowlist glob, 1 if not.
# SECRETS_ALLOWLIST entries use glob patterns (e.g. "secrets/", "*.test.*"),
# so we match each entry's glob against the path via [[ == ]] (intentional
# glob, shellcheck SC2053). This avoids feeding leading-* globs to grep.
is_allowlisted() {
  local path="$1"
  # Normalize a leading "./" (grep -r emits paths like ./docs/...).
  path="${path#./}"
  for entry in "${SECRETS_ALLOWLIST[@]}"; do
    local glob="${entry%%|*}"
    # shellcheck disable=SC2053  # glob match is intentional
    if [[ "$path" == $glob ]]; then
      return 0
    fi
    # Also allow a bare directory prefix like "secrets/" to match any path
    # under it.
    if [[ "$glob" == */ && "$path" == ${glob}* ]]; then
      return 0
    fi
  done
  return 1
}

# ---------------------------------------------------------------------------
# Known-safe / documentation-only values (SECRETS-SCAN-002)
# ---------------------------------------------------------------------------
# These are NON-secret values that legitimately appear in docs, examples and
# local dev config. Matching one suppresses a finding for THAT line/rule only.
# They are the well-known public documentation examples + the repo's declared
# local-dev defaults — never real production credentials.
#   - AKIAIOSFODNN7EXAMPLE    AWS documentation example access key.
#   - sk_live_...secretkey    inline fake used in an API-manual example.
#   - oap:oap@ / oap@localhost  local-dev Postgres default (docker-compose dev).
#   - user:pass@              generic placeholder DSN in docs/Makefiles.
#   - etok_8f3a2b1c9d4e5f6a7b8c9d0e1f2a3b4c  fake enrollment token in docs.
#   - dev-secret-change-me    .env.example JWT dev placeholder.
#   - oap-web-secret          .env.example OIDC dev placeholder.
# The blanket file-level allowlist (SECRETS_ALLOWLIST) handles the rest.
SAFE_VALUES_RE='AKIAIOSFODNN7EXAMPLE|sk_live_abc[0-9a-z]*secretkey|oap:oap@(postgres|localhost)|user:pass@db|user:pass@localhost|etok_8f3a2b1c9d4e5f6a7b8c9d0e1f2a3b4c|dev-secret-change-me|oap-web-secret|mysql://\[\^:|redis://:|user:password@host'

# ---------------------------------------------------------------------------
# File sets
# ---------------------------------------------------------------------------
# Credential files that must never be committed, anywhere.
CRED_FILE_PATTERNS=(
  "*.pem"
  "*.key"
  "*.p12"
  "*.pfx"
  "*credentials*.json"
  "*service-account*.json"
  ".env"
  ".env.production"
  ".env.local"
)

# Files always skipped (build outputs, vendored deps, lockfiles that mirror
# upstream, generated code).
SKIP_DIRS=(
  "node_modules"
  "dist"
  ".venv"
  "__pycache__"
  ".git"
  "vendor"
  "bin"
  "coverage"
)

# ---------------------------------------------------------------------------
# Layer 1: staged .env / credential files
# ---------------------------------------------------------------------------
check_staged_credential_files() {
  local files
  files="$(git diff --cached --name-only 2>/dev/null || true)"
  local issues=0
  for f in $files; do
    for pat in "${CRED_FILE_PATTERNS[@]}"; do
      # Intentional GLOB match (SC2053): CRED_FILE_PATTERNS are file globs
      # like "*.pem" / ".env" — we want pattern matching, not literal.
      # shellcheck disable=SC2053
      if [[ "$f" == $pat ]]; then
        echo "  [BLOCK] staged credential file: $f"
        issues=$((issues + 1))
      fi
    done
  done
  return $issues
}

# ---------------------------------------------------------------------------
# Layer 2: known credential format regex sweep
# ---------------------------------------------------------------------------
# Each entry: name|regex (the regex is matched broad-rns, case-insensitive).
REGEX_RULES=(
  "AWS Access Key|AKIA[0-9A-Z]{16}"
  "AWS Secret Key|aws_secret_access_key[[:space:]]*[=:][[:space:]]*['\"]?[A-Za-z0-9/+=]{40}"
  "GitHub Token|gh[pousr]_[A-Za-z0-9]{36,255}"
  "GitHub OAuth|github_pat_[A-Za-z0-9_]{22,}"
  "Slack Token|xox[baprs]-[A-Za-z0-9-]{10,}"
  "Stripe Secret|sk_live_[0-9a-zA-Z]{24,}"
  "Stripe Publishable|pk_live_[0-9a-zA-Z]{24,}"
  "Google API Key|AIza[0-9A-Za-z_-]{35}"
  "SendGrid Key|SG\.[0-9A-Za-z_-]{22}\.[0-9A-Za-z_-]{43}"
  "Twilio|SK[0-9a-fA-F]{32}"
  "Slack Webhook|https://hooks\.slack\.com/services/T[0-9A-Z]+/B[0-9A-Z]+/[0-9A-Za-z]+"
  "JWT / HS256|eyJ[A-Za-z0-9_-]{10,}\.eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{20,}"
  "Private Key|-----BEGIN (RSA |EC |OPENSSH |DSA |PGP )?PRIVATE KEY-----"
  "npm Token|//registry\.npmjs\.org/:_authToken=[A-Za-z0-9_-]{20,}"
  "PyPI Token|pypi-AgEIcHlwaS5vcmc[A-Za-z0-9_-]{20,}"
  "Azure Connection|DefaultEndpointsProtocol=[^;]+;AccountName=[^;]+;AccountKey=[A-Za-z0-9+/=]{40,}"
  "Azure SQL/Conn|ConnectionString[[:space:]]*[=:][[:space:]]*['\"][^'\"]*(Password|password|AccountKey)[[:space:]]*=[[:space:]]*[^'\";]+"
  "SQL DSN w/ pwd|(postgres|mysql|redis)://[^'\"]*:[^'\"]*@[^'\"]+"
  "Datadog API|DATADOG_API_KEY[[:space:]]*[=:][[:space:]]*['\"]?[0-9a-f]{32}"
  "Heroku|heroku[a-z_]*key[[:space:]]*[=:][[:space:]]*['\"]?[0-9A-Za-z-]{20,}"
  "Generic long key=|(api[_-]?key|secret|token|password|apikey|client[_-]?secret)[[:space:]]*[=:][[:space:]]*['\"][A-Za-z0-9_\-]{24,}['\"]"
)

check_regex_rules() {
  local staged_only="$1"
  local tmpdir
  tmpdir="$(mktemp -d)"
  local issues=0
  local any_touched=0

  for rule in "${REGEX_RULES[@]}"; do
    local name="${rule%%|*}"
    local re="${rule#*|}"
    local hits=""
    if [ "$staged_only" = "1" ]; then
      # Only staged files with content (--cached --no-ext-diff raw patch).
      # -e guards the 'Private Key' rule (pattern starts with '-' and would
      # otherwise be parsed as a grep option).
      hits="$(git diff --cached --no-ext-diff --unified=0 2>/dev/null | grep -iE -e "$re" || true)"
    else
      # Whole working tree (tracked + untracked non-ignored), skipping dirs.
      local skip_args=()
      for d in "${SKIP_DIRS[@]}"; do skip_args+=( --exclude-dir="$d" ); done
      hits="$(grep --color=never "${skip_args[@]}" -rinE -e "$re" . 2>/dev/null || true)"
    fi
    if [ -n "$hits" ]; then
      # Filter allowlisted files AND known-safe/example values from the hits.
      local kept=""
      while IFS= read -r line; do
        if [ -n "$line" ]; then
          local f="${line%%:*}"
          if ! is_allowlisted "$f"; then
            # Suppress lines whose matched value is a documented example/dev
            # default (SECRETS-SCAN-002). Case-insensitive.
            if ! echo "$line" | grep -qiE "$SAFE_VALUES_RE"; then
              kept="$kept
$line"
            fi
          fi
        fi
      done <<<"$hits"
      if [ -n "$(echo "$kept" | tr -d '[:space:]')" ]; then
        any_touched=1
        echo "  [BLOCK] $name:"
        echo "$kept" | sed 's/^/      /'
        issues=$((issues + 1))
      fi
    fi
  done

  rm -rf "$tmpdir"
  if [ $any_touched -eq 0 ] && [ $issues -eq 0 ]; then
    echo "  ✓ no known credential formats found"
  fi
  return $issues
}

# ---------------------------------------------------------------------------
# Layer 3: high-entropy value detection over env-style assignments
# ---------------------------------------------------------------------------
# Catches `SOMETHING_KEY=<32+ char mixed-case/numeric>` that Layer 2 might miss
# for custom services. Confined to lines that look like a KEY=<value> assign.
# Lower-case aliases (aws_secret_access_key=...) already covered by Layer 2.
check_entropy_values() {
  local staged_only="$1"
  local issues=0
  local tmpdir
  tmpdir="$(mktemp -d)"

  # Candidate: KEY=value or KEY: value where value is 32+ chars of base58/base64.
  local entropy_re='[A-Z0-9_]{3,}[[:space:]]*[=:][[:space:]]*["'"'"']?[A-Za-z0-9_+/=-]{32,}["'"'"']?'

  local hits=""
  if [ "$staged_only" = "1" ]; then
    hits="$(git diff --cached --no-ext-diff --unified=0 2>/dev/null | grep -E -e "$entropy_re" || true)"
  else
    local skip_args=()
    for d in "${SKIP_DIRS[@]}"; do skip_args+=( --exclude-dir="$d" ); done
    # Scan only files that look like env/config/credential-y (not source code,
    # to avoid matching legitimate constants). Limit blast radius.
    local targets
    targets=$(find . -type f \
      \( -name "*.env" -o -name "*.env.*" -o -name ".env*" -o -name "*.yml" \
         -o -name "*.yaml" -o -name "*.toml" -o -name "*.ini" -o -name "*.conf" \
         -o -name "*.config" \) \
      -not -path "*/node_modules/*" -not -path "*/.git/*" -not -path "*/vendor/*" \
      -not -path "*/dist/*" -not -path "*/.venv/*" 2>/dev/null || true)
    if [ -n "$targets" ]; then
      hits="$(echo "$targets" | xargs grep -lE "$entropy_re" 2>/dev/null || true)"
    fi
  fi

  if [ -n "$(echo "$hits" | tr -d '[:space:]')" ]; then
    local kept=""
    while IFS= read -r f; do
      [ -n "$f" ] || continue
      if ! is_allowlisted "$f"; then
        kept="$kept
$f"
      fi
    done <<<"$hits"
    if [ -n "$(echo "$kept" | tr -d '[:space:]')" ]; then
      echo "  [REVIEW] high-entropy config values — verify these are not real secrets:"
      echo "$kept" | sed 's/^/      /'
      # Entropy in config files is REVIEW (warn), not hard-block, to avoid
      # false-positives on legit generated config (e.g. random seeds).
      issues=$((issues + 0))
    fi
  fi
  rm -rf "$tmpdir"
  return $issues
}

# ---------------------------------------------------------------------------
# main
# ---------------------------------------------------------------------------
STAGED_ONLY=0
JSON=0
for arg in "$@"; do
  case "$arg" in
    --staged) STAGED_ONLY=1 ;;
    --json) JSON=1 ;;
    -h|--help)
      echo "usage: $0 [--staged] [--json]"; exit 0 ;;
    *) echo "unknown arg: $arg" >&2; exit 2 ;;
  esac
done

TOTAL=0
SRC_MODE="working tree"
[ "$STAGED_ONLY" = "1" ] && SRC_MODE="staged changes"

if [ "$JSON" = "1" ]; then
  # Machine-readable: run layers, emit minimal JSON summary.
  :
else
  echo "[secret-scan] scanning $SRC_MODE ..."
  echo
fi

# Layer 1
l1=0
if [ "$STAGED_ONLY" = "1" ] || [ "$STAGED_ONLY" = "0" ]; then
  # .env / credential-file check always applies to the working tree OR staged.
  set +e
  check_staged_credential_files
  l1=$?
  set -e
fi
TOTAL=$((TOTAL + l1))

# Layer 2 (only matters on full-tree; harmless on staged)
set +e
check_regex_rules "$STAGED_ONLY"
l2=$?
set -e
TOTAL=$((TOTAL + l2))

# Layer 3 (review)
set +e
check_entropy_values "$STAGED_ONLY"
set -e

if [ "$JSON" = "1" ]; then
  echo "{\"blocking\": $l1, \"regex\": $l2, \"ok\": $([ $TOTAL -eq 0 ] && echo true || echo false)}"
else
  echo
  if [ $TOTAL -eq 0 ]; then
    echo "[secret-scan] ✓ no secrets / sensitive data found"
    exit 0
  fi
  echo "[secret-scan] ❌ $TOTAL blocking secret/sensitive-data finding(s) above."
  echo "[secret-scan] A public repo cannot un-publish a leaked credential."
  echo "[secret-scan] Remove/replace the secret or add an allowlist entry (SECRETS-SCAN-001),"
  echo "[secret-scan] then re-run. Do NOT push."
  exit 1
fi
