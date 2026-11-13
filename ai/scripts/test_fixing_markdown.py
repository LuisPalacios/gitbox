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
import re
import shlex
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
            self.put(f"ai/skills/fixing-markdown/{config}", (SKILL / config).read_text(encoding="utf-8"))

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
            self.assertEqual(command[:3], ["pnpm", "--silent", "dlx"])
            if command[-1] == "--version":
                return subprocess.CompletedProcess(command, 0, stdout="version", stderr="")
            calls.append(command)
            if command[3] == runner.MARKDOWNLINT:
                config = Path(command[command.index("--config") + 1]).read_text(encoding="utf-8")
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
        self.assertIn("```text", (self.root / "ai/main.md").read_text(encoding="utf-8"))
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

    def test_missing_pnpm_fails_before_editing(self):
        master = self.root / "ai/main.md"
        before = master.read_bytes()
        with patch.object(runner.shutil, "which", return_value=None), self.assertRaises(ValueError):
            runner.run_pipeline([master], self.root)
        self.assertEqual(master.read_bytes(), before)

    def test_cache_failure_prevents_all_edits(self):
        master = self.root / "ai/main.md"
        before = master.read_bytes()
        calls = []

        def failed_prepare(command, **kwargs):
            calls.append(command)
            self.assertEqual(command[-1], "--version")
            return subprocess.CompletedProcess(command, int(command[3] == runner.PRETTIER),
                                               stdout="", stderr="cache access denied")

        with patch.object(runner.shutil, "which", return_value="pnpm"), \
                patch.object(runner.subprocess, "run", side_effect=failed_prepare), \
                self.assertRaisesRegex(ValueError, "cache access denied"):
            runner.run_pipeline([master], self.root)
        self.assertEqual(len(calls), 2)
        self.assertEqual(master.read_bytes(), before)

    @contextlib.contextmanager
    def bootstrap_only_path(self):
        """Keep system utilities and bootstrap tools, without global npm tools."""
        with tempfile.TemporaryDirectory() as directory:
            for name in ("pnpm", "node", "bash", "git"):
                executable = shutil.which(name)
                if executable:
                    # Invoke the original path: npm/pnpm shims use dirname($0)
                    # to find their payload, so symlinking the shim breaks it.
                    launcher = Path(directory) / name
                    launcher.write_text(f'#!/bin/sh\nexec {shlex.quote(executable)} "$@"\n', encoding="utf-8")
                    launcher.chmod(0o755)
            with patch.dict(os.environ, PATH=os.pathsep.join([directory, "/usr/bin", "/bin"])):
                self.assertIsNone(shutil.which("markdownlint-cli2"))
                self.assertIsNone(shutil.which("prettier"))
                yield

    def snapshot(self):
        return {path.relative_to(self.root): path.read_bytes() if path.is_file() else None
                for path in self.root.rglob("*")}

    @unittest.skipIf(os.name == "nt", "POSIX shell launcher")
    def test_isolated_path_preserves_bootstrap_launcher_location(self):
        # Package-manager shims locate their payload relative to their own path.
        shim = self.put("bootstrap tools/pnpm", '#!/bin/sh\ncat "$(dirname "$0")/version.txt"\n')
        shim.chmod(0o755)
        self.put("bootstrap tools/version.txt", "fixture-pnpm\n")
        real_which = shutil.which
        with patch.object(shutil, "which", side_effect=lambda name: str(shim) if name == "pnpm" else real_which(name)):
            with self.bootstrap_only_path():
                result = subprocess.run(["pnpm", "--version"], capture_output=True, text=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout, "fixture-pnpm\n")

    @unittest.skipIf(os.name == "nt", "isolated PATH uses POSIX shell launchers")
    @unittest.skipUnless(shutil.which("pnpm") and shutil.which("node"), "pnpm and Node are required")
    def test_real_cached_tools_without_global_installations_or_repo_footprint(self):
        # A target beside the nested config reproduces markdownlint's precedence rules.
        master = self.put("ai/skills/fixing-markdown/SKILL.md", "# Main\n- item\n\n"
                          "<!-- harness:codex -->\n\nUse {{ENTRY}}.\n\n<!-- /harness -->\n")
        generated = self.put(".agents/skills/fixing-markdown/SKILL.md", "# Generated\n- untouched\n")
        with self.bootstrap_only_path():
            before = self.snapshot()
            self.assertEqual(runner.run_pipeline([master], self.root, check=True), 1)
            self.assertEqual(self.snapshot(), before)
            # MD022/MD032 require markdownlint; the structural helper cannot fix them.
            self.assertEqual(runner.run_pipeline([generated], self.root), 0)
            self.assertEqual(master.read_text(encoding="utf-8"), "# Main\n\n- item\n\n"
                             "<!-- harness:codex -->\n\nUse {{ENTRY}}.\n\n<!-- /harness -->\n")
            before[master.relative_to(self.root)] = master.read_bytes()
            self.assertEqual(self.snapshot(), before)
            self.assertEqual(runner.run_pipeline([master], self.root, check=True), 0)
            self.assertEqual(self.snapshot(), before)

    @unittest.skipUnless(all(shutil.which(tool) for tool in ("uv", "pnpm", "node")),
                         "uv, pnpm and Node are required")
    def test_real_entry_formatting_preserves_both_harnesses(self):
        # Real adjacent list/block boundaries exercise more than an isolated marker.
        shutil.copytree(SKILL.parents[1], self.root / "ai", dirs_exist_ok=True,
                        ignore=shutil.ignore_patterns("__pycache__"))
        # The renderer verifies links to these repository files, without reading them.
        self.put("assets/svg/README.md")
        self.put("scripts/ship.sh", "#!/usr/bin/env bash\n")
        entry = self.root / "ai/main.md"
        marker = re.compile(r"<!-- (?:harness:.*|/harness) -->")
        markers = marker.findall(entry.read_text(encoding="utf-8"))

        def render(action):
            result = subprocess.run(["uv", "run", "ai/scripts/sync.py", action], cwd=self.root,
                                    capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)

        render("sync")
        before = {name: (self.root / name).read_text(encoding="utf-8")
                  for name in ("AGENTS.md", ".claude/CLAUDE.md")}
        self.assertEqual(runner.run_pipeline([entry], self.root), 0)
        # Prettier can indent comments inside lists; the engine accepts indentation.
        self.assertEqual(marker.findall(entry.read_text(encoding="utf-8")), markers)
        render("sync")
        render("verify")
        for name, original in before.items():
            after = (self.root / name).read_text(encoding="utf-8")
            self.assertEqual(re.sub(r"\s+", "", original), re.sub(r"\s+", "", after), name)

    @unittest.skipIf(os.name == "nt", "isolated PATH uses POSIX shell launchers")
    @unittest.skipUnless(all(shutil.which(tool) for tool in ("bash", "git", "pnpm", "node")),
                         "bash, git, pnpm and Node are required")
    def test_health_markdown_preserves_lint_and_configuration_failures(self):
        health = Path(__file__).resolve().parents[2] / "scripts/health.sh"
        self.put("scripts/health.sh", health.read_text(encoding="utf-8"))
        # No frontend/dist: Markdown-only checks must not install/build the app.
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
                before = self.snapshot()
                with self.bootstrap_only_path():
                    env["PATH"] = os.environ["PATH"]
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
                self.assertEqual(self.snapshot(), before)


if __name__ == "__main__":
    unittest.main()
