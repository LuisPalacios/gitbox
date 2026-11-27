#!/usr/bin/env python3
# /// script
# requires-python = ">=3.11"
# dependencies = []
# ///
"""Regression tests for the work-issue worktree root, resume lookup and access grants."""

from __future__ import annotations

import contextlib
import io
import json
import os
import re
import shutil
import subprocess
import sys
import tempfile
import tomllib
import unittest
from pathlib import Path
from unittest.mock import patch

sys.dont_write_bytecode = True
REPO = Path(__file__).resolve().parents[2]
SKILL = REPO / "ai/skills/work-issue"
sys.path.insert(0, str(SKILL / "scripts"))
REAL_HOME = Path.home().resolve()
import wt_access as access  # noqa: E402

GIT_ENV = {
    "GIT_AUTHOR_NAME": "Test",
    "GIT_AUTHOR_EMAIL": "test@example.invalid",
    "GIT_COMMITTER_NAME": "Test",
    "GIT_COMMITTER_EMAIL": "test@example.invalid",
    "GIT_CONFIG_GLOBAL": os.devnull,
    "GIT_CONFIG_NOSYSTEM": "1",
}


class IsolatedHome(unittest.TestCase):
    """Every test runs against a scratch home; touching the real one fails the suite."""

    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.home = Path(temporary.name).resolve()
        env = {"HOME": str(self.home), "USERPROFILE": str(self.home), **GIT_ENV}
        patcher = patch.dict(os.environ, env)
        patcher.start()
        self.addCleanup(patcher.stop)
        os.environ.pop("CODEX_HOME", None)
        go = patch.object(access, "go_cache_roots", return_value=[])
        go.start()
        self.addCleanup(go.stop)
        self.assertEqual(access.home().resolve(), self.home)
        self.assertNotEqual(access.home().resolve(), REAL_HOME)

    def run_main(self, *argv: str, cwd: Path | None = None) -> tuple[int, str, str]:
        out, err = io.StringIO(), io.StringIO()
        previous = Path.cwd()
        try:
            if cwd:
                os.chdir(cwd)
            with contextlib.redirect_stdout(out), contextlib.redirect_stderr(err):
                code = access.main(list(argv))
        finally:
            os.chdir(previous)
        return code, out.getvalue(), err.getvalue()


class RootTest(IsolatedHome):
    def test_root_is_under_home_in_posix_form(self):
        code, out, _ = self.run_main("root", "--repo", "gitbox")
        self.assertEqual(code, 0)
        self.assertEqual(out.strip(), (self.home / ".wt" / "gitbox").as_posix())
        self.assertNotIn("\\", out)
        self.assertFalse((self.home / ".wt").exists())

    def test_create_makes_the_folder(self):
        self.assertEqual(self.run_main("root", "--repo", "gitbox", "--create")[0], 0)
        self.assertTrue((self.home / ".wt" / "gitbox").is_dir())

    def test_rejects_unsafe_repo_names(self):
        for name in ("../x", "a/b", ""):
            self.assertEqual(self.run_main("root", "--repo", name)[0], 1, name)


