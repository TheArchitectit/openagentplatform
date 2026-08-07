#!/usr/bin/env python3
"""
Regression Check Tool
Scans staged/unstaged changes against failure registry to detect potential regressions.

Usage:
    python scripts/regression_check.py              # Check staged changes
    python scripts/regression_check.py --unstaged   # Check unstaged changes
    python scripts/regression_check.py --all        # Check all changes
    python scripts/regression_check.py --pre-commit # Exit with error code if issues found

Environment Variables:
    FAILURE_REGISTRY_PATH: Path to registry file
    PREVENTION_RULES_PATH: Path to prevention rules directory
"""

import argparse
import fnmatch
import json
import os
import re
import subprocess
import sys
from pathlib import Path

DEFAULT_REGISTRY_PATH = Path(".guardrails/failure-registry.jsonl")
DEFAULT_RULES_PATH = Path(".guardrails/prevention-rules")


# File-size limits: ALL source files soft=300 / hard=500. No language exceptions.
FILE_SIZE_DIRS = ("web", "py", "internal", "cmd", "pkg", "a2a", "mcp-server", "secrets")
FILE_SIZE_SKIP_PARTS = ("node_modules", "dist", ".claude", "worktrees", ".venv", "__pycache__")
FILE_SIZE_SKIP_SUFFIXES = (".d.ts",)
SOFT_LIMIT = 300
HARD_LIMIT = 500


def _classify_file(rel_path: str) -> tuple[int | None, int | None]:
    """Return (soft, hard) line limits for a repo-relative source path, or
    (None, None) if the file should be skipped.

    Universal limits: soft=300, hard=500 for ALL source files regardless of language."""
    parts = rel_path.split(os.sep)
    for skip in FILE_SIZE_SKIP_PARTS:
        if skip in parts:
            return (None, None)
    for suf in FILE_SIZE_SKIP_SUFFIXES:
        if rel_path.endswith(suf):
            return (None, None)

    if rel_path.startswith("web" + os.sep) and rel_path.endswith((".ts", ".tsx")):
        return (SOFT_LIMIT, HARD_LIMIT)
    if rel_path.startswith("py" + os.sep) and rel_path.endswith(".py"):
        return (SOFT_LIMIT, HARD_LIMIT)
    if rel_path.endswith(".go"):
        return (SOFT_LIMIT, HARD_LIMIT)
    return (None, None)


def check_file_sizes(repo_root: Path) -> list[dict]:
    """Scan tracked source dirs for files over soft/hard line limits.

    Returns a list of issue dicts: {file, lines, soft, hard, severity,
    kind}. severity is 'error' (over hard) or 'warning' (over soft only).
    Sorted: hard-limit violations first (by line count desc), then warnings.
    """
    violations: list[dict] = []
    warnings: list[dict] = []

    for top in FILE_SIZE_DIRS:
        base = repo_root / top
        if not base.is_dir():
            continue
        for dirpath, _dirnames, filenames in os.walk(base):
            # Prune heavy/generated subtrees that must never be size-gated.
            parts = Path(dirpath).relative_to(base).parts
            if any(skip in _dirnames for skip in ("node_modules", "dist", ".venv")):
                # keep walking but skip those specific dirs
                _dirnames[:] = [d for d in _dirnames if d not in ("node_modules", "dist", ".venv")]
            for name in filenames:
                if not (name.endswith((".ts", ".tsx", ".py", ".go"))):
                    continue
                abs_path = Path(dirpath) / name
                try:
                    rel_path = abs_path.relative_to(repo_root).as_posix()
                except ValueError:
                    continue
                soft, hard = _classify_file(rel_path)
                if hard is None:
                    continue
                try:
                    with open(abs_path, encoding="utf-8", errors="replace") as f:
                        line_count = sum(1 for _ in f)
                except OSError:
                    continue
                if line_count > hard:
                    violations.append(
                        {
                            "file": rel_path,
                            "lines": line_count,
                            "soft": soft,
                            "hard": hard,
                            "severity": "error",
                            "kind": "hard",
                        }
                    )
                elif soft is not None and line_count > soft:
                    warnings.append(
                        {
                            "file": rel_path,
                            "lines": line_count,
                            "soft": soft,
                            "hard": hard,
                            "severity": "warning",
                            "kind": "soft",
                        }
                    )

    violations.sort(key=lambda d: d["lines"], reverse=True)
    warnings.sort(key=lambda d: d["lines"], reverse=True)
    return violations + warnings


