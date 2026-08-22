#!/usr/bin/env bash
#
# scripts/pattern-scan.sh — DevGate-style pattern scanner for anti-patterns.
#
# Reads rules from .guardrails/prevention-rules/pattern-rules.json and
# scans tracked source files for violations. Blocking gate: exits non-zero
# on any 'critical' or 'error' severity match. 'warning' matches are
# reported but do not block.
#
# Usage:
#   ./scripts/pattern-scan.sh              # scan all tracked files
#   ./scripts/pattern-scan.sh --staged     # only staged files (for pre-commit)
#
# Exit codes: 0 = clean, 1 = blocking violations found, 2 = usage error.

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

STAGED_ONLY=0
for arg in "$@"; do
  case "$arg" in
    --staged) STAGED_ONLY=1 ;;
    -h|--help) echo "usage: $0 [--staged]"; exit 0 ;;
  esac
done

RULES_FILE=".guardrails/prevention-rules/pattern-rules.json"
if [ ! -f "$RULES_FILE" ]; then
  echo "[pattern-scan] ERROR: rules file not found: $RULES_FILE" >&2
  exit 2
fi

# Parse rules with node (available in this repo) or python3 as fallback
parse_rules() {
  if command -v node >/dev/null 2>&1; then
    node -e "
      const fs = require('fs');
      const data = JSON.parse(fs.readFileSync('$RULES_FILE', 'utf8'));
      for (const r of data.rules) {
        if (!r.enabled) continue;
        const globs = r.file_glob.join(',');
        console.log([r.rule_id, r.severity, r.pattern, r.message, globs, r.forbidden_context||''].join('\t'));
      }
    "
  elif command -v python3 >/dev/null 2>&1; then
    python3 -c "
import json, sys
with open('$RULES_FILE') as f:
    data = json.load(f)
for r in data['rules']:
    if not r.get('enabled', True): continue
    globs = ','.join(r.get('file_glob', ['*']))
    ctx = r.get('forbidden_context') or ''
    print('\t'.join([r['rule_id'], r['severity'], r['pattern'], r['message'], globs, ctx]))
"
  else
    echo "[pattern-scan] ERROR: need node or python3 to parse rules" >&2
    exit 2
  fi
}

# Build file list
if [ "$STAGED_ONLY" = "1" ]; then
  FILE_LIST="$(git diff --cached --name-only --diff-filter=ACM 2>/dev/null | grep -E '\.(go|ts|tsx|js|jsx|py)$' || true)"
else
  FILE_LIST="$(git ls-files -- '*.go' '*.ts' '*.tsx' '*.js' '*.jsx' '*.py' | grep -v node_modules | grep -v dist | grep -v '.gen.' | grep -v '.d.ts' || true)"
fi

if [ -z "$FILE_LIST" ]; then
  echo "[pattern-scan] no source files to scan"
  exit 0
fi

ERRORS=0
WARNINGS=0
BLOCKING_RULES=0

while IFS=$'\t' read -r rule_id severity pattern message globs forbidden_ctx; do
  # Convert comma-separated globs to a grep pattern
  glob_re=""
  IFS=',' read -ra GLOBS <<< "$globs"
  for g in "${GLOBS[@]}"; do
    # Strip leading * for extension matching
    ext="${g#\*}"
    if [ -n "$glob_re" ]; then glob_re="$glob_re|"; fi
    glob_re="${glob_re}\\.${ext#\\.}\$"
  done

  # Filter files to those matching this rule's glob
  matching_files=""
  for f in $FILE_LIST; do
    # Simple extension check
    case "$f" in
      *.go)   [[ "$globs" == *".go"* ]]   && matching_files="$matching_files $f" ;;
      *.ts)   [[ "$globs" == *".ts"* ]]   && matching_files="$matching_files $f" ;;
      *.tsx)  [[ "$globs" == *".tsx"* ]]  && matching_files="$matching_files $f" ;;
      *.js)   [[ "$globs" == *".js"* ]]   && matching_files="$matching_files $f" ;;
      *.jsx)  [[ "$globs" == *".jsx"* ]]  && matching_files="$matching_files $f" ;;
      *.py)   [[ "$globs" == *".py"* ]]   && matching_files="$matching_files $f" ;;
    esac
  done

  if [ -z "$matching_files" ]; then
    continue
  fi

  # Run the pattern against matching files
  hits=""
  for f in $matching_files; do
    file_hits=$(grep -nP "$pattern" "$f" 2>/dev/null || true)
    if [ -n "$file_hits" ]; then
      # Apply forbidden_context exclusion: if the context pattern matches
      # the line, suppress the finding
      if [ -n "$forbidden_ctx" ]; then
        file_hits=$(echo "$file_hits" | grep -vP "$forbidden_ctx" || true)
      fi
      if [ -n "$file_hits" ]; then
        hits="$hits$file_hits\n"
      fi
    fi
  done

  if [ -n "$hits" ]; then
    if [ "$severity" = "critical" ] || [ "$severity" = "error" ]; then
      echo "  [$rule_id] $message ($severity)"
      echo -e "$hits" | head -5 | sed 's/^/      /'
      BLOCKING_RULES=$((BLOCKING_RULES + 1))
      ERRORS=$((ERRORS + 1))
    else
      echo "  [$rule_id] $message (warning)"
      echo -e "$hits" | head -3 | sed 's/^/      /'
      WARNINGS=$((WARNINGS + 1))
    fi
  fi
done < <(parse_rules)

echo
echo "[pattern-scan] $ERRORS blocking, $WARNINGS warnings"

if [ $BLOCKING_RULES -gt 0 ]; then
  echo "[pattern-scan] BLOCKED — fix $BLOCKING_RULES violation(s) before committing"
  exit 1
fi

echo "[pattern-scan] ✓ no blocking violations"
exit 0