class FindTest(IsolatedHome):
    def git(self, *args: str, cwd: Path) -> str:
        return subprocess.run(["git", *args], cwd=cwd, check=True, capture_output=True, text=True).stdout

    def setUp(self):
        super().setUp()
        self.main = self.home / "src" / "gitbox"
        self.main.mkdir(parents=True)
        self.git("init", "-q", "-b", "main", cwd=self.main)
        self.git("commit", "-q", "--allow-empty", "-m", "base", cwd=self.main)
        self.root = self.home / ".wt" / "gitbox"

    def add(self, branch: str) -> Path:
        path = self.root / branch.split("/", 1)[1]
        self.git("worktree", "add", "-q", "--no-track", "-b", branch, str(path), "main", cwd=self.main)
        return path

    def test_anchored_match_ignores_longer_numbers(self):
        target = self.add("feat/352-isolation")
        self.add("fix/3520-other")
        self.add("chore/1352-other")
        code, out, _ = self.run_main("find", "--issue", "352", cwd=self.main)
        self.assertEqual(code, 0, out)
        self.assertEqual(out.count("match "), 1)
        self.assertIn(f"match ok feat/352-isolation {target.as_posix()}", out)
        self.assertIn(f"main {self.main.as_posix()}", out)

    def test_short_issue_number_does_not_match_longer_one(self):
        self.add("feat/352-isolation")
        code, out, _ = self.run_main("find", "--issue", "35", cwd=self.main)
        self.assertEqual(code, 4, out)

    def test_works_from_inside_a_worktree(self):
        target = self.add("feat/352-isolation")
        code, out, _ = self.run_main("find", "--issue", "352", cwd=target)
        self.assertEqual(code, 0, out)
        self.assertIn(f"main {self.main.as_posix()}", out)

    def test_several_matches_need_attention(self):
        self.add("feat/352-one")
        self.add("chore/352-two")
        self.assertEqual(self.run_main("find", "--issue", "352", cwd=self.main)[0], 5)

    def test_prunable_worktree_needs_attention(self):
        target = self.add("feat/352-isolation")
        shutil.rmtree(target)
        code, out, _ = self.run_main("find", "--issue", "352", cwd=self.main)
        self.assertEqual(code, 5, out)
        self.assertIn("match prunable", out)

    def test_locked_worktree_needs_attention(self):
        target = self.add("feat/352-isolation")
        self.git("worktree", "lock", str(target), cwd=self.main)
        self.assertEqual(self.run_main("find", "--issue", "352", cwd=self.main)[0], 5)

    def test_detects_a_worktree_stopped_in_a_rebase(self):
        target = self.add("feat/352-isolation")
        self.git("checkout", "-q", "--detach", cwd=target)
        gitdir = Path(self.git("rev-parse", "--path-format=absolute", "--git-dir", cwd=target).strip())
        (gitdir / "rebase-merge").mkdir()
        (gitdir / "rebase-merge" / "head-name").write_text("refs/heads/feat/352-isolation\n", encoding="utf-8")
        code, out, _ = self.run_main("find", "--issue", "352", cwd=self.main)
        self.assertEqual(code, 0, out)
        self.assertIn("match rebasing feat/352-isolation", out)

    def test_no_worktree(self):
        self.assertEqual(self.run_main("find", "--issue", "352", cwd=self.main)[0], 4)


class ClaudeGrantTest(IsolatedHome):
    def settings(self) -> Path:
        return self.home / ".claude" / "settings.json"

    def write(self, data: object) -> None:
        self.settings().parent.mkdir(parents=True, exist_ok=True)
        self.settings().write_text(json.dumps(data), encoding="utf-8")

    def test_missing_file_reports_both_parts(self):
        code, out, _ = self.run_main("check", "--harness", "claude")
        self.assertEqual(code, 3)
        self.assertIn("additionalDirectories", out)
        self.assertIn("Edit(~/.wt/**)", out)

    def test_granted_with_absolute_or_tilde_path(self):
        for entry in ((self.home / ".wt").as_posix(), "~/.wt"):
            self.write({"permissions": {"additionalDirectories": [entry], "allow": ["Edit(~/.wt/**)"]}})
            self.assertEqual(self.run_main("check", "--harness", "claude")[0], 0, entry)

    def test_partial_grant_lists_only_the_rule(self):
        self.write({"permissions": {"additionalDirectories": ["~/.wt"]}})
        code, out, _ = self.run_main("check", "--harness", "claude")
        self.assertEqual(code, 3)
        self.assertNotIn("additionalDirectories", out)
        self.assertIn("allow Edit(~/.wt/**)", out)

    def test_dry_run_shows_a_diff_and_keeps_other_keys(self):
        self.write({"model": "x", "permissions": {"allow": ["Bash(ls)"]}})
        before = self.settings().read_text(encoding="utf-8")
        code, out, _ = self.run_main("apply", "--harness", "claude", "--dry-run")
        self.assertEqual(code, 3)
        self.assertTrue(re.search(r'^\+\s+"additionalDirectories"', out, re.M), out)
        self.assertIn("Edit(~/.wt/**)", out)
        self.assertIn('"model": "x"', out)
        self.assertEqual(self.settings().read_text(encoding="utf-8"), before)

    def test_script_never_writes_claude_settings(self):
        self.write({})
        code, _, err = self.run_main("apply", "--harness", "claude", "--yes")
        self.assertEqual(code, 1)
        self.assertIn("edit tool", err)
        self.assertEqual(json.loads(self.settings().read_text(encoding="utf-8")), {})

    def test_invalid_json_is_left_untouched(self):
        self.settings().parent.mkdir(parents=True)
        self.settings().write_text("{ not json", encoding="utf-8")
        self.assertEqual(self.run_main("check", "--harness", "claude")[0], 1)
        self.assertEqual(self.settings().read_text(encoding="utf-8"), "{ not json")