def print_file_size_report(size_issues: list[dict]) -> None:
    """Print formatted report of file-size issues."""
    if not size_issues:
        print("✓ All source files within soft/hard line limits")
        return

    hard_count = sum(1 for i in size_issues if i["kind"] == "hard")
    soft_count = sum(1 for i in size_issues if i["kind"] == "soft")

    print("\n" + "=" * 70)
    print("FILE-SIZE CHECK")
    print("=" * 70)

    for issue in size_issues:
        severity = format_severity(issue["severity"])
        tag = "OVER HARD LIMIT" if issue["kind"] == "hard" else "over soft limit"
        print(
            f"  {severity}  {issue['file']}  ({issue['lines']} lines, "
            f"limit {issue['hard'] if issue['kind'] == 'hard' else issue['soft']})  {tag}"
        )

    print("-" * 70)
    print(
        f"  {hard_count} over hard limit (blocks commit), {soft_count} over soft limit (warning)"
    )
    print("=" * 70)


# ---------------------------------------------------------------------------
# Settings surface audit — sanity check that platform runtime settings are
# NOT exposed through the public web bundle.
#
# OAP's runtime settings (DB DSN, NATS url/certs, OIDC secret, JWT secret) live
# in py/oap/settings.py / internal/config and are owned by operators via
# .env / docker-compose. They are intentionally NOT surfaced through the web
# UI (that would leak credentials). This check therefore does NOT require every
# pydantic field to appear in the dashboard — instead it flags the OPPOSITE
# hazard: a server credential grazing into the public web bundle. Hardcoded
# credential detection proper is handled by the deploy.sh secrets gate, and by
# PREVENT-003 in pattern-rules.json.
# ---------------------------------------------------------------------------
SETTINGS_CONFIG_FILES = (
    "py/oap/settings.py",
)

# Server/runtime-internal settings that must never be rendered or shipped to
# the web UI. Presence of these in web/ src is a hard failure.
WEB_SENSITIVE_SETTINGS = frozenset(
    {
        "POSTGRES_DSN",
        "NATS_URL",
        "NATS_CERT_FILE",
        "NATS_KEY_FILE",
        "NATS_CA_FILE",
        "OIDC_CLIENT_SECRET",
        "JWT_SECRET",
        "SENTRY_DSN",
        "OIDC_ISSUER_URL",
    }
)


def check_settings_coverage(repo_root: Path) -> list[dict]:
    """Verify none of the runtime-sensitive settings leak into the web bundle.

    Returns a list of issue dicts: {var, message}. Empty = pass."""
    web_dir = repo_root / "web" / "src"
    if not web_dir.is_dir():
        return []
    leaked: list[dict] = []
    for path in web_dir.rglob("*.ts"):
        try:
            text = path.read_text(encoding="utf-8", errors="replace")
        except OSError:
            continue  # skip unreadable/generated files
        for var in WEB_SENSITIVE_SETTINGS:
            if re.search(rf"['\"]{var}['\"]", text):
                leaked.append(
                    {
                        "var": var,
                        "message": f"sensitive runtime setting {var} referenced in web bundle ({path}); operator-only config must not ship to the browser",
                    }
                )
    # de-dup by var
    seen: set[str] = set()
    out: list[dict] = []
    for item in leaked:
        if item["var"] not in seen:
            seen.add(item["var"])
            out.append(item)
    return out


def _pnpm_audit_available() -> bool:
    """True if `pnpm audit --json` is available in PATH."""
    try:
        result = subprocess.run(
            ["pnpm", "--version"],
            capture_output=True,
            text=True,
            timeout=5,
        )
        return result.returncode == 0
    except (FileNotFoundError, subprocess.TimeoutExpired):
        return False


