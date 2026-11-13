#!/usr/bin/env python3
# /// script
# requires-python = ">=3.11"
# dependencies = []
# ///
"""Regression tests for safe Markdown selection and read-only verification."""

from __future__ import annotations

import contextlib
import io
import os
import shutil
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

sys.dont_write_bytecode = True
SKILL = Path(__file__).resolve().parents[1] / "skills/fixing-markdown"
sys.path.insert(0, str(SKILL / "scripts"))
import fix_md_extra as extra  # noqa: E402
import format_markdown as runner  # noqa: E402


class MarkdownTest(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name).resolve()
        self.put("ai/harness.yaml", "version: 2\n")
        self.put("ai/main.md", "# Main\n\n```\nexample\n```\n")
        for config in (".markdownlint-cli2.jsonc", ".prettierrc"):
            self.put(f"ai/skills/fixing-markdown/{config}", (SKILL / config).read_text())

    def put(self, relative: str, content: str = "# Heading\n") -> Path:
        path = self.root / relative
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(content, encoding="utf-8")
        return path

    def test_root_selection_excludes_outputs_private_files_and_dependencies(self):
        editable = ["ai/main.md", "docs/guide.md", "docs/es/guide.md", ".github/pull_request_template.md"]
        for name in editable[1:]:
            self.put(name)
        excluded = ["AGENTS.md", ".claude/CLAUDE.md", ".codex/context/guide.md",
                    ".agents/skills/example/SKILL.md", "node_modules/pkg/README.md",
                    "cmd/gui/build/bin/README.md", "dist/README.md", "tmp/note.md",
                    "CONTINUITY.md", "CONTINUITY-scratch.md", ".continuity/note.md"]
        for name in excluded:
            self.put(name)
        self.assertEqual(extra.select_targets([self.root], self.root), sorted(self.root / p for p in editable))

    def test_explicit_generated_files_and_subtrees_resolve_to_masters(self):
        context = self.put("ai/context/guide.md")
        skill = self.put("ai/skills/example/SKILL.md")
        cases = {
            "AGENTS.md": [self.root / "ai/main.md"],
            ".claude/CLAUDE.md": [self.root / "ai/main.md"],
            ".claude/context/guide.md": [context],
            ".codex/context": [context],
            ".agents/skills/example": [skill],
        }
        for target, expected in cases.items():
            with self.subTest(target=target):
                self.assertEqual(extra.select_targets([self.root / target], self.root), expected)

    def test_missing_master_and_outside_target_fail(self):
        for target in [self.root / ".agents/skills/missing/SKILL.md", self.root.parent / "outside.md"]:
            with self.subTest(target=target), self.assertRaises(ValueError):
                extra.select_targets([target], self.root)

    @unittest.skipIf(os.name == "nt", "symlinks require privileges on Windows")
    def test_symlink_aliases_cannot_reach_generated_or_outside_files(self):
        generated = self.put("AGENTS.md")
        for name, destination in [("alias.md", generated), ("alias-dir", self.root.parent)]:
            alias = self.root / name
            alias.symlink_to(destination, target_is_directory=destination.is_dir())
            with self.assertRaises(ValueError):
                extra.select_targets([alias], self.root)
        self.assertEqual(extra.select_targets([self.root], self.root), [self.root / "ai/main.md"])

    def test_duplicate_master_and_rendered_targets_do_not_repeat_work(self):
        targets = [self.root / "AGENTS.md", self.root / "ai/main.md", self.root / ".claude/CLAUDE.md"]
        self.assertEqual(extra.select_targets(targets, self.root), [self.root / "ai/main.md"])

    def run_fake_tools(self, check: bool):
        calls = []

        def fake_run(command, **kwargs):
            calls.append(command)
            if command[0] == "markdownlint-cli2":
                config = Path(command[command.index("--config") + 1]).read_text()
                self.assertNotIn('"fix":', config)
            # All tools must receive exactly the selected master, never its renders.
            self.assertEqual(command[-1], str(self.root / "ai/main.md"))
            self.assertNotIn(str(self.root / "AGENTS.md"), command)
            return subprocess.CompletedProcess(command, 0)

        with patch.object(runner.shutil, "which", side_effect=lambda name: name), \
                patch.object(runner.subprocess, "run", side_effect=fake_run), \
                contextlib.redirect_stdout(io.StringIO()):
            result = runner.run_pipeline([self.root / "AGENTS.md"], self.root, check=check)
        return result, calls

    def test_fix_changes_master_but_never_generated_files(self):
        generated = self.put("AGENTS.md", "# Generated\n\n```\nuntouched\n```\n")
        before = generated.read_bytes()
        result, calls = self.run_fake_tools(check=False)
        self.assertEqual(result, 0)
        self.assertIn("```text", (self.root / "ai/main.md").read_text())
        self.assertEqual(generated.read_bytes(), before)
        self.assertIn("--fix", calls[0])
        self.assertIn("--write", calls[1])
        self.assertNotIn("--fix", calls[2])
        self.assertIn("--check", calls[3])

    def test_check_reports_structural_findings_without_writing(self):
        master = self.root / "ai/main.md"
        before = master.read_bytes()
        result, calls = self.run_fake_tools(check=True)
        self.assertEqual(result, 1)
        self.assertEqual(master.read_bytes(), before)
        self.assertEqual(len(calls), 2)
        self.assertTrue(all("--fix" not in call and "--write" not in call for call in calls))

    def test_missing_tool_fails_before_editing(self):
        master = self.root / "ai/main.md"
        before = master.read_bytes()
        with patch.object(runner.shutil, "which", return_value=None), self.assertRaises(ValueError):
            runner.run_pipeline([master], self.root)
        self.assertEqual(master.read_bytes(), before)

    @unittest.skipUnless(shutil.which("markdownlint-cli2"), "markdownlint-cli2 is not installed")
    def test_real_markdownlint_fixes_and_checks_with_nested_config(self):
        markdownlint = shutil.which("markdownlint-cli2")
        real_run = subprocess.run
        # A target beside the nested config reproduces markdownlint's precedence rules.
        master = self.put("ai/skills/fixing-markdown/SKILL.md", "# Main\n- item\n")
        generated = self.put(".agents/skills/fixing-markdown/SKILL.md", "# Generated\n- untouched\n")
        generated_before = generated.read_bytes()

        def run_with_fake_prettier(command, **kwargs):
            if command[0] == "prettier-test-double":
                return subprocess.CompletedProcess(command, 0)
            return real_run(command, **kwargs, capture_output=True, text=True)

        with patch.object(runner.shutil, "which", side_effect=lambda name: markdownlint if name == "markdownlint-cli2" else "prettier-test-double"), \
                patch.object(runner.subprocess, "run", side_effect=run_with_fake_prettier):
            malformed = master.read_bytes()
            self.assertEqual(runner.run_pipeline([master], self.root, check=True), 1)
            self.assertEqual(master.read_bytes(), malformed)
            # MD022/MD032 require markdownlint; the structural helper cannot fix them.
            self.assertEqual(runner.run_pipeline([generated], self.root), 0)
            self.assertEqual(master.read_text(), "# Main\n\n- item\n")
            fixed = master.read_bytes()
            self.assertEqual(runner.run_pipeline([master], self.root, check=True), 0)
            self.assertEqual(master.read_bytes(), fixed)
        self.assertEqual(generated.read_bytes(), generated_before)


    @unittest.skipUnless(all(shutil.which(tool) for tool in ("bash", "git", "markdownlint-cli2")),
                         "bash, git and markdownlint-cli2 are required")
    def test_health_markdown_preserves_lint_and_configuration_failures(self):
        health = Path(__file__).resolve().parents[2] / "scripts/health.sh"
        self.put("scripts/health.sh", health.read_text(encoding="utf-8"))
        # Avoid unrelated frontend installation in this isolated health-check fixture.
        (self.root / "cmd/gui/frontend/dist").mkdir(parents=True)
        env = {key: value for key, value in os.environ.items() if not key.startswith("GIT_")}
        env.update(GIT_CONFIG_GLOBAL=os.devnull, GIT_CONFIG_NOSYSTEM="1")
        subprocess.run(["git", "init", "--quiet", str(self.root)], env=env, check=True)
        valid_config = (SKILL / ".markdownlint-cli2.jsonc").read_text(encoding="utf-8")
        cases = [
            ("clean", "# Main\n", valid_config, 0),
            ("lint finding", "# Main\n- item\n", valid_config, 1),
            ("broken configuration", "# Main\n", '{"config": invalid}', 1),
        ]
        for name, content, config, expected in cases:
            with self.subTest(case=name):
                master = self.put("ai/main.md", content)
                settings = self.put("ai/skills/fixing-markdown/.markdownlint-cli2.jsonc", config)
                before = (master.read_bytes(), settings.read_bytes())
                result = subprocess.run(["bash", "scripts/health.sh", "markdown"], cwd=self.root,
                                        env=env, capture_output=True, text=True)
                output = result.stdout + result.stderr
                self.assertEqual(result.returncode, expected, output)
                if expected:
                    self.assertRegex(output, r"FAIL\s+markdown")
                    self.assertNotIn("all checks clean", output)
                    self.assertIn("MD032" if name == "lint finding" else "Unable to parse", output)
                else:
                    self.assertIn("all checks clean", output)
                self.assertEqual((master.read_bytes(), settings.read_bytes()), before)


if __name__ == "__main__":
    unittest.main()
