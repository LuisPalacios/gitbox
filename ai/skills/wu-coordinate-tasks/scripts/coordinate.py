#!/usr/bin/env python3
# /// script
# requires-python = ">=3.11"
# dependencies = []
# ///
"""Coordinate tasks from one work unit in one local Git clone.

Run with uv run. `status` reads the registry; `apply --request-file -` reads a
JSON request from stdin. This helper records state; it never publishes or runs
workers. Ownership fencing applies to this registry, not to external processes.
The v1 roadmap column stores the work-unit number without rewriting old journals.
This offline helper does not verify GitHub parent/child relationships.
"""

from __future__ import annotations

import argparse
import json
import re
import sqlite3
import subprocess
import sys
import time
from pathlib import Path

VERSION = 1
TABLES = {
    "sessions": "owner TEXT PRIMARY KEY, generation INTEGER, heartbeat REAL, active INTEGER, roadmap INTEGER",
    "claims": "issue INTEGER PRIMARY KEY, owner TEXT, generation INTEGER, state TEXT, detail TEXT",
    "resources": "name TEXT PRIMARY KEY, owner TEXT, generation INTEGER",
    "evidence": "issue INTEGER, criterion TEXT, head TEXT, base TEXT, result TEXT, detail TEXT, PRIMARY KEY(issue, criterion)",
    "actions": "id TEXT PRIMARY KEY, owner TEXT, generation INTEGER, issue INTEGER, kind TEXT, state TEXT, request TEXT, result TEXT",
}
STATES = {"reserved", "working", "blocked", "verifying", "publication-pending", "pr-open", "ready"}
DETAIL_FIELDS = {"branch", "worktree", "worker", "head", "base", "criteria", "note", "pr", "roadmap"}


class Conflict(ValueError):
    """Reject an operation without changing the registry."""


def identifier(value: object, label: str) -> str:
    if not isinstance(value, str) or not re.fullmatch(r"[A-Za-z0-9_.:-]{1,160}", value):
        raise Conflict(f"{label} must be a nonempty identifier")
    return value


def positive(value: object, label: str) -> int:
    if type(value) is not int or value < 1:
        raise Conflict(f"{label} must be a positive integer")
    return value


def registry_path(repo: Path) -> Path:
    result = subprocess.run(
        ["git", "-C", str(repo), "rev-parse", "--path-format=absolute", "--git-common-dir"],
        check=True, capture_output=True, text=True,
    )
    return Path(result.stdout.strip()).resolve() / "gitbox-coordination" / "registry.sqlite"


def open_registry(path: Path, *, write: bool) -> sqlite3.Connection:
    if write:
        path.parent.mkdir(parents=True, exist_ok=True)
    db = sqlite3.connect(path if write else path.as_uri() + "?mode=ro", uri=not write, timeout=10)
    db.row_factory = sqlite3.Row
    try:
        db.execute("BEGIN IMMEDIATE" if write else "BEGIN")
        version = db.execute("PRAGMA user_version").fetchone()[0]
        names = {r[0] for r in db.execute("SELECT name FROM sqlite_master WHERE type='table'")}
        if version == 0 and not names:
            if write:
                for name, columns in TABLES.items():
                    db.execute(f"CREATE TABLE {name} ({columns})")
                db.execute(f"PRAGMA user_version={VERSION}")
        elif version != VERSION or names != set(TABLES):
            raise Conflict("unknown or incompatible coordination registry; preserve it for investigation")
        return db
    except BaseException:
        db.close()
        raise


def snapshot(db: sqlite3.Connection) -> dict:
    if db.execute("PRAGMA user_version").fetchone()[0] == 0:
        return {table: [] for table in TABLES}
    result = {}
    for table in TABLES:
        rows = [dict(r) for r in db.execute(f"SELECT * FROM {table} ORDER BY 1")]
        for row in rows:
            if table == "sessions":
                # Old roadmap values need live validation before resuming a WU.
                row["work_unit"] = row["roadmap"]
            for field in ("detail", "request", "result") if table == "actions" else ("detail",):
                if field in row and row[field] is not None:
                    row[field] = json.loads(row[field])
        result[table] = rows
    return result


def status(path: Path) -> dict:
    if not path.exists():
        return {table: [] for table in TABLES}
    db = open_registry(path, write=False)
    try:
        return snapshot(db)
    finally:
        db.close()