def _pnpm_audit_title(info: dict) -> str:
    """Extract a short human-readable advisory title from a pnpm audit vuln entry."""
    via = info.get("via") or []
    for v in via:
        if isinstance(v, dict) and v.get("title"):
            return str(v["title"])
    if isinstance(via, list) and via:
        return str(via[0])
    return "(no advisory title)"


def check_npm_audit(repo_root: Path) -> tuple[int, int, list[dict]]:
    """Run `pnpm audit` on the web workspace and classify by severity × scope.

    Returns ``(blocking_count, warning_count, issues)``. Runtime (production
    ``dependencies``) HIGH/CRITICAL findings block; dev-only / moderate are
    warnings. Tooling failures themselves block (a deploy gate must not
    silently skip the audit because pnpm audit errored).
    """
    web_dir = repo_root / "web"
    if not _pnpm_audit_available():
        return (
            1,
            0,
            [
                {
                    "name": "(pnpm)",
                    "severity": "critical",
                    "is_runtime": True,
                    "advisory": "pnpm not found in PATH — cannot run `pnpm audit`",
                    "fix_available": False,
                    "effects": [],
                }
            ],
        )
    try:
        result = subprocess.run(
            ["pnpm", "audit", "--json"],
            capture_output=True,
            text=True,
            cwd=str(web_dir),
            timeout=120,
        )
    except subprocess.TimeoutExpired:
        return (
            1,
            0,
            [
                {
                    "name": "(pnpm)",
                    "severity": "critical",
                    "is_runtime": True,
                    "advisory": "`pnpm audit --json` timed out after 120s",
                    "fix_available": False,
                    "effects": [],
                }
            ],
        )
    # pnpm audit exits 0 when clean, 1 when vulns found, >1 on tool error.
    raw = result.stdout.strip()
    if not raw:
        msg = (
            result.stderr.strip()
            or f"pnpm audit exited {result.returncode} with no JSON output"
        )
        return (
            1,
            0,
            [
                {
                    "name": "(pnpm)",
                    "severity": "critical",
                    "is_runtime": True,
                    "advisory": msg,
                    "fix_available": False,
                    "effects": [],
                }
            ],
        )
    try:
        audit = json.loads(raw)
    except json.JSONDecodeError as exc:
        return (
            1,
            0,
            [
                {
                    "name": "(pnpm)",
                    "severity": "critical",
                    "is_runtime": True,
                    "advisory": f"pnpm audit JSON unparseable: {exc}",
                    "fix_available": False,
                    "effects": [],
                }
            ],
        )

    pkg_path = web_dir / "package.json"
    runtime_deps: set[str] | None = set()
    try:
        pkg = json.loads(pkg_path.read_text(encoding="utf-8"))
        runtime_deps = set((pkg.get("dependencies") or {}).keys())
    except (OSError, json.JSONDecodeError):
        runtime_deps = None

    vuln_map = audit.get("vulnerabilities") or {}
    issues: list[dict] = []
    for name, info in vuln_map.items():
        severity = str(info.get("severity", "unknown")).lower()
        effects = info.get("effects") or []
        if runtime_deps is None:
            is_runtime = True
        else:
            is_runtime = any(eff in runtime_deps for eff in effects)
        issues.append(
            {
                "name": name,
                "severity": severity,
                "is_runtime": is_runtime,
                "advisory": _pnpm_audit_title(info),
                "fix_available": bool(info.get("fixAvailable")),
                "effects": effects,
            }
        )
    blocking = [
        i for i in issues if i["is_runtime"] and i["severity"] in ("high", "critical")
    ]
    warning = [
        i
        for i in issues
        if not (i["is_runtime"] and i["severity"] in ("high", "critical"))
    ]
    return len(blocking), len(warning), issues


