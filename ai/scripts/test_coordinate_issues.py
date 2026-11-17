#!/usr/bin/env python3
# /// script
# requires-python = ">=3.11"
# dependencies = []
# ///
"""Exercise coordination with temporary registries, Git clones and processes."""

from __future__ import annotations

import importlib.util
import json
import os
import sqlite3
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

sys.dont_write_bytecode = True
HELPER = Path(__file__).resolve().parents[1] / "skills/coordinate-issues/scripts/coordinate.py"
spec = importlib.util.spec_from_file_location("coordinate", HELPER)
coordinate = importlib.util.module_from_spec(spec)
spec.loader.exec_module(coordinate)

CHILD = """
import importlib.util, json, sys, time
from pathlib import Path
sys.dont_write_bytecode = True
spec = importlib.util.spec_from_file_location('coordinate', sys.argv[1])
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)
request = json.loads(sys.stdin.readline())
if len(sys.argv) > 3 and sys.argv[3] == 'before':
    original = module.dispatch
    def dispatch(db, request):
        result = original(db, request)
        print('ready', flush=True)
        time.sleep(60)
        return result
    module.dispatch = dispatch
try:
    result = module.apply(Path(sys.argv[2]), request)
    print(json.dumps(result), flush=True)
    if len(sys.argv) > 3 and sys.argv[3] == 'after':
        time.sleep(60)
except module.Conflict as exc:
    print(str(exc), file=sys.stderr)
    sys.exit(1)
"""


class CoordinationTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(prefix="gitbox coordination ")
        self.root = Path(self.tmp.name)
        self.db = self.root / "registry.sqlite"

    def tearDown(self):
        self.tmp.cleanup()

    def apply(self, op, **kwargs):
        return coordinate.apply(self.db, {"op": op, "owner": "one", "generation": 1, **kwargs})

    def start(self, owner="one", issues=None):
        return self.apply("start", owner=owner, issues=issues or [42])

    def child(self, request, crash=None):
        command = [sys.executable, "-c", CHILD, str(HELPER), str(self.db)]
        if crash:
            command.append(crash)
        process = subprocess.Popen(command, stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
        process.stdin.write(json.dumps(request) + "\n")
        process.stdin.flush()
        return process

    def configured(self):
        self.start()
        self.apply("update", issue=42, detail={"head": "a" * 40, "base": "b" * 40, "criteria": ["unit", "smoke"]})

    def evidence(self, criterion="unit", result="PASS"):
        return self.apply("evidence", issue=42, criterion=criterion, head="a" * 40, base="b" * 40, result=result,
                          detail={"command": "fixture check", "platform": "test", "fixture": "isolated", "outcome": result})

    def test_status_does_not_create_a_registry(self):
        self.assertEqual(coordinate.status(self.db)["claims"], [])
        self.assertFalse(self.db.exists())

    def test_concurrent_overlapping_batches_have_one_winner_and_no_partial_claim(self):
        processes = [self.child({"op": "start", "owner": owner, "issues": issues})
                     for owner, issues in (("one", [41, 42]), ("two", [42, 43]))]
        outputs = [p.communicate(timeout=20) for p in processes]
        self.assertEqual(sorted(p.returncode for p in processes), [0, 1], outputs)
        snapshot = coordinate.status(self.db)
        self.assertEqual(len(snapshot["sessions"]), 1)
        self.assertEqual(len(snapshot["claims"]), 2)
        owner = snapshot["sessions"][0]["owner"]
        self.assertTrue(all(r["owner"] == owner for r in snapshot["claims"]))
        self.assertEqual([r["issue"] for r in snapshot["claims"]], [41, 42] if owner == "one" else [42, 43])

    def test_existing_conflict_rolls_back_every_new_claim(self):
        self.start("two", [43])
        with self.assertRaises(coordinate.Conflict):
            self.start("one", [41, 42, 43])
        self.assertEqual([r["issue"] for r in coordinate.status(self.db)["claims"]], [43])

    def test_crash_before_commit_rolls_back_and_after_commit_keeps_reservation(self):
        for phase in ("before", "after"):
            with self.subTest(phase=phase):
                process = self.child({"op": "start", "owner": phase, "issues": [42]}, phase)
                try:
                    self.assertTrue(process.stdout.readline())
                finally:
                    process.terminate()
                    process.communicate(timeout=10)
                rows = coordinate.status(self.db)["claims"]
                self.assertEqual(len(rows), 0 if phase == "before" else 1)
        with self.assertRaises(coordinate.Conflict):
            self.start()

    def test_heartbeat_age_never_releases_a_claim_or_resource(self):
        self.start()
        self.apply("acquire", resource="gui")
        with sqlite3.connect(self.db) as db:
            db.execute("UPDATE sessions SET heartbeat=0")
        with self.assertRaises(coordinate.Conflict):
            self.start("two")
        self.start("two", [43])
        with self.assertRaises(coordinate.Conflict):
            self.apply("acquire", owner="two", resource="gui")

    def test_takeover_preserves_work_and_resources_but_fences_old_owner(self):
        self.configured()
        self.apply("update", issue=42, detail={"worker": "child-1", "branch": "fix/42-test", "worktree": "fixture"})
        self.evidence()
        self.apply("acquire", resource="gui")
        with self.assertRaises(coordinate.Conflict):
            self.apply("takeover", new_owner="two")
        result = self.apply("takeover", new_owner="two", confirmed_stopped=True)
        self.assertEqual(result, {"owner": "two", "generation": 2})
        for request in ({"op": "heartbeat"}, {"op": "update", "issue": 42, "state": "working"},
                        {"op": "release-resource", "resource": "gui"}):
            with self.assertRaises(coordinate.Conflict):
                self.apply(**request)
        with self.assertRaises(coordinate.Conflict):
            self.apply("heartbeat", owner="two", generation=1)
        state = coordinate.status(self.db)
        self.assertEqual(state["claims"][0]["detail"]["worker"], "child-1")
        self.assertEqual(state["claims"][0]["state"], "blocked")
        self.assertEqual(state["evidence"], [])
        self.assertEqual(state["resources"][0]["owner"], "two")
        self.apply("release-resource", owner="two", generation=2, resource="gui")

    def test_revision_change_invalidates_evidence_and_rejects_stale_results(self):
        self.configured()
        self.evidence()
        self.evidence("smoke")
        self.assertEqual(self.apply("check", issue=42)["result"], "PASS")
        self.apply("update", issue=42, state="ready")
        self.apply("update", issue=42, detail={"head": "c" * 40})
        self.assertEqual(coordinate.status(self.db)["evidence"], [])
        with self.assertRaises(coordinate.Conflict):
            self.evidence()
        with self.assertRaises(coordinate.Conflict):
            self.apply("check", issue=42)

    def test_pending_failed_and_missing_checks_block_publication_intent(self):
        self.configured()
        self.evidence()
        revision = {"head": "a" * 40, "base": "b" * 40}
        for outcome in ("PENDING", "FAIL"):
            self.evidence("smoke", outcome)
            with self.assertRaises(coordinate.Conflict):
                self.apply("intent", issue=42, id="push-42", kind="push", request=revision)
        self.evidence("smoke")
        self.apply("intent", issue=42, id="push-42", kind="push", request=revision)
        self.apply("update", issue=42, detail={"criteria": ["unit", "smoke", "review"]})
        with self.assertRaises(coordinate.Conflict):
            self.apply("intent", issue=42, id="push-42", kind="push", request=revision)
        with self.assertRaises(coordinate.Conflict):
            self.apply("intent", issue=42, id="pr-42", kind="pr", request=revision)

    def test_publication_intent_cannot_reuse_old_revision_after_reverification(self):
        self.configured()
        self.evidence()
        self.evidence("smoke")
        old = {"head": "a" * 40, "base": "b" * 40}
        new = {"head": "c" * 40, "base": "d" * 40}
        for kind in ("push", "pr"):
            with self.assertRaises(coordinate.Conflict):
                self.apply("intent", issue=42, id=kind + "-missing", kind=kind)
            self.apply("intent", issue=42, id=kind + "-pending", kind=kind, request=old)
            self.apply("intent", issue=42, id=kind + "-done", kind=kind, request=old)
            self.apply("result", issue=42, id=kind + "-done", result={"verified": True})
        self.apply("update", issue=42, detail=new)
        for criterion in ("unit", "smoke"):
            self.apply("evidence", issue=42, criterion=criterion, **new, result="PASS",
                       detail={"command": "fixture check", "platform": "test", "fixture": "isolated", "outcome": "PASS"})
        for kind in ("push", "pr"):
            with self.subTest(kind=kind):
                with self.assertRaises(coordinate.Conflict):
                    self.apply("intent", issue=42, id=kind + "-pending", kind=kind, request=old)
                with self.assertRaises(coordinate.Conflict):
                    self.apply("intent", issue=42, id=kind + "-new-old", kind=kind, request=old)
                result = self.apply("intent", issue=42, id=kind + "-new", kind=kind, request=new)
                self.assertEqual(result["state"], "intent")
                done = self.apply("intent", issue=42, id=kind + "-done", kind=kind, request=old)
                self.assertEqual(done["state"], "done")

    def test_action_recovery_is_idempotent_and_never_performs_external_work(self):
        self.start()
        request = {"issue": 42, "id": "roadmap-42", "kind": "roadmap", "request": {"roadmap": 214, "issue": 42}}
        first = self.apply("intent", **request)
        self.assertEqual(first, self.apply("intent", **request))
        result = {"body_digest": "example", "verified": True}
        self.apply("result", issue=42, id="roadmap-42", result=result)
        self.assertEqual(self.apply("intent", **request)["state"], "done")
        self.apply("result", issue=42, id="roadmap-42", result=result)
        with self.assertRaises(coordinate.Conflict):
            self.apply("result", issue=42, id="roadmap-42", result={"different": True})
        with self.assertRaises(coordinate.Conflict):
            self.apply("intent", **{**request, "request": {"roadmap": 215}})
        self.apply("takeover", new_owner="two", confirmed_stopped=True)
        self.assertEqual(self.apply("intent", owner="two", generation=2, **request)["state"], "done")

    def test_release_requires_explicit_stop_and_terminal_reason(self):
        self.start()
        with self.assertRaises(coordinate.Conflict):
            self.apply("release", issue=42, reason="merged")
        with self.assertRaises(coordinate.Conflict):
            self.apply("release", issue=42, confirmed_stopped=True, reason="pr-open")
        self.apply("release", issue=42, confirmed_stopped=True, reason="merged")
        self.start("two")
        with self.assertRaises(coordinate.Conflict):
            self.apply("update", issue=42, state="working")

    def test_pending_roadmap_result_can_finish_after_release_with_owner_fencing(self):
        self.start()
        self.apply("intent", issue=42, id="roadmap-42", kind="roadmap", request={"roadmap": 214})
        self.apply("release", issue=42, confirmed_stopped=True, reason="merged")
        self.apply("takeover", new_owner="two", confirmed_stopped=True)
        with self.assertRaises(coordinate.Conflict):
            self.apply("result", issue=42, id="roadmap-42", result={"verified": True})
        done = self.apply("result", owner="two", generation=2, issue=42, id="roadmap-42", result={"verified": True})
        self.assertEqual(done["state"], "done")
        with self.assertRaises(coordinate.Conflict):
            self.apply("intent", owner="two", generation=2, issue=42, id="new-intent", kind="roadmap")

    def test_corrupt_or_unknown_registry_fails_closed(self):
        self.db.write_bytes(b"not sqlite")
        with self.assertRaises(sqlite3.DatabaseError):
            self.start()
        self.assertEqual(self.db.read_bytes(), b"not sqlite")
        self.db.unlink()
        self.start()
        with sqlite3.connect(self.db) as db:
            db.execute("PRAGMA user_version=999")
        with self.assertRaises(coordinate.Conflict):
            self.start("two", [43])
        with self.assertRaises(coordinate.Conflict):
            coordinate.status(self.db)

    def test_git_worktrees_resolve_the_same_registry_and_cli_reads_it(self):
        repo, worktree = self.root / "repo", self.root / "worktree"
        repo.mkdir()
        env = {**os.environ, "GIT_CONFIG_NOSYSTEM": "1", "GIT_CONFIG_GLOBAL": os.devnull,
               "GIT_AUTHOR_NAME": "Test", "GIT_AUTHOR_EMAIL": "test@example.invalid",
               "GIT_COMMITTER_NAME": "Test", "GIT_COMMITTER_EMAIL": "test@example.invalid"}
        for command in (["git", "init", str(repo)], ["git", "-C", str(repo), "commit", "--allow-empty", "-m", "Fixture"],
                        ["git", "-C", str(repo), "worktree", "add", "--detach", str(worktree)]):
            subprocess.run(command, env=env, check=True, capture_output=True, text=True)
        first, second = coordinate.registry_path(repo), coordinate.registry_path(worktree)
        self.assertEqual(first, second)
        result = subprocess.run([sys.executable, str(HELPER), "--repo", str(worktree), "apply"],
                                input=json.dumps({"op": "start", "owner": "cli", "issues": [42]}),
                                capture_output=True, text=True, check=True)
        self.assertEqual(json.loads(result.stdout)["generation"], 1)
        self.assertEqual(coordinate.status(first)["claims"][0]["owner"], "cli")
        result = subprocess.run([sys.executable, str(HELPER), "--repo", str(repo), "apply"],
                                input="{bad json", capture_output=True, text=True)
        self.assertEqual(result.returncode, 1)
        self.assertIn("error", json.loads(result.stderr))


if __name__ == "__main__":
    unittest.main()