def session(db: sqlite3.Connection, request: dict) -> sqlite3.Row:
    owner = identifier(request.get("owner"), "owner")
    generation = positive(request.get("generation"), "generation")
    row = db.execute("SELECT * FROM sessions WHERE owner=?", (owner,)).fetchone()
    if row is None or not row["active"] or row["generation"] != generation:
        raise Conflict("owner/generation is inactive or stale")
    db.execute("UPDATE sessions SET heartbeat=? WHERE owner=?", (time.time(), owner))
    return row


def claim(db: sqlite3.Connection, request: dict) -> sqlite3.Row:
    issue = positive(request.get("issue"), "issue")
    row = db.execute("SELECT * FROM claims WHERE issue=?", (issue,)).fetchone()
    if row is None or row["state"] == "released" or (
        row["owner"], row["generation"]
    ) != (request["owner"], request["generation"]):
        raise Conflict("issue does not belong to this owner/generation")
    return row


def check(db: sqlite3.Connection, row: sqlite3.Row) -> dict:
    detail = json.loads(row["detail"])
    criteria = detail.get("criteria", [])
    if not criteria or not detail.get("head") or not detail.get("base"):
        raise Conflict("set head, base and required criteria before verification")
    evidence = {r["criterion"]: r for r in db.execute("SELECT * FROM evidence WHERE issue=?", (row["issue"],))}
    missing = [c for c in criteria if c not in evidence or (
        evidence[c]["head"], evidence[c]["base"], evidence[c]["result"]
    ) != (detail["head"], detail["base"], "PASS")]
    if missing:
        raise Conflict("required evidence is missing, stale or not PASS: " + ", ".join(missing))
    return {"issue": row["issue"], "head": detail["head"], "base": detail["base"], "result": "PASS"}


def check_publication(db: sqlite3.Connection, row: sqlite3.Row, payload: object) -> None:
    verified = check(db, row)
    if not isinstance(payload, dict) or (
        payload.get("head"), payload.get("base")
    ) != (verified["head"], verified["base"]):
        raise Conflict("publication intent head and base must match the verified full Git object IDs")


def start(db: sqlite3.Connection, request: dict) -> dict:
    owner = identifier(request.get("owner"), "owner")
    issues = request.get("issues")
    if not isinstance(issues, list) or not issues:
        raise Conflict("issues must be a nonempty list")
    issues = sorted({positive(n, "issue") for n in issues})
    if "roadmap" in request:
        raise Conflict("use work_unit for a validated container; roadmap selection is no longer supported")
    external = request.get("external", False)
    if type(external) is not bool:
        raise Conflict("external must be a boolean")
    work_unit = request.get("work_unit")
    if external:
        if "work_unit" in request:
            raise Conflict("external reservations cannot select a work unit")
    else:
        positive(work_unit, "work_unit")
        if work_unit in issues:
            raise Conflict("reserve child tasks, not the work-unit container")
    previous = db.execute("SELECT * FROM sessions WHERE owner=?", (owner,)).fetchone()
    if previous:
        owned = [r[0] for r in db.execute("SELECT issue FROM claims WHERE owner=? AND state!='released' ORDER BY issue", (owner,))]
        if previous["active"] and owned == issues and previous["roadmap"] == work_unit:
            return {"owner": owner, "generation": previous["generation"], "issues": issues}
        raise Conflict("owner already exists with different or inactive work; use a new owner ID")
    if work_unit is not None:
        occupied = db.execute(
            "SELECT s.owner FROM sessions s JOIN claims c ON c.owner=s.owner "
            "WHERE s.active=1 AND s.roadmap=? AND c.state!='released' LIMIT 1", (work_unit,),
        ).fetchone()
        if occupied:
            raise Conflict(f"work unit {work_unit} is reserved by {occupied['owner']}")
    for issue in issues:
        row = db.execute("SELECT owner, state FROM claims WHERE issue=?", (issue,)).fetchone()
        if row and row["state"] != "released":
            raise Conflict(f"issue {issue} is reserved by {row['owner']}")
    db.execute("INSERT INTO sessions VALUES (?,1,?,1,?)", (owner, time.time(), work_unit))
    for issue in issues:
        db.execute("INSERT OR REPLACE INTO claims VALUES (?,?,1,'reserved','{}')", (issue, owner))
        db.execute("DELETE FROM evidence WHERE issue=?", (issue,))
    return {"owner": owner, "generation": 1, "issues": issues}