def print_npm_audit_report(blocking: int, warnings: int, issues: list[dict]) -> None:
    """Print formatted report of audit findings."""
    if not issues:
        print("✓ pnpm audit clean — no vulnerabilities")
        return
    print("\n" + "=" * 70)
    print("PNPM AUDIT (runtime HIGH/CRITICAL = blocking; dev-only = warning)")
    print("=" * 70)
    for i in sorted(issues, key=lambda x: (not x["is_runtime"], x["severity"])):
        scope = "RUNTIME" if i["is_runtime"] else "dev-only"
        fix = "fix available" if i["fix_available"] else "NO fix"
        print(f"  {i['severity'].upper():8s} {scope:8s} {i['name']:<32s} {fix}")
        if i["advisory"]:
            print(f"           → {i['advisory']}")
    print("-" * 70)
    print(
        f"  {blocking} blocking (runtime high/critical) | {warnings} warning(s) (dev-only/moderate/low)"
    )
    if blocking:
        print(
            "  ❌ resolve blocking vulns before deploy: `pnpm audit fix` (non-breaking)"
        )
    else:
        print("  ⓘ  no blocking vulns; warnings are dev-toolchain-only")
    print("=" * 70)


def print_settings_report(settings_issues: list[dict]) -> None:
    """Print formatted report of settings coverage issues."""
    if not settings_issues:
        print("✓ All config env vars have web settings coverage")
        return

    count = len(settings_issues)
    print("\n" + "=" * 70)
    print("SETTINGS COVERAGE CHECK")
    print("=" * 70)

    for issue in settings_issues:
        print(f"  ⚠️  MISSING  {issue['var']}")
        print(f"      {issue['message']}")

    print("-" * 70)
    print(f"  {count} setting(s) missing from web (blocks commit)")
    print("=" * 70)


def run_git_command(args: list[str]) -> tuple[int, str, str]:
    """Run a git command and return (returncode, stdout, stderr)."""
    try:
        result = subprocess.run(
            ["git"] + args, capture_output=True, text=True, cwd=Path.cwd()
        )
        return result.returncode, result.stdout, result.stderr
    except FileNotFoundError:
        return 1, "", "git command not found"


def get_changed_files(staged: bool = True, unstaged: bool = False) -> list[str]:
    """Get list of changed files from git."""
    files = []

    if staged:
        rc, stdout, _ = run_git_command(["diff", "--cached", "--name-only"])
        if rc == 0:
            files.extend(stdout.strip().split("\n") if stdout.strip() else [])

    if unstaged:
        rc, stdout, _ = run_git_command(["diff", "--name-only"])
        if rc == 0:
            files.extend(stdout.strip().split("\n") if stdout.strip() else [])

    return list({f for f in files if f})


def get_diff_content(file_path: str, staged: bool = True) -> str:
    """Get diff content for a specific file."""
    cmd = ["diff", "--cached"] if staged else ["diff"]
    rc, stdout, _ = run_git_command(cmd + ["--", file_path])
    return stdout if rc in (0, 1) else ""


def load_failure_registry(registry_path: Path) -> list[dict]:
    """Load failure entries from registry."""
    if not registry_path.exists():
        return []

    entries = []
    with open(registry_path) as f:
        for line in f:
            line = line.strip()
            if line and not line.startswith("#"):
                try:
                    entry = json.loads(line)
                    if entry.get("status") == "active":
                        entries.append(entry)
                except json.JSONDecodeError:
                    continue
    return entries


def validate_rule_regex(rule: dict) -> bool:
    """Validate regex patterns in a rule."""
    pattern = rule.get("pattern", "")
    if pattern:
        try:
            re.compile(pattern)
        except re.error as e:
            print(f"Warning: Invalid regex in rule {rule.get('rule_id')}: {e}")
            return False

    forbidden = rule.get("forbidden_context", "")
    if forbidden:
        try:
            re.compile(forbidden)
        except re.error as e:
            print(
                f"Warning: Invalid forbidden_context in rule {rule.get('rule_id')}: {e}"
            )
            return False

    return True


def load_prevention_rules(rules_path: Path) -> list[dict]:
    """Load prevention rules from rules directory."""
    rules = []

    pattern_rules_file = rules_path / "pattern-rules.json"
    if pattern_rules_file.exists():
        try:
            with open(pattern_rules_file) as f:
                data = json.load(f)
                for rule in data.get("rules", []):
                    if rule.get("enabled", True) and validate_rule_regex(rule):
                        rule["rule_type"] = "pattern"
                        rules.append(rule)
        except (OSError, json.JSONDecodeError):
            pass

    semantic_rules_file = rules_path / "semantic-rules.json"
    if semantic_rules_file.exists():
        try:
            with open(semantic_rules_file) as f:
                data = json.load(f)
                for rule in data.get("rules", []):
                    if rule.get("enabled", True):
                        rule["rule_type"] = "semantic"
                        rules.append(rule)
        except (OSError, json.JSONDecodeError):
            pass

    return rules


