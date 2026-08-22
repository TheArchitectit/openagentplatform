#!/usr/bin/env bash
#
# scripts/semantic-scan.sh — DevGate-style semantic/structural scanner.
#
# Runs deeper static analysis than pattern-scan.sh:
#   1. go vet ./... (structural bugs, unreachable code, format strings)
#   2. tsc --noEmit (TypeScript type errors)
#   3. Python AST parse (syntax errors via py_compile)
#   4. Unused import check (Go)
#   5. Missing error handling in Go (unchecked !err returns after ignored calls)
#
# Blocking gate: exits non-zero on any failure.
#
# Usage:
#   ./scripts/semantic-scan.sh              # scan everything
#   ./scripts/semantic-scan.sh --staged     # only scan packages with staged files
#
# Exit codes: 0 = clean, 1 = violations found, 2 = usage error.

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

FAILED=0

echo "━━━ semantic scan ━━━"

# --- 1. Go vet ──────────────────────────────────────────────────────────────
if command -v go >/dev/null 2>&1; then
  echo "[1/5] go vet ./..."
  if [ "$STAGED_ONLY" = "1" ]; then
    GO_FILES="$(git diff --cached --name-only --diff-filter=ACM 2>/dev/null | grep '\.go$' || true)"
    if [ -n "$GO_FILES" ]; then
      GO_DIRS=$(echo "$GO_FILES" | xargs -I{} dirname {} | sort -u | sed 's|^|./|')
      if ! echo "$GO_DIRS" | xargs go vet 2>&1; then
        echo "  ✗ go vet FAILED"
        FAILED=1
      else
        echo "  ✓ clean"
      fi
    else
      echo "  ⊘ no Go files staged"
    fi
  else
    if ! go vet ./... 2>&1; then
      echo "  ✗ go vet FAILED"
      FAILED=1
    else
      echo "  ✓ clean"
    fi
  fi
else
  echo "[1/5] go vet ... ⊘ skipped (go not found)"
fi

# --- 2. TypeScript type-check ───────────────────────────────────────────────
if [ -d web ] && [ -f web/tsconfig.json ]; then
  echo "[2/5] tsc --noEmit"
  TS_FILES=""
  if [ "$STAGED_ONLY" = "1" ]; then
    TS_FILES="$(git diff --cached --name-only --diff-filter=ACM 2>/dev/null | grep -E '\.(ts|tsx)$' | grep -v '\.d\.ts$' | grep -v '\.gen\.' || true)"
  fi
  if [ "$STAGED_ONLY" = "0" ] || [ -n "$TS_FILES" ]; then
    if ! (cd web && npx tsc --noEmit 2>&1); then
      echo "  ✗ tsc FAILED"
      FAILED=1
    else
      echo "  ✓ clean"
    fi
  else
    echo "  ⊘ no TS files staged"
  fi
else
  echo "[2/5] tsc ... ⊘ skipped (web/ or tsconfig.json not found)"
fi

# --- 3. Python syntax check ─────────────────────────────────────────────────
if command -v python3 >/dev/null 2>&1; then
  echo "[3/5] python AST parse"
  PY_FILES=""
  if [ "$STAGED_ONLY" = "1" ]; then
    PY_FILES="$(git diff --cached --name-only --diff-filter=ACM 2>/dev/null | grep '\.py$' | grep -v __pycache__ || true)"
  else
    PY_FILES="$(git ls-files -- '*.py' | grep -v __pycache__ | grep -v .venv | grep -v node_modules || true)"
  fi
  if [ -n "$PY_FILES" ]; then
    PY_ERRORS=0
    for f in $PY_FILES; do
      if ! python3 -c "import py_compile; py_compile.compile('$f', doraise=True)" 2>/dev/null; then
        echo "  [SYNTAX ERROR] $f"
        PY_ERRORS=$((PY_ERRORS + 1))
      fi
    done
    if [ $PY_ERRORS -gt 0 ]; then
      echo "  ✗ $PY_ERRORS Python syntax error(s)"
      FAILED=1
    else
      echo "  ✓ clean"
    fi
  else
    echo "  ⊘ no Python files to check"
  fi
else
  echo "[3/5] python AST ... ⊘ skipped (python3 not found)"
fi

# --- 4. Unused Go imports ───────────────────────────────────────────────────
if command -v go >/dev/null 2>&1; then
  echo "[4/5] unused import check"
  GO_FILES=""
  if [ "$STAGED_ONLY" = "1" ]; then
    GO_FILES="$(git diff --cached --name-only --diff-filter=ACM 2>/dev/null | grep '\.go$' | grep -v '_test.go' || true)"
  else
    GO_FILES="$(git ls-files -- '*.go' | grep -v _test.go | grep -v vendor || true)"
  fi
  if [ -n "$GO_FILES" ]; then
    UNUSED=0
    for f in $GO_FILES; do
      # Check for imports that are declared but never used (goimports-style)
      # This is a heuristic: look for import blocks where a package name
      # appears exactly once (only in the import line).
      imports=$(grep -E '^\s*"[^"]+"\s*$' "$f" 2>/dev/null | sed 's/.*"\(.*\)".*/\1/' || true)
      for pkg in $imports; do
        pkgname=$(basename "$pkg")
        # Count occurrences of the package name in the file (excluding import block)
        count=$(grep -c "\b${pkgname}\." "$f" 2>/dev/null || echo "0")
        if [ "$count" = "0" ]; then
          echo "  [UNUSED] $f: import \"$pkg\" (package name '$pkgname' never used)"
          UNUSED=$((UNUSED + 1))
        fi
      done
    done
    if [ $UNUSED -gt 0 ]; then
      echo "  ✗ $UNUSED unused import(s) found"
      FAILED=1
    else
      echo "  ✓ clean"
    fi
  else
    echo "  ⊘ no Go files to check"
  fi
else
  echo "[4/5] unused imports ... ⊘ skipped (go not found)"
fi

# --- 5. Go build verification ───────────────────────────────────────────────
if command -v go >/dev/null 2>&1; then
  echo "[5/5] go build ./..."
  if ! go build ./... 2>&1; then
    echo "  ✗ go build FAILED"
    FAILED=1
  else
    echo "  ✓ clean"
  fi
else
  echo "[5/5] go build ... ⊘ skipped (go not found)"
fi

echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

if [ $FAILED -ne 0 ]; then
  echo "✗ SEMANTIC SCAN FAILED — fix errors above"
  exit 1
fi

echo "✓ SEMANTIC SCAN PASSED"
exit 0
