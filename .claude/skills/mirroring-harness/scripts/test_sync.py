#!/usr/bin/env python3
# /// script
# requires-python = ">=3.11"
# dependencies = [
#     "pyyaml>=6.0",  # sync.py parses frontmatter with it
# ]
# ///
"""Tests for sync.py. Run: uv run .claude/skills/mirroring-harness/scripts/test_sync.py"""

from __future__ import annotations

import contextlib
import io
import os
import sys
import tempfile
import unittest
from pathlib import Path

sys.dont_write_bytecode = True  # no __pycache__ inside the tracked skill folders
sys.path.insert(0, str(Path(__file__).resolve().parent))
import sync  # noqa: E402

CONFIG = (Path(__file__).resolve().parents[4] / ".claude/harness.yaml").read_text(encoding="utf-8")

CLAUDE_MD = """# CLAUDE.md

Intro. See [the layout](https://example.com/layout).

<!-- index:rules -->
<!-- /index:rules -->

<!-- index:context -->
<!-- /index:context -->

<!-- index:skills -->
<!-- /index:skills -->
"""


class SyncTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.root = Path(self.tmp.name)
        self.put(".claude/harness.yaml", CONFIG)
        self.put(".claude/CLAUDE.md", CLAUDE_MD)
        self.put(".claude/rules/docs.md", '---\npaths:\n  - "docs/**/*.md"\n---\n\n# Docs rule\n')
        self.put(".claude/context/layout.md", "---\nread-when: Locating a component.\n---\n\n# Layout\n")
        self.put(
            ".claude/skills/doing-things/SKILL.md",
            "---\nname: doing-things\ndescription: Does things. Use when asked.\n---\n\n# Doing things\n",
        )
        self.put(".claude/skills/doing-things/scripts/run.py", "print('hi')\n")
        os.chmod(self.root / ".claude/skills/doing-things/scripts/run.py", 0o755)

    def tearDown(self):
        self.tmp.cleanup()

    def put(self, rel: str, text: str) -> Path:
        path = self.root / rel
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(text, encoding="utf-8")
        return path

    def run_cmd(self, *args: str) -> tuple[int, str]:
        out = io.StringIO()
        with contextlib.redirect_stdout(out):
            rc = sync.main([*args, "--root", str(self.root)])
        return rc, out.getvalue()

    def test_sync_writes_mirrors_and_verify_passes(self):
        rc, out = self.run_cmd("sync")
        self.assertEqual(rc, 0, out)
        agents = (self.root / "AGENTS.md").read_text(encoding="utf-8")
        self.assertTrue(agents.startswith("# AGENTS.md\n\n> **Generated file"))
        self.assertIn("`.codex/rules/docs.md`", agents)
        self.assertIn("**Before editing a file that matches a pattern, read its rule.**", agents)
        self.assertIn("`$doing-things` | Does things.", agents)
        claude = (self.root / ".claude/CLAUDE.md").read_text(encoding="utf-8")
        self.assertIn("`.claude/context/layout.md`", claude)
        self.assertIn("`/doing-things`", claude)
        copied = self.root / ".agents/skills/doing-things/scripts/run.py"
        self.assertEqual(copied.read_text(encoding="utf-8"), "print('hi')\n")
        if os.name != "nt":
            self.assertTrue(os.access(copied, os.X_OK))
        self.assertTrue((self.root / ".codex/context/layout.md").is_file())
        self.assertEqual(self.run_cmd("verify", "--quiet"), (0, ""))

    def test_verify_reports_a_hand_edited_mirror(self):
        self.run_cmd("sync")
        (self.root / ".codex/rules/docs.md").write_text("edited\n", encoding="utf-8")
        rc, out = self.run_cmd("verify", "--quiet")
        self.assertEqual(rc, 1)
        self.assertIn(".codex/rules/docs.md: drift (update)", out)

    def test_sync_deletes_orphans(self):
        self.run_cmd("sync")
        (self.root / ".claude/rules/docs.md").unlink()
        self.put(".claude/CLAUDE.md", CLAUDE_MD)
        rc, out = self.run_cmd("sync")
        self.assertEqual(rc, 0, out)
        self.assertFalse((self.root / ".codex/rules/docs.md").exists())

    @unittest.skipIf(os.name == "nt", "symlinks need privileges on Windows")
    def test_sync_replaces_symlinked_mirrors(self):
        (self.root / "AGENTS.md").symlink_to(".claude/CLAUDE.md")
        (self.root / ".agents").mkdir()
        (self.root / ".agents/skills").symlink_to("../.claude/skills")
        rc, out = self.run_cmd("verify", "--quiet")
        self.assertEqual(rc, 1)
        self.assertIn("AGENTS.md: is a symlink", out)
        rc, out = self.run_cmd("sync")
        self.assertEqual(rc, 0, out)
        self.assertFalse((self.root / "AGENTS.md").is_symlink())
        self.assertFalse((self.root / ".agents/skills").is_symlink())
        self.assertTrue((self.root / ".agents/skills/doing-things/SKILL.md").is_file())
        self.assertTrue((self.root / ".claude/skills/doing-things/SKILL.md").is_file())

    def test_missing_read_when_fails_before_writing(self):
        self.put(".claude/context/bare.md", "# No frontmatter\n")
        rc, out = self.run_cmd("sync")
        self.assertEqual(rc, 1)
        self.assertIn("read-when", out)
        self.assertFalse((self.root / "AGENTS.md").exists())

    def test_skill_name_must_match_folder(self):
        self.put(".claude/skills/other/SKILL.md", "---\nname: wrong\ndescription: X.\n---\n")
        rc, out = self.run_cmd("sync")
        self.assertEqual(rc, 1)
        self.assertIn("`name` must be 'other'", out)

    def test_oversized_agents_md_fails(self):
        self.put(".claude/CLAUDE.md", CLAUDE_MD + "x" * 33000 + "\n")
        rc, out = self.run_cmd("sync")
        self.assertEqual(rc, 1)
        self.assertIn("Codex reads only 32768", out)
        self.assertFalse((self.root / "AGENTS.md").exists())

    def test_broken_link_fails(self):
        self.put(".claude/CLAUDE.md", CLAUDE_MD + "\nSee [gone](nowhere.md) and `[code](skip.md)`.\n")
        rc, out = self.run_cmd("sync")
        self.assertEqual(rc, 1)
        self.assertIn("link to nowhere.md", out)
        self.assertNotIn("skip.md", out)


if __name__ == "__main__":
    unittest.main()
