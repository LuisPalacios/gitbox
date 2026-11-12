#!/usr/bin/env python3
# /// script
# requires-python = ">=3.11"
# dependencies = [
#     "pyyaml>=6.0",  # sync.py parses frontmatter with it
# ]
# ///
"""Tests for sync.py. Run: uv run ai/scripts/test_sync.py"""

from __future__ import annotations

import contextlib
import io
import os
import sys
import tempfile
import unittest
from pathlib import Path

sys.dont_write_bytecode = True  # no __pycache__ next to the tracked engine
sys.path.insert(0, str(Path(__file__).resolve().parent))
import sync  # noqa: E402

CONFIG = (Path(__file__).resolve().parents[1] / "harness.yaml").read_text(encoding="utf-8")

MAIN = """# Instructions

Shared intro for {{HARNESS}}. Invoke skills as `{{INVOKE}}name`.

<!-- harness:claude -->
Claude-only prose.
<!-- /harness -->

<!-- harness:codex -->
Codex-only prose.
<!-- /harness -->

## Context

<!-- index:context -->
<!-- /index:context -->

<!-- harness:codex -->
## Rules

<!-- index:rules -->
<!-- /index:rules -->

## Skills

<!-- index:skills -->
<!-- /index:skills -->
<!-- /harness -->
"""


class SyncTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.root = Path(self.tmp.name)
        self.put("ai/harness.yaml", CONFIG)
        self.put("ai/main.md", MAIN)
        self.put("ai/rules/docs.md", '---\npaths:\n  - "docs/**/*.md"\n---\n\n# Docs rule\n\nSee `{{CONTEXT}}/layout.md`.\n')
        self.put("ai/context/layout.md", "---\nread-when: Locating a component.\n---\n\n# Layout\n")
        self.put(
            "ai/skills/doing-things/SKILL.md",
            "---\nname: doing-things\ndescription: Does things. Use when asked.\n"
            "claude:\n  allowed-tools: Bash\n---\n\n# Doing things\n\n"
            "<!-- harness:codex -->\nIn Codex, invoke with `$doing-things`.\n<!-- /harness -->\n\nBody.\n",
        )
        self.put("ai/skills/doing-things/scripts/run.py", "print('{{NOT_A_TOKEN}}')\n")
        os.chmod(self.root / "ai/skills/doing-things/scripts/run.py", 0o755)

    def tearDown(self):
        self.tmp.cleanup()

    def put(self, rel: str, text: str) -> Path:
        path = self.root / rel
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(text, encoding="utf-8")
        return path

    def read(self, rel: str) -> str:
        return (self.root / rel).read_text(encoding="utf-8")

    def run_cmd(self, *args: str) -> tuple[int, str]:
        out = io.StringIO()
        with contextlib.redirect_stdout(out):
            rc = sync.main([*args, "--root", str(self.root)])
        return rc, out.getvalue()

    def sync_ok(self) -> str:
        rc, out = self.run_cmd("sync")
        self.assertEqual(rc, 0, out)
        return out

    def test_each_harness_gets_its_own_entry(self):
        self.sync_ok()
        claude, codex = self.read(".claude/CLAUDE.md"), self.read("AGENTS.md")
        self.assertTrue(claude.startswith("# CLAUDE.md\n\n> **Generated file"))
        self.assertTrue(codex.startswith("# AGENTS.md\n\n> **Generated file"))
        self.assertIn("Shared intro for Claude Code. Invoke skills as `/name`.", claude)
        self.assertIn("Shared intro for Codex. Invoke skills as `$name`.", codex)
        self.assertIn("Claude-only prose.", claude)
        self.assertNotIn("Codex-only prose.", claude)
        self.assertIn("Codex-only prose.", codex)
        self.assertNotIn("Claude-only prose.", codex)
        self.assertNotIn("<!-- harness", claude + codex)
        self.assertNotIn("\n\n\n", claude + codex)

    def test_indexes_follow_each_harness(self):
        self.sync_ok()
        claude, codex = self.read(".claude/CLAUDE.md"), self.read("AGENTS.md")
        self.assertIn("| Locating a component. | `.claude/context/layout.md` |", claude)
        self.assertIn("| Locating a component. | `.codex/context/layout.md` |", codex)
        self.assertNotIn("index:rules", claude)
        self.assertIn("| `docs/**/*.md` | `.codex/rules/docs.md` |", codex)
        self.assertIn("| `$doing-things` | Does things. |", codex)

    def test_trees_render_tokens_blocks_and_frontmatter(self):
        self.sync_ok()
        self.assertIn("`.claude/context/layout.md`", self.read(".claude/rules/docs.md"))
        self.assertIn("`.codex/context/layout.md`", self.read(".codex/rules/docs.md"))
        claude_skill = self.read(".claude/skills/doing-things/SKILL.md")
        codex_skill = self.read(".agents/skills/doing-things/SKILL.md")
        self.assertIn("allowed-tools: Bash", claude_skill)
        self.assertNotIn("claude:", claude_skill)
        self.assertNotIn("allowed-tools", codex_skill)
        self.assertNotIn("In Codex", claude_skill)
        self.assertIn("In Codex, invoke with `$doing-things`.", codex_skill)
        # Non-Markdown files are copied verbatim, tokens and modes included.
        script = self.root / ".agents/skills/doing-things/scripts/run.py"
        self.assertEqual(script.read_text(encoding="utf-8"), "print('{{NOT_A_TOKEN}}')\n")
        if os.name != "nt":
            self.assertTrue(os.access(script, os.X_OK))

    def test_untouched_frontmatter_keeps_its_bytes(self):
        self.sync_ok()
        self.assertTrue(self.read(".codex/rules/docs.md").startswith('---\npaths:\n  - "docs/**/*.md"\n---\n'))

    def test_only_globs_keep_a_master_out_of_the_other_harness(self):
        self.put("ai/harness.yaml", CONFIG.replace("  claude: []", '  claude: ["skills/doing-things/**"]'))
        self.sync_ok()
        self.assertTrue((self.root / ".claude/skills/doing-things/SKILL.md").is_file())
        self.assertFalse((self.root / ".agents/skills/doing-things").exists())
        self.assertNotIn("doing-things", self.read("AGENTS.md"))

    def test_verify_names_the_master_to_edit(self):
        self.sync_ok()
        (self.root / ".codex/rules/docs.md").write_text("hand edit\n", encoding="utf-8")
        rc, out = self.run_cmd("verify", "--quiet")
        self.assertEqual(rc, 1)
        self.assertIn(".codex/rules/docs.md: drift (update); edit ai/rules/docs.md and run sync", out)

    def test_sync_deletes_orphans(self):
        self.sync_ok()
        (self.root / "ai/rules/docs.md").unlink()
        self.sync_ok()
        self.assertFalse((self.root / ".claude/rules/docs.md").exists())
        self.assertFalse((self.root / ".codex/rules/docs.md").exists())
        self.assertEqual(self.run_cmd("verify", "--quiet"), (0, ""))

    def test_hand_edited_native_config_is_left_alone(self):
        self.put(".claude/settings.json", "{}\n")
        self.put(".codex/config.toml", "model = 'x'\n")
        self.sync_ok()
        self.assertEqual(self.read(".claude/settings.json"), "{}\n")
        self.assertEqual(self.read(".codex/config.toml"), "model = 'x'\n")

    @unittest.skipIf(os.name == "nt", "symlinks need privileges on Windows")
    def test_sync_replaces_symlinked_outputs(self):
        (self.root / "AGENTS.md").symlink_to("ai/main.md")
        rc, out = self.run_cmd("verify", "--quiet")
        self.assertEqual(rc, 1)
        self.assertIn("AGENTS.md: is a symlink", out)
        self.sync_ok()
        self.assertFalse((self.root / "AGENTS.md").is_symlink())
        self.assertEqual(self.read("ai/main.md"), MAIN)

    def test_failures_write_nothing(self):
        cases = {
            "ai/context/bare.md": ("# No frontmatter\n", "read-when"),
            "ai/skills/other/SKILL.md": ("---\nname: wrong\ndescription: X.\n---\n", "`name` must be 'other'"),
            "ai/rules/open.md": ('---\npaths: ["x"]\n---\n<!-- harness:codex -->\nnever closed\n', "never closed"),
            "ai/rules/who.md": ('---\npaths: ["x"]\n---\n<!-- harness:cursor -->\nx\n<!-- /harness -->\n', "unknown harness"),
            "ai/rules/tok.md": ('---\npaths: ["x"]\n---\nSee {{NOPE}}.\n', "unknown token"),
            "ai/rules/link.md": ('---\npaths: ["x"]\n---\nSee [gone](nowhere.md), not `[c](skip.md)`.\n', "link to nowhere.md"),
        }
        for rel, (text, expected) in cases.items():
            with self.subTest(rel=rel):
                path = self.put(rel, text)
                rc, out = self.run_cmd("sync")
                self.assertEqual(rc, 1, out)
                self.assertIn(expected, out)
                self.assertNotIn("skip.md", out)
                self.assertFalse((self.root / "AGENTS.md").exists())
                path.unlink()
                if not any(path.parent.iterdir()):
                    path.parent.rmdir()

    def test_oversized_agents_md_fails(self):
        self.put("ai/main.md", MAIN + "x" * 33000 + "\n")
        rc, out = self.run_cmd("sync")
        self.assertEqual(rc, 1)
        self.assertIn("Codex reads only 32768", out)

    def test_harness_markers_inside_code_fences_are_text(self):
        self.put("ai/context/layout.md", "---\nread-when: X.\n---\n\n```text\n<!-- harness:codex -->\n```\n")
        self.sync_ok()
        self.assertIn("<!-- harness:codex -->", self.read(".claude/context/layout.md"))


if __name__ == "__main__":
    unittest.main()