def check_file_against_failures(file_path: str, failures: list[dict]) -> list[dict]:
    """Check if file is in affected_files of any active failure."""
    matching_failures = []

    for failure in failures:
        affected_files = failure.get("affected_files", [])
        for affected in affected_files:
            # Use fnmatch for proper glob pattern matching
            if fnmatch.fnmatch(file_path, affected):
                matching_failures.append(failure)
                break

    return matching_failures


def file_matches_globs(rel_path: str, globs: list[str] | None) -> bool:
    """True if rel_path matches any glob in globs.

    An absent/empty file_glob list means "applies everywhere" (all files).
    Globs match the filename and/or any path segment via fnmatch, so
    'Dockerfile' matches a file literally named Dockerfile, and '*.py' matches
    any .py file at any depth. A trailing-slash glob is treated as a directory
    prefix.
    """
    if not globs:
        return True
    base = os.path.basename(rel_path)
    for g in globs:
        if g.endswith("/"):
            if g in rel_path or rel_path.startswith(g):
                return True
            continue
        if fnmatch.fnmatch(rel_path, g) or fnmatch.fnmatch(base, g):
            return True
    return False


def check_diff_against_patterns(
    diff_content: str,
    rules: list[dict],
    rel_path: str | None = None,
) -> list[dict]:
    """Check diff content against pattern rules.

    If rel_path is given, each rule is only applied when its declared
    file_glob matches the file (rules with no file_glob apply everywhere).
    This honours the rule schema and prevents an over-broad pattern from
    firing on every changed file.
    """
    violations = []

    # Extract added lines only (lines starting with +)
    added_lines = []
    for line in diff_content.split("\n"):
        if line.startswith("+") and not line.startswith("+++"):
            added_lines.append(line[1:])  # Remove the + prefix

    added_content = "\n".join(added_lines)

    for rule in rules:
        if rule.get("rule_type") != "pattern":
            continue

        # Honour the rule's declared file_glob: only apply this rule to
        # files whose path matches one of its globs. Rules with no/empty
        # file_glob apply everywhere. This prevents an over-broad pattern
        # (e.g. a Dockerfile-only rule with pattern '.*') from firing on
        # every changed file.
        if rel_path is not None and not file_matches_globs(
            rel_path, rule.get("file_glob")
        ):
            continue

        pattern = rule.get("pattern")
        if not pattern:
            continue

        try:
            if re.search(pattern, added_content, re.MULTILINE):
                # Check forbidden context if specified
                forbidden = rule.get("forbidden_context")
                if forbidden and re.search(forbidden, added_content, re.MULTILINE):
                    continue  # Context suggests this is OK

                violations.append(
                    {
                        "rule_id": rule.get("rule_id"),
                        "name": rule.get("name"),
                        "message": rule.get("message"),
                        "severity": rule.get("severity", "warning"),
                        "suggestion": rule.get("suggestion"),
                        "failure_id": rule.get("failure_id"),
                    }
                )
        except re.error:
            continue  # Invalid regex, skip

    return violations


def format_severity(severity: str) -> str:
    """Format severity with color codes (if terminal supports it)."""
    colors = {
        "critical": "\033[91m",  # Red
        "high": "\033[93m",  # Yellow
        "medium": "\033[94m",  # Blue
        "low": "\033[90m",  # Gray
        "error": "\033[91m",
        "warning": "\033[93m",
    }
    reset = "\033[0m"

    if sys.stdout.isatty():
        return f"{colors.get(severity.lower(), '')}{severity.upper()}{reset}"
    return severity.upper()