class CodexGrantTest(IsolatedHome):
    def config(self) -> Path:
        return self.home / ".codex" / "config.toml"

    def rules(self) -> Path:
        return self.home / ".codex" / "rules" / "default.rules"

    def write(self, text: str) -> None:
        self.config().parent.mkdir(parents=True, exist_ok=True)
        self.config().write_text(text, encoding="utf-8")

    def test_apply_on_an_empty_home_grants_and_verifies(self):
        self.assertEqual(self.run_main("check", "--harness", "codex")[0], 3)
        code, out, err = self.run_main("apply", "--harness", "codex", "--yes")
        self.assertEqual(code, 0, err)
        self.assertIn("relaunch Codex", out)
        data = tomllib.loads(self.config().read_text(encoding="utf-8"))
        self.assertEqual(data["sandbox_workspace_write"]["writable_roots"], ["~/.wt"])
        self.assertTrue((self.home / ".wt").is_dir())
        self.assertEqual(self.run_main("check", "--harness", "codex")[0], 0)

    def test_apply_is_idempotent(self):
        self.run_main("apply", "--harness", "codex", "--yes")
        config, rules = self.config().read_text(encoding="utf-8"), self.rules().read_text(encoding="utf-8")
        code, out, _ = self.run_main("apply", "--harness", "codex", "--yes")
        self.assertEqual(code, 0)
        self.assertIn("nothing to change", out)
        self.assertEqual(self.config().read_text(encoding="utf-8"), config)
        self.assertEqual(self.rules().read_text(encoding="utf-8"), rules)

    def test_keeps_comments_tables_and_existing_roots(self):
        original = (
            '# my config\nmodel = "m"\n\n[sandbox_workspace_write]\n'
            'writable_roots = ["/opt/x"]  # keep me\nnetwork_access = false\n\n[tui]\ntheme = "dark"\n'
        )
        self.write(original)
        with patch.object(access, "go_cache_roots", return_value=["/cache/go-build"]):
            self.assertEqual(self.run_main("apply", "--harness", "codex", "--yes")[0], 0)
        text = self.config().read_text(encoding="utf-8")
        self.assertIn("# my config", text)
        self.assertIn("# keep me", text)
        data = tomllib.loads(text)
        self.assertEqual(data["sandbox_workspace_write"]["writable_roots"], ["/opt/x", "~/.wt", "/cache/go-build"])
        self.assertFalse(data["sandbox_workspace_write"]["network_access"])
        self.assertEqual(data["tui"]["theme"], "dark")
        self.assertEqual(Path(str(self.config()) + ".bak-wt").read_text(encoding="utf-8"), original)

    def test_section_without_roots_gets_the_key(self):
        self.write('[sandbox_workspace_write]\nnetwork_access = false\n[tui]\ntheme = "dark"\n')
        self.assertEqual(self.run_main("apply", "--harness", "codex", "--yes")[0], 0)
        data = tomllib.loads(self.config().read_text(encoding="utf-8"))
        self.assertEqual(data["sandbox_workspace_write"]["writable_roots"], ["~/.wt"])
        self.assertNotIn("writable_roots", data["tui"])

    def test_refuses_shapes_it_cannot_edit_safely(self):
        cases = {
            "profiles": 'default_permissions = "dev"\n[permissions.dev]\n',
            "full access": 'sandbox_mode = "danger-full-access"\n',
            "multiline": '[sandbox_workspace_write]\nwritable_roots = [\n  "/a",\n]\n',
            "dotted": 'sandbox_workspace_write.network_access = false\n',
            "invalid": "model = \n",
        }
        for name, text in cases.items():
            with self.subTest(name):
                self.write(text)
                code, _, err = self.run_main("apply", "--harness", "codex", "--yes")
                self.assertEqual(code, 1, name)
                self.assertTrue(err.startswith("error:"), err)
                self.assertEqual(self.config().read_text(encoding="utf-8"), text)

    def test_rules_cover_local_git_writes_only(self):
        self.run_main("apply", "--harness", "codex", "--yes")
        rules = [line for line in self.rules().read_text(encoding="utf-8").splitlines() if line.startswith("prefix_rule")]
        self.assertEqual(len(rules), 3)
        for forbidden in ('"push"', '"rebase"', '"branch"', '"worktree"', '"gh"', "gate.sh"):
            self.assertFalse(any(forbidden in rule for rule in rules), forbidden)
        commit = next(rule for rule in rules if '"commit"' in rule)
        hooks = (self.home / ".codex" / ".no-hooks").as_posix()
        self.assertIn(f'"core.hooksPath={hooks}"', commit)
        self.assertFalse(Path(hooks).exists())
        _, out, _ = self.run_main("check", "--harness", "codex")
        self.assertIn(f"commit with: git -c core.hooksPath={hooks} commit", out)

    def test_existing_rules_are_kept(self):
        self.rules().parent.mkdir(parents=True)
        self.rules().write_text('prefix_rule(pattern=["ls"], decision="allow")\n', encoding="utf-8")
        self.run_main("apply", "--harness", "codex", "--yes")
        text = self.rules().read_text(encoding="utf-8")
        self.assertTrue(text.startswith('prefix_rule(pattern=["ls"], decision="allow")\n'))