def takeover(db: sqlite3.Connection, request: dict) -> dict:
    old = session(db, request)
    if request.get("confirmed_stopped") is not True:
        raise Conflict("confirm old supervisor and workers stopped before takeover")
    if old["roadmap"] is None:
        raise Conflict("unscoped legacy/external session needs review and release; it cannot resume as a work-unit batch")
    if positive(request.get("work_unit"), "work_unit") != old["roadmap"]:
        raise Conflict("work_unit does not match the recorded container")
    owner = identifier(request.get("new_owner"), "new_owner")
    generation = old["generation"] + 1
    if db.execute("SELECT 1 FROM sessions WHERE owner=?", (owner,)).fetchone():
        raise Conflict("new_owner has already been used")
    db.execute("UPDATE sessions SET active=0 WHERE owner=?", (old["owner"],))
    db.execute("INSERT INTO sessions VALUES (?,?,?,1,?)", (owner, generation, time.time(), old["roadmap"]))
    db.execute("DELETE FROM evidence WHERE issue IN (SELECT issue FROM claims WHERE owner=? AND state!='released')", (old["owner"],))
    for table in ("claims", "resources", "actions"):
        db.execute(f"UPDATE {table} SET owner=?, generation=? WHERE owner=?", (owner, generation, old["owner"]))
    db.execute("UPDATE claims SET state='blocked' WHERE owner=? AND state!='released'", (owner,))
    return {"owner": owner, "generation": generation}


def update(db: sqlite3.Connection, request: dict, row: sqlite3.Row) -> dict:
    detail = json.loads(row["detail"])
    patch = request.get("detail", {})
    state = request.get("state", row["state"])
    if not isinstance(patch, dict) or set(patch) - DETAIL_FIELDS or not isinstance(state, str) or state not in STATES:
        raise Conflict("unknown detail fields or work state")
    for name in ("head", "base"):
        if name in patch and not re.fullmatch(r"[0-9a-f]{40}|[0-9a-f]{64}", str(patch[name])):
            raise Conflict(f"{name} must be a full Git object ID")
    if "criteria" in patch:
        if not isinstance(patch["criteria"], list) or not patch["criteria"]:
            raise Conflict("criteria must be a nonempty list")
        for criterion in patch["criteria"]:
            identifier(criterion, "criterion")
        if len(set(patch["criteria"])) != len(patch["criteria"]):
            raise Conflict("criteria must be unique")
    changed = any(name in patch and patch[name] != detail.get(name) for name in ("head", "base", "criteria"))
    detail.update(patch)
    if changed:
        db.execute("DELETE FROM evidence WHERE issue=?", (row["issue"],))
        if request.get("state") == "ready":
            raise Conflict("changed revision or criteria needs fresh evidence")
        state = "verifying"
    db.execute("UPDATE claims SET state=?, detail=? WHERE issue=?", (state, json.dumps(detail), row["issue"]))
    if state == "ready":
        check(db, claim(db, request))
    return {"issue": row["issue"], "state": state, "detail": detail}


def apply(path: Path, request: dict) -> dict:
    if not isinstance(request, dict):
        raise Conflict("request must be a JSON object")
    db = open_registry(path, write=True)
    try:
        result = dispatch(db, request)
        db.commit()
        return result
    except BaseException:
        db.rollback()
        raise
    finally:
        db.close()


