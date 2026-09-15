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

# Field separator for the rule records below. It must be a character that
# (a) can never appear in a rule field and (b) is not IFS whitespace.
#
# Tab fails (a)-adjacent requirement (b): bash treats tab as IFS *whitespace*,
# so a run of tabs collapses into one delimiter and empty fields vanish. Every
# rule without a forbidden_context then shifts its exclude_glob into the
# context slot — which silently disabled exclude_glob everywhere and fed
# `*_test.go` to grep -vP as a regex.
#
# Unit Separator (0x1f) is not IFS whitespace, so empty fields are preserved.
SEP=$'\x1f'

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

# Parse rules with node (available in this repo) or python3 as fallback.
# Every record carries all eight fields, empty ones included, joined by SEP.
parse_rules() {
  if command -v node >/dev/null 2>&1; then
    node -e "
      const fs = require('fs');
      const data = JSON.parse(fs.readFileSync('$RULES_FILE', 'utf8'));
      const sep = '\x1f';
      for (const r of data.rules) {
        if (!r.enabled) continue;
        const globs = r.file_glob.join(',');
        const excludes = (r.exclude_glob || []).join(',');
        const comments = r.scan_comments ? '1' : '';
        console.log([r.rule_id, r.severity, r.pattern, r.message, globs, r.forbidden_context||'', excludes, comments].join(sep));
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
    excludes = ','.join(r.get('exclude_glob', []))
    comments = '1' if r.get('scan_comments') else ''
    print('\x1f'.join([r['rule_id'], r['severity'], r['pattern'], r['message'], globs, ctx, excludes, comments]))
"
  else
    echo "[pattern-scan] ERROR: need node or python3 to parse rules" >&2
    exit 2
  fi
}

# glob_list_match PATH LIST — true when PATH matches any comma-separated glob.
#
# Matching follows the DevGate contract: '*' crosses '/', and '**/' matches
# zero or more directories. PATH is tested as given and by basename, so a
# bare-extension glob like '*.go' reaches nested files. Empty LIST is no match.
#
# The list is split with `read -a`, not an unquoted `for … in $LIST`: word
# splitting on an unquoted expansion also runs pathname expansion, which
# replaces a pattern like 'cmd/*' with the real entries of ./cmd before it can
# ever be compared. `read` splits without expanding, and needs no global
# `set -f` that would change behaviour for the rest of the script.
glob_list_match() {
  local path="$1" list="$2" base glob variant i
  [ -z "$list" ] && return 1
  base="${path##*/}"
  local -a parts
  IFS=',' read -r -a parts <<< "$list"
  for ((i = 0; i < ${#parts[@]}; i++)); do
    glob="${parts[i]}"
    [ -z "$glob" ] && continue
    # shellcheck disable=SC2053  # unquoted RHS is the pattern, deliberately
    if [[ "$path" == $glob || "$base" == $glob ]]; then return 0; fi
    # bash reads '**' as '*', which needs at least one directory where the
    # contract allows zero. Retry with the zero-directory expansion.
    if [[ "$glob" == *"/**/"* ]]; then
      variant="${glob//\/\*\*\//\/}"
      # shellcheck disable=SC2053
      if [[ "$path" == $variant || "$base" == $variant ]]; then return 0; fi
    fi
  done
  return 1
}

# is_test_file PATH — mirrors guardrails-scan.mjs isTestFile: error/critical
# rules do not apply to test code. A gate that fires production rules on
# scanner fixtures gets waived into meaninglessness within a week, so the
# canonical scanner exempts them by design and this script has to agree —
# otherwise the two disagree about the same commit.
is_test_file() {
  local rel="$1" base="${1##*/}"
  case "$rel" in tests/*|test/*|*/tests/*|*/test/*) return 0 ;; esac
  case "$base" in *_test.go|*_test.rs|*_test.py) return 0 ;; esac
  case "$base" in conftest*|test_*) return 0 ;; esac
  case "$base" in *.test.*|*.spec.*) return 0 ;; esac
  return 1
}

# is_comment_line LINE EXT — true when LINE is a comment-only line for file
# extension EXT. Mirrors guardrails-scan.mjs isCommentLine: full-line comments
# only; a trailing comment on a code line is still scanned.
is_comment_line() {
  local line="$1" ext="$2" trimmed
  trimmed="${line#"${line%%[![:space:]]*}"}"
  [ -z "$trimmed" ] && return 1
  case "$ext" in
    .py|.rb|.sh)
      [[ "$trimmed" == \#* ]] && return 0 ;;
    .html|.xml|.svg)
      [[ "$trimmed" == "<!--"* ]] && return 0 ;;
    .ts|.tsx|.js|.jsx|.rs|.go|.java|.kt|.gd|.php)
      [[ "$trimmed" == "//"* || "$trimmed" == "/*"* || "$trimmed" == "*" ]] && return 0 ;;
  esac
  return 1
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

while IFS="$SEP" read -r rule_id severity pattern message globs forbidden_ctx exclude_globs scan_comments; do
  # A regex grep cannot compile exits >1; it does not report "no match". Left
  # unchecked, an uncompilable rule would look like a rule that found nothing —
  # a clean-looking gate that checked nothing. Validate once per rule.
  st=0
  grep -nP -e "$pattern" /dev/null >/dev/null 2>&1 || st=$?
  if [ "$st" -gt 1 ]; then
    echo "  [$rule_id] WARNING: pattern failed to compile; rule skipped" >&2
    continue
  fi
  if [ -n "$forbidden_ctx" ]; then
    st=0
    grep -nP -e "$forbidden_ctx" /dev/null >/dev/null 2>&1 || st=$?
    if [ "$st" -gt 1 ]; then
      echo "  [$rule_id] WARNING: forbidden_context failed to compile; suppression disabled" >&2
      forbidden_ctx=""
    fi
  fi

  # Select the files this rule applies to: file_glob must match, exclude_glob
  # must not. Honoring exclude_glob is not optional — rules like OAP-002 and
  # OAP-007 exist to keep fmt.Print/os.Exit out of *library* code and
  # explicitly exclude cmd/*, so ignoring the field reports every legitimate
  # CLI main() as a blocking error.
  blocking=0
  if [ "$severity" = "critical" ] || [ "$severity" = "error" ]; then blocking=1; fi

  matching_files=""
  for f in $FILE_LIST; do
    if [ "$blocking" = "1" ] && is_test_file "$f"; then continue; fi
    if glob_list_match "$f" "$globs" && ! glob_list_match "$f" "$exclude_globs"; then
      matching_files="$matching_files $f"
    fi
  done

  if [ -z "$matching_files" ]; then
    continue
  fi

  # Run the pattern against matching files.
  hits=""
  hit_count=0
  for f in $matching_files; do
    # -e separates the pattern from the filename: a rule whose pattern begins
    # with '-' (e.g. the PEM private-key marker) is otherwise read as an option.
    file_hits=$(grep -nP -e "$pattern" -- "$f" 2>/dev/null || true)
    [ -z "$file_hits" ] && continue

    # Comment-only lines are skipped unless the rule opts in via
    # scan_comments: rules otherwise fire on their own explanatory comments
    # (the os.Exit gate tripping over a comment that says "os.Exit").
    if [ "$scan_comments" != "1" ]; then
      ext=".${f##*.}"
      file_hits=$(printf '%s\n' "$file_hits" | while IFS= read -r hl; do
        [ -n "$hl" ] || continue
        if ! is_comment_line "${hl#*:}" "$ext"; then
          printf '%s\n' "$hl"
        fi
      done)
      [ -z "$file_hits" ] && continue
    fi

    if [ -n "$forbidden_ctx" ]; then
      # Apply forbidden_context: a line carrying its documented safe usage is
      # suppressed. Filtered on the raw line, before the filename prefix is
      # added, so a path like internal/testutil/ cannot trip a rule's
      # (test|mock) context pattern and blank the whole file.
      file_hits=$(printf '%s\n' "$file_hits" | grep -vP -e "$forbidden_ctx" || true)
      [ -z "$file_hits" ] && continue
    fi

    # Attribute surviving hits to their file; the bare line numbers the scan
    # used to print were unactionable across a thousand-file tree.
    file_hits=$(printf '%s\n' "$file_hits" | sed "s|^|$f:|")
    hits="${hits}${file_hits}"$'\n'
    hit_count=$((hit_count + $(printf '%s\n' "$file_hits" | sed -n '$=')))
  done

  if [ -n "$hits" ]; then
    # Truncate with sed, never `| head`: under `set -o pipefail`, head exits
    # after N lines and the SIGPIPE on the writer kills the whole script with
    # status 141 — before it can print a verdict. A single noisy warning rule
    # would then abort the gate instead of reporting, and the pre-commit hook
    # reads a non-zero exit as failure. printf, not echo -e, so backslashes in
    # matched source lines survive.
    if [ "$severity" = "critical" ] || [ "$severity" = "error" ]; then
      echo "  [$rule_id] $message ($severity)"
      printf '%s' "$hits" | sed -n '1,5p' | sed 's/^/      /'
      BLOCKING_RULES=$((BLOCKING_RULES + 1))
      ERRORS=$((ERRORS + hit_count))
    else
      echo "  [$rule_id] $message (warning)"
      printf '%s' "$hits" | sed -n '1,3p' | sed 's/^/      /'
      WARNINGS=$((WARNINGS + hit_count))
    fi
  fi
done < <(parse_rules)

echo
echo "[pattern-scan] $ERRORS blocking, $WARNINGS warnings"

if [ $BLOCKING_RULES -gt 0 ]; then
  echo "[pattern-scan] BLOCKED — fix $ERRORS violation(s) before committing"
  exit 1
fi

echo "[pattern-scan] ✓ no blocking violations"
exit 0