def run_regression_check(
    registry_path: Path,
    rules_path: Path,
    staged: bool = True,
    unstaged: bool = False,
    verbose: bool = False,
) -> tuple[int, list[dict]]:
    """
    Run full regression check.
    Returns (issue_count, issues_details).
    """
    issues = []

    # Load data
    failures = load_failure_registry(registry_path)
    rules = load_prevention_rules(rules_path)

    if verbose:
        print(f"Loaded {len(failures)} active failures, {len(rules)} enabled rules")

    # Get changed files
    changed_files = get_changed_files(staged=staged, unstaged=unstaged)

    if not changed_files:
        if verbose:
            print("No changed files to check")
        return 0, []

    if verbose:
        print(f"Checking {len(changed_files)} changed file(s)...")

    # Check each file
    for file_path in changed_files:
        file_issues = {
            "file": file_path,
            "failures": [],
            "violations": [],
        }

        # Check against failure registry
        matching_failures = check_file_against_failures(file_path, failures)
        if matching_failures:
            file_issues["failures"] = matching_failures

        # Check diff against pattern rules
        diff = get_diff_content(file_path, staged=staged)
        if diff:
            violations = check_diff_against_patterns(diff, rules, rel_path=file_path)
            if violations:
                file_issues["violations"] = violations

        if file_issues["failures"] or file_issues["violations"]:
            issues.append(file_issues)

    return len(issues), issues


def print_report(issues: list[dict], verbose: bool = False):
    """Print formatted report of issues."""
    if not issues:
        print("\n✓ No potential regressions detected")
        return

    print("\n" + "=" * 70)
    print("REGRESSION CHECK REPORT")
    print("=" * 70)

    for issue in issues:
        file_path = issue["file"]
        print(f"\n📄 {file_path}")
        print("-" * 70)

        # Print matching failures
        for failure in issue["failures"]:
            severity = format_severity(failure.get("severity", "medium"))
            print(f"\n  ⚠️  {severity} - Known Bug History")
            print(f"      Failure ID: {failure['failure_id']}")
            print(f"      Category: {failure.get('category', 'unknown')}")
            print(
                f"      Previous Error: {failure.get('error_message', 'N/A')[:80]}..."
            )
            print(f"      Prevention: {failure.get('prevention_rule', 'N/A')}")

        # Print pattern violations
        for violation in issue["violations"]:
            severity = format_severity(violation.get("severity", "warning"))
            print(f"\n  🚫 {severity} - Pattern Violation")
            print(f"      Rule: {violation.get('name', 'Unknown')}")
            print(f"      Message: {violation.get('message', 'N/A')}")
            if violation.get("failure_id"):
                print(f"      Related Failure: {violation['failure_id']}")
            if violation.get("suggestion"):
                print(f"      Suggestion: {violation['suggestion']}")

    print("\n" + "=" * 70)
    print(f"Total files with potential issues: {len(issues)}")
    print("=" * 70)
    print("\nReview the above carefully before committing.")


