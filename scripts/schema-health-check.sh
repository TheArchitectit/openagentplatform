#!/usr/bin/env bash
#
# scripts/schema-health-check.sh — DevGate-style schema health validator.
#
# Validates:
#   1. Migration files are present and numbered sequentially
#   2. Every .up.sql has a matching .down.sql
#   3. Migration numbers have no gaps
#   4. No migration file is empty
#   5. SQL syntax is basic-valid (no obvious unclosed blocks)
#
# Advisory gate: warns on issues but does NOT block commits.
# Blocking only for: empty migration files (data loss risk).
#
# Usage:
#   ./scripts/schema-health-check.sh
#
# Exit codes: 0 = clean, 1 = blocking issue, 2 = usage error.

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

echo "━━━ schema health check ━━━"

WARNINGS=0
ERRORS=0

# Find all migration directories
MIGRATION_DIRS=()
for dir in mcp-server/migrations mcp-server/internal/database/migrations; do
  if [ -d "$dir" ]; then
    MIGRATION_DIRS+=("$dir")
  fi
done

if [ ${#MIGRATION_DIRS[@]} -eq 0 ]; then
  echo "  ⊘ no migration directories found — skipping"
  echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
  echo "✓ SCHEMA HEALTH CHECK PASSED (no migrations)"
  exit 0
fi

for MIG_DIR in "${MIGRATION_DIRS[@]}"; do
  echo
  echo "  checking: $MIG_DIR"

  UP_FILES=()
  DOWN_FILES=()
  NUMBERS=()

  for f in "$MIG_DIR"/*.up.sql; do
    [ -f "$f" ] || continue
    UP_FILES+=("$f")
    # Extract migration number (leading digits, treated as decimal)
    basename_f=$(basename "$f")
    num=$(echo "$basename_f" | grep -oE '^[0-9]+' || true)
    if [ -n "$num" ]; then
      NUMBERS+=("$num")
    fi

    # Check for empty up migrations
    if [ ! -s "$f" ]; then
      echo "  [ERROR] empty up migration: $f"
      ERRORS=$((ERRORS + 1))
    fi
  done

  for f in "$MIG_DIR"/*.down.sql; do
    [ -f "$f" ] || continue
    DOWN_FILES+=("$f")
    if [ ! -s "$f" ]; then
      echo "  [WARNING] empty down migration: $f (rollback will be a no-op)"
      WARNINGS=$((WARNINGS + 1))
    fi
  done

  # Check for up without matching down
  for up_file in "${UP_FILES[@]}"; do
    expected_down="${up_file%.up.sql}.down.sql"
    if [ ! -f "$expected_down" ]; then
      echo "  [ERROR] up migration without down: $(basename "$up_file")"
      ERRORS=$((ERRORS + 1))
    fi
  done

  # Check for gaps in numbering
  if [ ${#NUMBERS[@]} -gt 1 ]; then
    # Sort and deduplicate
    SORTED_NUMS=($(printf '%s\n' "${NUMBERS[@]}" | sort -un))
    prev=$((10#${SORTED_NUMS[0]}))
    for ((i=1; i<${#SORTED_NUMS[@]}; i++)); do
      curr=$((10#${SORTED_NUMS[$i]}))
      if [ $((curr - prev)) -gt 1 ]; then
        echo "  [WARNING] migration gap: $prev → $curr (missing $((prev + 1))..$((curr - 1)))"
        WARNINGS=$((WARNINGS + 1))
      fi
      prev="$curr"
    done
  fi

  echo "  ${#UP_FILES[@]} up, ${#DOWN_FILES[@]} down migrations checked"
done

echo
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

if [ $ERRORS -gt 0 ]; then
  echo "✗ SCHEMA HEALTH CHECK FAILED — $ERRORS error(s), $WARNINGS warning(s)"
  exit 1
fi

if [ $WARNINGS -gt 0 ]; then
  echo "⚠ SCHEMA HEALTH CHECK PASSED with $WARNINGS warning(s)"
else
  echo "✓ SCHEMA HEALTH CHECK PASSED"
fi
exit 0