class ProjectAllowlistGuardTest(unittest.TestCase):
    """AC9: the checked-in allowlist never pre-authorizes publication."""

    PUBLISH = (
        "git push origin main",
        "git push -u origin feat/1-x",
        "git push --force-with-lease origin feat/1-x",
        "git -C /tmp/x push",
        "gh pr create --title x --body y",
        "gh pr merge 1 --squash",
        "gh pr edit 1 --body x",
        "gh pr comment 1 --body x",
        "gh issue create --title x",
        "gh issue edit 1 --body x",
        "gh issue comment 1 --body x",
        "gh issue close 1",
        "./scripts/gate.sh --report",
        "git rebase -i origin/main",
        "git branch -D main",
    )

    @staticmethod
    def pattern(rule: str) -> re.Pattern[str] | None:
        match = re.fullmatch(r"Bash\((.*)\)", rule)
        if not match:
            return None
        body = match.group(1)
        if body.endswith(":*"):
            body = body[:-2] + " *"
        regex = ".*".join(re.escape(part) for part in body.split("*"))
        regex = regex.replace(r"\ .*", r"(?: .*)?")  # "cmd *" also matches the bare "cmd"
        return re.compile(regex)

    def test_no_rule_matches_a_publication_command(self):
        settings = json.loads((REPO / ".claude/settings.json").read_text(encoding="utf-8"))
        rules = settings.get("permissions", {}).get("allow", [])
        self.assertTrue(rules)
        for rule in rules:
            compiled = self.pattern(rule)
            if compiled is None:
                continue
            for command in self.PUBLISH:
                self.assertIsNone(compiled.fullmatch(command), f"{rule} would allow: {command}")

    def test_guard_catches_a_broad_rule(self):
        for rule in ("Bash(git *)", "Bash(gh pr:*)", "Bash(./scripts/gate.sh *)", "Bash(git -C *)"):
            compiled = self.pattern(rule)
            self.assertTrue(any(compiled.fullmatch(c) for c in self.PUBLISH), rule)


if __name__ == "__main__":
    unittest.main(verbosity=1)