def main():
    parser = argparse.ArgumentParser(
        description="Check for potential regressions in changed code",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
Examples:
    %(prog)s                    # Check staged changes
    %(prog)s --unstaged         # Check unstaged changes
    %(prog)s --all              # Check all changes
    %(prog)s --pre-commit       # Exit with error if issues found
        """,
    )

    parser.add_argument(
        "--registry",
        "-r",
        type=Path,
        default=Path(os.getenv("FAILURE_REGISTRY_PATH", DEFAULT_REGISTRY_PATH)),
        help="Path to failure registry",
    )
    parser.add_argument(
        "--rules",
        type=Path,
        default=Path(os.getenv("PREVENTION_RULES_PATH", DEFAULT_RULES_PATH)),
        help="Path to prevention rules directory",
    )

    # What to check
    group = parser.add_mutually_exclusive_group()
    group.add_argument(
        "--staged",
        action="store_true",
        default=True,
        help="Check staged changes (default)",
    )
    group.add_argument(
        "--unstaged", "-u", action="store_true", help="Check unstaged changes"
    )
    group.add_argument(
        "--all",
        "-a",
        action="store_true",
        help="Check both staged and unstaged changes",
    )

    # Output options
    parser.add_argument(
        "--pre-commit",
        action="store_true",
        help="Exit with non-zero code if issues found (for pre-commit hooks)",
    )
    parser.add_argument("--json", action="store_true", help="Output results as JSON")
    parser.add_argument(
        "--no-file-sizes",
        action="store_true",
        help="Skip the file-size scan of source dirs",
    )
    parser.add_argument(
        "--no-settings", action="store_true", help="Skip the settings coverage check"
    )
    parser.add_argument(
        "--no-audit",
        action="store_true",
        help="Skip the pnpm audit (runtime HIGH/CRITICAL vuln) check",
    )
    parser.add_argument("--verbose", "-v", action="store_true", help="Verbose output")
    parser.add_argument(
        "--quiet", "-q", action="store_true", help="Only output on issues found"
    )
    parser.add_argument(
        "--soft-as-hard",
        action="store_true",
        help=(
            "Promote soft-limit file-size warnings to BLOCKING, but only for files "
            "changed since a base ref (see --soft-as-hard-base, default the working "
            "tree: staged+unstaged). Forces headroom: an agent that grows a file "
            "past its soft limit must split it rather than squeeze toward the hard "
            "limit. Pre-existing violators NOT touched by the current change are "
            "unaffected (tech debt, tracked separately)."
        ),
    )
    parser.add_argument(
        "--soft-as-hard-base",
        default=None,
        help=(
            "Base git ref for --soft-as-hard. Files changed since this ref "
            "(git diff <base>...HEAD, plus working-tree edits) are subject to the "
            "headroom gate. For deploy.sh use the prior release tag. If unset, "
            "defaults to the working-tree diff (staged+unstaged) — correct for "
            "pre-commit (uncommitted agent work)."
        ),
    )

    args = parser.parse_args()

    # Determine what to check
    staged = args.staged and not args.unstaged and not args.all
    unstaged = args.unstaged or args.all
    if args.all:
        staged = True

    # Run check
    count, issues = run_regression_check(
        registry_path=args.registry,
        rules_path=args.rules,
        staged=staged,
        unstaged=unstaged,
        verbose=args.verbose and not args.quiet,
    )

    # File-size check (always on unless --no-file-sizes).
    size_issues: list[dict] = []
    size_hard_count = 0
    if not args.no_file_sizes:
        size_issues = check_file_sizes(Path.cwd())
        size_hard_count = sum(1 for i in size_issues if i["kind"] == "hard")

    # Soft-as-hard: promote soft-limit violations to BLOCKING, but ONLY for
    # files changed in the working tree. This is the headroom gate.
    #
    # ALSO gates HARD-limit violations to the changed-file set. Pre-existing
    # over-limit files (tech debt) are reported as errors but do NOT block the
    # deploy gate — otherwise a repo with large legacy files would be
    # permanently red. Only a file GROWN during this release past its hard
    # limit (or past its soft limit under --soft-as-hard) blocks, forcing a
    # split. This mirrors the intent: never ship a file big enough to breach
    # the ceiling, and never squeeze a changed file toward it.
    soft_as_hard_count = 0
    soft_as_hard_files: list[dict] = []
    hard_changed_count = 0
    hard_changed_files: list[dict] = []
    if (args.soft_as_hard or not args.no_file_sizes) and args.soft_as_hard_base:
        # Release-gate mode: files changed since the base ref, plus working-tree edits.
        rc, stdout, _ = run_git_command(
            ["diff", "--name-only", f"{args.soft_as_hard_base}...HEAD"]
        )
        changed: set[str] = set()
        if rc == 0 and stdout.strip():
            changed.update(stdout.strip().split("\n"))
        changed.update(get_changed_files(staged=True, unstaged=True))
    elif args.soft_as_hard:
        # Pre-commit mode: only uncommitted working-tree edits.
        changed = set(get_changed_files(staged=True, unstaged=True))
    else:
        # No --soft-as-hard / --soft-as-hard-base: still gate HARD-limit
        # violations on files changed in the working tree, so a plain
        # `--pre-commit` blocks if the current change pushed a file past its
        # hard ceiling. (Pre-existing debt stays non-blocking.)
        changed = set(get_changed_files(staged=True, unstaged=True))
    for issue in size_issues:
        rel = issue["file"]
        in_change = rel in changed or rel.replace("/", os.sep) in changed
        if issue["kind"] == "soft":
            if args.soft_as_hard and in_change:
                soft_as_hard_count += 1
                soft_as_hard_files.append(issue)
        elif issue["kind"] == "hard":
            if in_change:
                hard_changed_count += 1
                hard_changed_files.append(issue)

    # Settings coverage check (always on unless --no-settings).
    settings_issues: list[dict] = []
    settings_count = 0
    if not args.no_settings:
        settings_issues = check_settings_coverage(Path.cwd())
        settings_count = len(settings_issues)

    # pnpm audit check (always on unless --no-audit).
    audit_blocking = 0
    audit_warnings = 0
    audit_issues: list[dict] = []
    if not args.no_audit:
        audit_blocking, audit_warnings, audit_issues = check_npm_audit(Path.cwd())

    # Output results
    if args.json:
        print(
            json.dumps(
                {
                    "issue_count": count,
                    "size_violations_hard": size_hard_count,
                    "size_violations_hard_changed": hard_changed_count,
                    "soft_as_hard_blocked": soft_as_hard_count,
                    "settings_missing": settings_count,
                    "npm_audit_blocking": audit_blocking,
                    "npm_audit_warnings": audit_warnings,
                    "issues": issues,
                    "file_sizes": size_issues,
                    "settings_coverage": settings_issues,
                    "npm_audit": audit_issues,
                },
                indent=2,
            )
        )
    else:
        if not args.quiet or count > 0:
            print_report(issues, verbose=args.verbose)
        if (
            size_issues
            and (not args.quiet or size_hard_count > 0)
            or not args.quiet
            and not size_issues
            and not args.json
        ):
            print_file_size_report(size_issues)
        if (
            settings_issues
            and (not args.quiet or settings_count > 0)
            or not args.no_settings
            and not args.quiet
            and not settings_issues
            and not args.json
        ):
            print_settings_report(settings_issues)
        if not args.no_audit and (
            not args.quiet or audit_blocking > 0 or not audit_issues
        ):
            print_npm_audit_report(audit_blocking, audit_warnings, audit_issues)
        if hard_changed_files:
            print("\n" + "=" * 70)
            print("HARD-LIMIT GATE (files grown past ceiling this release)")
            print("=" * 70)
            print(
                "  These CHANGED files exceeded the HARD limit — split them before shipping:"
            )
            for issue in hard_changed_files:
                print(
                    f"    {issue['file']}  ({issue['lines']} lines, hard {issue['hard']})"
                )
            print("=" * 70)
        if args.soft_as_hard and soft_as_hard_count > 0:
            print("\n" + "=" * 70)
            print("SOFT-AS-HARD HEADROOM GATE (--soft-as-hard)")
            print("=" * 70)
            print(
                "  These changed files exceeded the SOFT limit — split them (delegate"
            )
            print("  + impl) rather than squeezing toward the hard limit:")
            for issue in soft_as_hard_files:
                print(
                    f"    {issue['file']}  ({issue['lines']} lines, soft {issue['soft']})"
                )
            print("=" * 70)

    # Exit code: pre-commit fails on ANY failure-registry issue, file over
    # hard size limit, soft-limit headroom violation on a changed file
    # (--soft-as-hard), missing settings coverage, OR a runtime HIGH/CRITICAL
    # npm vulnerability.
    # Exit code: pre-commit fails on ANY failure-registry issue, a HARD-limit
    # violation on a file changed in the working tree (or since the base ref),
    # a soft-limit headroom violation on a changed file (--soft-as-hard),
    # missing settings coverage, OR a runtime HIGH/CRITICAL web dependency
    # vulnerability. Pre-existing over-limit files (tech debt) are reported
    # but do not block.
    blocks = (
        count > 0
        or hard_changed_count > 0
        or soft_as_hard_count > 0
        or settings_count > 0
        or audit_blocking > 0
    )
    if args.pre_commit and blocks:
        sys.exit(1)  # pragma: no cover - argparse/exits path
    sys.exit(0)
    sys.exit(0)


if __name__ == "__main__":
    main()
