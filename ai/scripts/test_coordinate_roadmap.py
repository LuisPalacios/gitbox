#!/usr/bin/env python3
# /// script
# requires-python = ">=3.11"
# dependencies = []
# ///
"""Offline regression tests for minimal roadmap updates."""

from __future__ import annotations

import importlib.util
import sys
import unittest
from pathlib import Path

sys.dont_write_bytecode = True
SCRIPT = Path(__file__).resolve().parents[1] / "skills/coordinate-issues/scripts/roadmap.py"
SPEC = importlib.util.spec_from_file_location("coordinate_roadmap", SCRIPT)
assert SPEC and SPEC.loader
roadmap = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(roadmap)


class RoadmapTests(unittest.TestCase):
    def test_minimal_idempotent_multiple_update(self):
        body = "# Roadmap\r\n\r\n- [ ] #12 First — depends on #13\r\n- [ ] #13 Next\r\n- [ ] #123 Later\r\n"
        expected = body.replace("[ ] #12 ", "[x] #12 ").replace("[ ] #13 ", "[x] #13 ")
        actual = roadmap.complete(body, [12, 13], "example/project")
        self.assertEqual(actual, expected)
        self.assertEqual(roadmap.complete(actual, [12, 13], "example/project"), expected)

    def test_only_leading_same_repo_references(self):
        body = "- [ ] #123 Other\n- [ ] #9 depends on #12\n- [ ] [#12](https://github.com/example/project/issues/12) Done\n- [ ] https://github.com/other/project/issues/12 Elsewhere\n"
        self.assertEqual(roadmap.complete(body, [12], "example/project"), body.replace("[ ] [#12]", "[x] [#12]"))

    def test_plain_url(self):
        body = "- [ ] https://github.com/example/project/issues/12 Done\n"
        self.assertEqual(roadmap.complete(body, [12], "example/project"), body.replace("[ ]", "[x]"))

    def test_mismatched_link_label_is_rejected(self):
        body = "- [ ] [#12](https://github.com/example/project/issues/13) Ambiguous\n"
        for issue in (12, 13):
            with self.assertRaises(ValueError):
                roadmap.complete(body, [issue], "example/project")

    def test_fenced_examples_do_not_count(self):
        body = "````markdown\n```text\n- [ ] #12 Example\n```\n````\n~~~text\n- [ ] #12 Example\n~~~\n- [ ] #12 Actual\n"
        self.assertEqual(roadmap.complete(body, [12], "example/project"), body.replace("[ ] #12 Actual", "[x] #12 Actual"))

    def test_ambiguous_or_missing_is_not_partially_edited(self):
        for body, issues in [("- [ ] #12 A\n- [x] #12 B\n", [12]), ("- [ ] #12 A\n", [12, 13])]:
            with self.assertRaises(ValueError):
                roadmap.complete(body, issues, "example/project")


if __name__ == "__main__":
    unittest.main()
