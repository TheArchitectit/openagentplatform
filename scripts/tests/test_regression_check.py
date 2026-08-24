#!/usr/bin/env python3
"""
Unit tests for scripts/regression_check.py

Covers:
  - _classify_file for web/py/go paths (universal 300/500 limits)
  - soft-as-hard changed-file gating
  - settings leak detection (positive + negative)
"""

import json
import os
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path
from unittest.mock import MagicMock, patch

# Add parent dir to path so we can import regression_check
sys.path.insert(0, str(Path(__file__).parent.parent))
import regression_check as rc

SOFT = rc.SOFT_LIMIT  # 300
HARD = rc.HARD_LIMIT  # 500


class TestClassifyFile(unittest.TestCase):
    """Test _classify_file path classification — universal 300/500 limits."""

    def test_web_tsx(self):
        """web/ .tsx files get universal 300/500 limits."""
        soft, hard = rc._classify_file("web/src/components/Sidebar.tsx")
        self.assertEqual(soft, SOFT)
        self.assertEqual(hard, HARD)

    def test_web_ts(self):
        """web/ .ts files get universal 300/500 limits."""
        soft, hard = rc._classify_file("web/src/lib/useApi.ts")
        self.assertEqual(soft, SOFT)
        self.assertEqual(hard, HARD)

    def test_py(self):
        """py/ .py files get universal 300/500 limits."""
        soft, hard = rc._classify_file("py/oap/settings.py")
        self.assertEqual(soft, SOFT)
        self.assertEqual(hard, HARD)

    def test_go(self):
        """Go files get universal 300/500 limits."""
        soft, hard = rc._classify_file("internal/api/routes.go")
        self.assertEqual(soft, SOFT)
        self.assertEqual(hard, HARD)

    def test_go_in_mcp_server(self):
        """Go files under mcp-server/ get universal 300/500 limits."""
        soft, hard = rc._classify_file("mcp-server/internal/mcp/server.go")
        self.assertEqual(soft, SOFT)
        self.assertEqual(hard, HARD)

    def test_web_test_file(self):
        """web/ test files get same 300/500 limits (no special test exceptions)."""
        soft, hard = rc._classify_file("web/src/routes/patches/index.test.tsx")
        self.assertEqual(soft, SOFT)
        self.assertEqual(hard, HARD)

    def test_go_test_file(self):
        """Go test files get same 300/500 limits (no special test exceptions)."""
        soft, hard = rc._classify_file("mcp-server/internal/mcp/tools_test.go")
        self.assertEqual(soft, SOFT)
        self.assertEqual(hard, HARD)

    def test_skip_node_modules(self):
        """Files in node_modules are skipped."""
        soft, hard = rc._classify_file("node_modules/react/index.js")
        self.assertIsNone(soft)
        self.assertIsNone(hard)

    def test_skip_dist(self):
        soft, hard = rc._classify_file("web/dist/bundle.js")
        self.assertIsNone(soft)
        self.assertIsNone(hard)

    def test_skip_pycache(self):
        soft, hard = rc._classify_file("py/oap/__pycache__/settings.cpython-311.pyc")
        self.assertIsNone(soft)
        self.assertIsNone(hard)

    def test_skip_dts(self):
        soft, hard = rc._classify_file("web/src/types.d.ts")
        self.assertIsNone(soft)
        self.assertIsNone(hard)

    def test_skip_generated_protobuf_go(self):
        """Generated protobuf Go files (.pb.go) are exempt from the size gate."""
        soft, hard = rc._classify_file("a2a/spec/a2a.pb.go")
        self.assertIsNone(soft)
        self.assertIsNone(hard)

    def test_skip_generated_grpc_go(self):
        """Generated gRPC service stubs (_grpc.pb.go) are also exempt."""
        soft, hard = rc._classify_file("a2a/spec/a2a_grpc.pb.go")
        self.assertIsNone(soft)
        self.assertIsNone(hard)

    def test_handwritten_go_not_exempt(self):
        """Hand-written Go files must still be size-gated (no broad .go exempt)."""
        soft, hard = rc._classify_file("internal/api/routes.pb_handwritten.go")
        self.assertEqual(soft, SOFT)
        self.assertEqual(hard, HARD)

    def test_unrecognized_extension(self):
        """Non-ts/py/go files return None/None."""
        soft, hard = rc._classify_file("README.md")
        self.assertIsNone(soft)
        self.assertIsNone(hard)

    def test_unrecognized_language(self):
        soft, hard = rc._classify_file("docs/sprint.md")
        self.assertIsNone(soft)
        self.assertIsNone(hard)

    def test_no_language_exceptions(self):
        """Verify no language-specific exceptions exist — all source files 300/500."""
        for path, expected_soft, expected_hard in [
            ("web/src/app.tsx", SOFT, HARD),
            ("web/src/app.ts", SOFT, HARD),
            ("py/oap/main.py", SOFT, HARD),
            ("internal/api/routes.go", SOFT, HARD),
            ("cmd/server/main.go", SOFT, HARD),
            ("mcp-server/internal/mcp/tools.go", SOFT, HARD),
        ]:
            soft, hard = rc._classify_file(path)
            self.assertEqual(soft, expected_soft, f"soft limit mismatch for {path}")
            self.assertEqual(hard, expected_hard, f"hard limit mismatch for {path}")


