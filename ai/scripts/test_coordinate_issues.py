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
from contextlib import closing
from pathlib import Path

sys.dont_write_bytecode = True
HELPER = Path(__file__).resolve().parents[1] / "skills/wu-coordinate-tasks/scripts/coordinate.py"
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

    def start(self, owner="one", issues=None, work_unit=40):
        return self.apply("start", owner=owner, issues=issues or [42], work_unit=work_unit)

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

    def test_start_requires_explicit_work_unit_and_rejects_container_as_task(self):
        for fields in ({}, {"roadmap": 40}, {"work_unit": None}, {"work_unit": True},
                       {"work_unit": 0}, {"work_unit": "40"}, {"work_unit": 42},
                       {"work_unit": 40, "roadmap": 40}, {"external": "true"}):
            with self.subTest(fields=fields), self.assertRaises(coordinate.Conflict):
                self.apply("start", issues=[42], **fields)
        self.assertEqual(coordinate.status(self.db)["claims"], [])

    def test_same_work_unit_excludes_disjoint_batches_until_all_claims_release(self):
        first = self.start(issues=[41, 42])
        self.assertEqual(first, self.start(issues=[41, 42]))
        with self.assertRaises(coordinate.Conflict):
            self.start(issues=[41, 42], work_unit=50)
        with self.assertRaisesRegex(coordinate.Conflict, "work unit 40"):
            self.start("two", [43])
        self.apply("release", issue=41, reason="merged", confirmed_stopped=True)
        with self.assertRaises(coordinate.Conflict):
            self.start("two", [43])
        self.apply("release", issue=42, reason="merged", confirmed_stopped=True)
        self.start("two", [43])
        self.assertEqual(coordinate.status(self.db)["sessions"][-1]["work_unit"], 40)

    def test_concurrent_disjoint_batches_in_same_work_unit_have_one_winner(self):
        processes = [self.child({"op": "start", "owner": owner, "issues": issues, "work_unit": 40})
                     for owner, issues in (("one", [41]), ("two", [42]))]
        outputs = [p.communicate(timeout=20) for p in processes]
        self.assertEqual(sorted(p.returncode for p in processes), [0, 1], outputs)
        snapshot = coordinate.status(self.db)
        self.assertEqual(len(snapshot["sessions"]), 1)
        self.assertEqual(len(snapshot["claims"]), 1)

    def test_independent_work_units_can_reserve_but_share_resource_exclusion(self):
        self.start()
        self.start("two", [43], work_unit=50)
        self.apply("acquire", resource="heavy-checks")
        with self.assertRaises(coordinate.Conflict):
            self.apply("acquire", owner="two", resource="heavy-checks")
        state = coordinate.status(self.db)
        self.assertEqual([s["work_unit"] for s in state["sessions"]], [40, 50])

    def test_external_reservations_only_exclude_work_and_cannot_become_batches(self):
        self.apply("start", owner="external", issues=[41], external=True)
        self.start(issues=[42])
        with self.assertRaises(coordinate.Conflict):
            self.start("two", [41], work_unit=50)
        with self.assertRaises(coordinate.Conflict):
            self.apply("start", owner="mixed", issues=[43], external=True, work_unit=50)
        with self.assertRaisesRegex(coordinate.Conflict, "unscoped"):
            self.apply("takeover", owner="external", new_owner="converted", work_unit=50, confirmed_stopped=True)
        self.apply("release", owner="external", issue=41, reason="abandoned", confirmed_stopped=True)
        self.start("two", [41], work_unit=50)

    def test_takeover_requires_matching_explicit_work_unit(self):
        self.start()
        before = coordinate.status(self.db)
        for fields in ({}, {"work_unit": 50}, {"roadmap": 40}):
            with self.subTest(fields=fields), self.assertRaises(coordinate.Conflict):
                self.apply("takeover", new_owner="two", confirmed_stopped=True, **fields)
            self.assertEqual(coordinate.status(self.db), before)
        self.apply("takeover", new_owner="two", confirmed_stopped=True, work_unit=40)

    def test_legacy_v1_data_is_read_without_migration_and_unscoped_work_needs_review(self):
        with closing(sqlite3.connect(self.db)) as db, db:
            for name, columns in coordinate.TABLES.items():
                db.execute(f"CREATE TABLE {name} ({columns})")
            db.execute("PRAGMA user_version=1")
            db.execute("INSERT INTO sessions VALUES ('legacy',1,123,1,40)")
            db.execute("INSERT INTO sessions VALUES ('unscoped',1,123,1,NULL)")
            db.execute("INSERT INTO claims VALUES (42,'legacy',1,'working',?)", (json.dumps({"note": "old work"}),))
            db.execute("INSERT INTO claims VALUES (43,'unscoped',1,'working','{}')")
            db.execute("INSERT INTO actions VALUES ('old-action','legacy',1,42,'roadmap','done','{}','{}')")
        original = self.db.read_bytes()
        state = coordinate.status(self.db)
        self.assertEqual(state["sessions"][0]["work_unit"], 40)
        self.assertIsNone(state["sessions"][1]["work_unit"])
        self.assertEqual(self.db.read_bytes(), original)
        with self.assertRaises(coordinate.Conflict):
            self.start(issues=[44])
        with self.assertRaisesRegex(coordinate.Conflict, "unscoped"):
            self.apply("takeover", owner="unscoped", new_owner="new", work_unit=50, confirmed_stopped=True)
        self.apply("takeover", owner="legacy", new_owner="new", work_unit=40, confirmed_stopped=True)
        state = coordinate.status(self.db)
        self.assertEqual(state["claims"][0]["detail"], {"note": "old work"})
        self.assertEqual(state["actions"][0]["id"], "old-action")
        self.apply("release", owner="unscoped", issue=43, reason="abandoned", confirmed_stopped=True)

    def test_concurrent_overlapping_batches_have_one_winner_and_no_partial_claim(self):
        processes = [self.child({"op": "start", "owner": owner, "issues": issues, "work_unit": work_unit})
                     for owner, issues, work_unit in (("one", [41, 42], 40), ("two", [42, 43], 50))]
        outputs = [p.communicate(timeout=20) for p in processes]
        self.assertEqual(sorted(p.returncode for p in processes), [0, 1], outputs)
        snapshot = coordinate.status(self.db)
        self.assertEqual(len(snapshot["sessions"]), 1)
        self.assertEqual(len(snapshot["claims"]), 2)
        owner = snapshot["sessions"][0]["owner"]
        self.assertTrue(all(r["owner"] == owner for r in snapshot["claims"]))
        self.assertEqual([r["issue"] for r in snapshot["claims"]], [41, 42] if owner == "one" else [42, 43])

    def test_existing_conflict_rolls_back_every_new_claim(self):
        self.start("two", [43], work_unit=50)
        with self.assertRaises(coordinate.Conflict):
            self.start("one", [41, 42, 43])
        self.assertEqual([r["issue"] for r in coordinate.status(self.db)["claims"]], [43])

    def test_crash_before_commit_rolls_back_and_after_commit_keeps_reservation(self):
        for phase in ("before", "after"):
            with self.subTest(phase=phase):
                process = self.child({"op": "start", "owner": phase, "issues": [42], "work_unit": 40}, phase)
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
        # The connection's own context only commits; closing() releases the
        # file, which Windows keeps locked and tearDown could not delete.
        with closing(sqlite3.connect(self.db)) as db, db:
            db.execute("UPDATE sessions SET heartbeat=0")
        with self.assertRaises(coordinate.Conflict):
            self.start("two")
        self.start("two", [43], work_unit=50)
        with self.assertRaises(coordinate.Conflict):
            self.apply("acquire", owner="two", resource="gui")

    def test_takeover_preserves_work_and_resources_but_fences_old_owner(self):
        self.configured()
        self.apply("update", issue=42, detail={"worker": "child-1", "branch": "fix/42-test", "worktree": "fixture"})
        self.evidence()
        self.apply("acquire", resource="gui")
        with self.assertRaises(coordinate.Conflict):
            self.apply("takeover", new_owner="two")
        result = self.apply("takeover", new_owner="two", confirmed_stopped=True, work_unit=40)
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
        self.apply("takeover", new_owner="two", confirmed_stopped=True, work_unit=40)
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
        self.apply("takeover", new_owner="two", confirmed_stopped=True, work_unit=40)
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
        with closing(sqlite3.connect(self.db)) as db, db:
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
                                input=json.dumps({"op": "start", "owner": "cli", "issues": [42], "work_unit": 40}),
                                capture_output=True, text=True, check=True)
        self.assertEqual(json.loads(result.stdout)["generation"], 1)
        self.assertEqual(coordinate.status(first)["claims"][0]["owner"], "cli")
        result = subprocess.run([sys.executable, str(HELPER), "--repo", str(repo), "apply"],
                                input="{bad json", capture_output=True, text=True)
        self.assertEqual(result.returncode, 1)
        self.assertIn("error", json.loads(result.stderr))


if __name__ == "__main__":
    unittest.main()