def dispatch(db: sqlite3.Connection, request: dict) -> dict:
    op = identifier(request.get("op"), "op")
    if op == "start":
        return start(db, request)
    if op == "takeover":
        return takeover(db, request)
    current = session(db, request)
    owner, generation = current["owner"], current["generation"]
    if op == "heartbeat":
        return {"owner": owner, "generation": generation}
    if op in {"acquire", "release-resource"}:
        name = identifier(request.get("resource"), "resource")
        resource = db.execute("SELECT * FROM resources WHERE name=?", (name,)).fetchone()
        if resource and (resource["owner"], resource["generation"]) != (owner, generation):
            raise Conflict("resource belongs to another owner/generation")
        if op == "acquire":
            db.execute("INSERT OR REPLACE INTO resources VALUES (?,?,?)", (name, owner, generation))
        else:
            db.execute("DELETE FROM resources WHERE name=?", (name,))
        return {"resource": name, "held": op == "acquire"}
    if op == "result":
        action_id = identifier(request.get("id"), "id")
        issue = positive(request.get("issue"), "issue")
        action = db.execute("SELECT * FROM actions WHERE id=?", (action_id,)).fetchone()
        if action is None:
            raise Conflict("record action intent before its result")
        if (action["owner"], action["generation"], action["issue"]) != (owner, generation, issue):
            raise Conflict("action belongs to another owner/generation or issue")
        payload = json.dumps(request.get("result"), sort_keys=True)
        if action["state"] == "done" and action["result"] != payload:
            raise Conflict("action already completed with a different result")
        db.execute("UPDATE actions SET state='done', result=? WHERE id=?", (payload, action_id))
        return dict(db.execute("SELECT * FROM actions WHERE id=?", (action_id,)).fetchone())
    row = claim(db, request)
    if op == "update":
        return update(db, request, row)
    if op == "check":
        return check(db, row)
    if op == "evidence":
        criterion = identifier(request.get("criterion"), "criterion")
        detail = json.loads(row["detail"])
        if criterion not in detail.get("criteria", []) or (
            request.get("head"), request.get("base")
        ) != (detail.get("head"), detail.get("base")):
            raise Conflict("evidence does not match the required criterion and revision")
        result = request.get("result")
        if not isinstance(result, str) or result not in {"PASS", "FAIL", "PENDING"}:
            raise Conflict("evidence result must be PASS, FAIL or PENDING")
        if not request.get("detail"):
            raise Conflict("evidence needs command/steps, platform, fixture and outcome details")
        db.execute("INSERT OR REPLACE INTO evidence VALUES (?,?,?,?,?,?)", (
            row["issue"], criterion, request["head"], request["base"], result, json.dumps(request["detail"]),
        ))
        if result != "PASS" and row["state"] == "ready":
            db.execute("UPDATE claims SET state='verifying' WHERE issue=?", (row["issue"],))
        return {"issue": row["issue"], "criterion": criterion, "result": result}
    if op == "release":
        if request.get("confirmed_stopped") is not True or request.get("reason") not in ("merged", "abandoned"):
            raise Conflict("release needs stopped workers and a merged or abandoned reason")
        db.execute("UPDATE claims SET state='released' WHERE issue=?", (row["issue"],))
        return {"issue": row["issue"], "state": "released"}
    if op == "intent":
        action_id = identifier(request.get("id"), "id")
        action = db.execute("SELECT * FROM actions WHERE id=?", (action_id,)).fetchone()
        if action and (action["owner"], action["generation"], action["issue"]) != (owner, generation, row["issue"]):
            raise Conflict("action belongs to another owner/generation or issue")
        kind = identifier(request.get("kind"), "kind")
        payload = json.dumps(request.get("request", {}), sort_keys=True)
        if action:
            if (action["kind"], action["request"]) != (kind, payload):
                raise Conflict("action ID already identifies a different intent")
            if kind in {"push", "pr"} and action["state"] != "done":
                check_publication(db, row, request.get("request"))
            return dict(action)
        if kind in {"push", "pr"}:
            check_publication(db, row, request.get("request"))
        db.execute("INSERT INTO actions VALUES (?,?,?,?,?,'intent',?,NULL)", (
            action_id, owner, generation, row["issue"], kind, payload,
        ))
        return dict(db.execute("SELECT * FROM actions WHERE id=?", (action_id,)).fetchone())
    raise Conflict("unknown operation")


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--repo", type=Path, default=Path.cwd())
    commands = parser.add_subparsers(dest="command", required=True)
    commands.add_parser("status")
    mutation = commands.add_parser("apply")
    mutation.add_argument("--request-file", default="-", help="UTF-8 JSON file, or - for stdin")
    args = parser.parse_args()
    try:
        path = registry_path(args.repo)
        if args.command == "status":
            result = status(path)
        else:
            raw = sys.stdin.read() if args.request_file == "-" else Path(args.request_file).read_text(encoding="utf-8")
            result = apply(path, json.loads(raw))
        print(json.dumps(result, indent=2))
        return 0
    except (Conflict, sqlite3.Error, OSError, subprocess.SubprocessError, json.JSONDecodeError) as exc:
        print(json.dumps({"error": str(exc)}), file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