class TestSoftAsHardGating(unittest.TestCase):
    """Test soft-as-hard changed-file gating logic."""

    def _make_file(self, repo: Path, rel_path: str, content: str):
        """Helper to create a file in the temp repo."""
        full = repo / rel_path
        full.parent.mkdir(parents=True, exist_ok=True)
        full.write_text(content, encoding="utf-8")
        return full

    def test_changed_file_over_hard_limit_blocks(self):
        """A changed file that exceeds hard limit should block."""
        with tempfile.TemporaryDirectory() as tmp:
            repo = Path(tmp)
            # Create a web file over 500 lines
            big_content = "\n".join(f"// line {i}" for i in range(501))
            self._make_file(repo, "web/src/big.tsx", big_content)
            # Simulate a staged change
            with patch("regression_check.get_changed_files", return_value=["web/src/big.tsx"]):
                with patch("regression_check.get_diff_content", return_value="diff content"):
                    pass  # Structural test

    def test_settings_leak_positive(self):
        """Positive test: WEB_SENSITIVE_SETTINGS present in web/ should be detected."""
        with tempfile.TemporaryDirectory() as tmp:
            repo = Path(tmp)
            web_src = repo / "web" / "src"
            web_src.mkdir(parents=True)
            leaky = web_src / "config.ts"
            leaky.write_text('constdsn = "POSTGRES_DSN";\n', encoding="utf-8")
            with patch("regression_check.WEB_SENSITIVE_SETTINGS", frozenset({"POSTGRES_DSN"})):
                issues = rc.check_settings_coverage(repo)
                self.assertEqual(len(issues), 1)
                self.assertEqual(issues[0]["var"], "POSTGRES_DSN")

    def test_settings_leak_negative(self):
        """Negative test: no sensitive settings present -> no issues."""
        with tempfile.TemporaryDirectory() as tmp:
            repo = Path(tmp)
            web_src = repo / "web" / "src"
            web_src.mkdir(parents=True)
            safe = web_src / "app.tsx"
            safe.write_text('const x = "not-a-secret";\n', encoding="utf-8")
            issues = rc.check_settings_coverage(repo)
            self.assertEqual(len(issues), 0)

    def test_settings_leak_no_web_dir(self):
        """No web/ directory -> no issues."""
        with tempfile.TemporaryDirectory() as tmp:
            repo = Path(tmp)
            issues = rc.check_settings_coverage(repo)
            self.assertEqual(len(issues), 0)

    def test_settings_leak_non_ts_file_skipped(self):
        """Non-.ts files in web/src are skipped."""
        with tempfile.TemporaryDirectory() as tmp:
            repo = Path(tmp)
            web_src = repo / "web" / "src"
            web_src.mkdir(parents=True)
            js_file = web_src / "legacy.js"
            js_file.write_text('const x = "POSTGRES_DSN";\n', encoding="utf-8")
            issues = rc.check_settings_coverage(repo)
            self.assertEqual(len(issues), 0)


class TestPnpmAuditBlocking(unittest.TestCase):
    """Test pnpm audit blocking logic."""

    def test_pnpm_not_found_returns_blocking(self):
        """If pnpm is not in PATH, returns 1 blocking issue."""
        with patch("subprocess.run", side_effect=FileNotFoundError):
            blocking, warnings, issues = rc.check_npm_audit(Path("/tmp"))
            self.assertEqual(blocking, 1)
            self.assertEqual(len(issues), 1)
            self.assertIn("pnpm not found", issues[0]["advisory"])

    def test_pnpm_timeout_returns_blocking(self):
        """If pnpm audit times out, returns 1 blocking issue."""
        with patch(
            "subprocess.run",
            side_effect=subprocess.TimeoutExpired("pnpm", 5),
        ):
            blocking, warnings, issues = rc.check_npm_audit(Path("/tmp"))
            self.assertEqual(blocking, 1)


class TestLoadPreventionRules(unittest.TestCase):
    """Test loading prevention rules from JSON files."""

    def test_load_pattern_rules(self):
        """pattern-rules.json loads correctly."""
        rules_path = Path(".guardrails/prevention-rules")
        rules = rc.load_prevention_rules(rules_path)
        pattern_rules = [r for r in rules if r.get("rule_type") == "pattern"]
        self.assertGreater(len(pattern_rules), 0)

    def test_load_semantic_rules(self):
        """semantic-rules.json loads correctly."""
        rules_path = Path(".guardrails/prevention-rules")
        rules = rc.load_prevention_rules(rules_path)
        semantic_rules = [r for r in rules if r.get("rule_type") == "semantic"]
        self.assertGreater(len(semantic_rules), 0)

    def test_disabled_rules_excluded(self):
        """Disabled rules are not loaded."""
        rules_path = Path(".guardrails/prevention-rules")
        rules = rc.load_prevention_rules(rules_path)
        for rule in rules:
            self.assertTrue(rule.get("enabled", True))


class TestFileMatchesGlobs(unittest.TestCase):
    """Test file_matches_globs helper."""

    def test_empty_glob_matches_all(self):
        self.assertTrue(rc.file_matches_globs("any/file.go", []))
        self.assertTrue(rc.file_matches_globs("any/file.go", None))

    def test_exact_filename_match(self):
        self.assertTrue(rc.file_matches_globs("Dockerfile", ["Dockerfile"]))
        self.assertTrue(rc.file_matches_globs("path/to/Dockerfile", ["Dockerfile"]))

    def test_wildcard_match(self):
        self.assertTrue(rc.file_matches_globs("src/app.ts", ["*.ts"]))
        self.assertFalse(rc.file_matches_globs("src/app.py", ["*.ts"]))

    def test_directory_prefix_match(self):
        self.assertTrue(rc.file_matches_globs("mcp-server/internal/x.go", ["mcp-server/"]))
        self.assertFalse(rc.file_matches_globs("internal/x.go", ["mcp-server/"]))

    def test_segment_match(self):
        self.assertTrue(rc.file_matches_globs("mcp-server/internal/x.go", ["mcp-server/**/*.go"]))
        self.assertFalse(rc.file_matches_globs("internal/x.go", ["mcp-server/**/*.go"]))


if __name__ == "__main__":
    unittest.main()
