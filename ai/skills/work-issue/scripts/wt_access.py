#!/usr/bin/env python3
# /// script
# requires-python = ">=3.11"
# dependencies = []
# ///
"""Worktree root, resume lookup and one-time access grants for work-issue.

Commands:
  root  --repo NAME [--create]        print the absolute worktree root ~/.wt/NAME
  find  --issue N                     find the issue's worktree from the current clone
  check --harness claude|codex        report whether the user-level grant is present
  apply --harness claude|codex --dry-run | --yes
                                      show (or, for Codex, write) the grant

Exit codes: 0 ok/granted/one match, 1 error, 2 usage, 3 grant missing,
4 no worktree for the issue, 5 needs attention (several matches, prunable,
locked or another clone).

The script never writes the Claude Code settings file: the agent applies the
printed diff with its edit tool so the harness asks for confirmation itself.
"""

from __future__ import annotations

import argparse
import difflib
import json
import os
import re
import shutil
import subprocess
import sys
import tempfile
import tomllib
from dataclasses import dataclass, field
from pathlib import Path

ROOT_NAME = ".wt"
NO_HOOKS = ".no-hooks"  # never created; lives under ~/.codex, which the sandbox cannot write
REPO_NAME = re.compile(r"^[A-Za-z0-9._-]+$")

EXIT_OK, EXIT_ERROR, EXIT_MISSING, EXIT_NONE, EXIT_ATTENTION = 0, 1, 3, 4, 5


class AccessError(Exception):
    """A condition the user must resolve by hand; the message says how."""


def home() -> Path:
    return Path.home()


def posix(path: Path | str) -> str:
    return Path(path).as_posix()


def same_path(a: Path | str, b: Path | str) -> bool:
    return os.path.normcase(os.path.realpath(a)) == os.path.normcase(os.path.realpath(b))


def wt_root() -> Path:
    return home() / ROOT_NAME


# ---------------------------------------------------------------- root


def cmd_root(repo: str, create: bool) -> int:
    if not REPO_NAME.match(repo):
        raise AccessError(f"invalid repository name: {repo!r}")
    path = wt_root() / repo
    if create:
        path.mkdir(parents=True, exist_ok=True)
    print(posix(path))
    return EXIT_OK


# ---------------------------------------------------------------- find


@dataclass
class Worktree:
    path: str
    head: str = ""
    branch: str = ""  # refs/heads/... or "" when detached
    detached: bool = False
    locked: bool = False
    prunable: bool = False
    rebasing: str = ""  # branch being rebased in a detached worktree
    attributes: dict[str, str] = field(default_factory=dict)


def git(args: list[str], cwd: Path | None = None) -> str:
    try:
        done = subprocess.run(["git", *args], cwd=cwd, capture_output=True, text=True, encoding="utf-8")
    except FileNotFoundError as exc:
        raise AccessError("git is not on PATH") from exc
    if done.returncode != 0:
        raise AccessError(f"git {' '.join(args)} failed: {done.stderr.strip()}")
    return done.stdout


def parse_worktrees(porcelain: str) -> list[Worktree]:
    trees: list[Worktree] = []
    for block in porcelain.strip().split("\n\n"):
        current: Worktree | None = None
        for line in block.splitlines():
            key, _, value = line.partition(" ")
            if key == "worktree":
                current = Worktree(path=value)
                trees.append(current)
            elif current is None:
                continue
            elif key == "HEAD":
                current.head = value
            elif key == "branch":
                current.branch = value
            elif key == "detached":
                current.detached = True
            elif key == "locked":
                current.locked = True
            elif key == "prunable":
                current.prunable = True
            else:
                current.attributes[key] = value
    return trees


def rebasing_branch(tree: Worktree) -> str:
    """Return refs/heads/<b> when a detached worktree is in the middle of a rebase."""
    try:
        gitdir = Path(git(["rev-parse", "--path-format=absolute", "--git-dir"], cwd=Path(tree.path)).strip())
    except AccessError:
        return ""
    for state in ("rebase-merge", "rebase-apply"):
        head_name = gitdir / state / "head-name"
        if head_name.is_file():
            return head_name.read_text(encoding="utf-8").strip()
    return ""


def issue_pattern(issue: int) -> re.Pattern[str]:
    return re.compile(rf"^refs/heads/[^/]+/{issue}-")


def cmd_find(issue: int, cwd: Path) -> int:
    trees = parse_worktrees(git(["worktree", "list", "--porcelain"], cwd=cwd))
    if not trees:
        raise AccessError("git worktree list returned nothing; run from inside a clone")
    main = trees[0]
    pattern = issue_pattern(issue)
    matches: list[Worktree] = []
    for tree in trees[1:]:
        if tree.detached and not tree.prunable:
            tree.rebasing = rebasing_branch(tree)
        if pattern.match(tree.branch) or pattern.match(tree.rebasing):
            matches.append(tree)
    print(f"main {posix(main.path)}")
    for tree in matches:
        state = "rebasing" if tree.rebasing else "prunable" if tree.prunable else "locked" if tree.locked else "ok"
        branch = (tree.branch or tree.rebasing).removeprefix("refs/heads/")
        print(f"match {state} {branch} {posix(tree.path)}")
    if not matches:
        print(f"none: no worktree has a branch */{issue}-*")
        return EXIT_NONE
    if len(matches) > 1:
        print("attention: several worktrees match this issue; ask which one to use")
        return EXIT_ATTENTION
    if matches[0].prunable or matches[0].locked:
        print("attention: the worktree is prunable or locked; ask before touching it")
        return EXIT_ATTENTION
    return EXIT_OK


# ---------------------------------------------------------------- grants


@dataclass
class Plan:
    harness: str
    target: Path
    before: str
    after: str
    missing: list[str]
    extra_files: dict[Path, tuple[str, str]] = field(default_factory=dict)

    def diff(self) -> str:
        parts = []
        for path, (old, new) in [(self.target, (self.before, self.after)), *self.extra_files.items()]:
            if old == new:
                continue
            parts.append(
                "".join(
                    difflib.unified_diff(
                        old.splitlines(keepends=True),
                        new.splitlines(keepends=True),
                        fromfile=posix(path),
                        tofile=posix(path),
                    )
                )
            )
        return "\n".join(parts)


def read_text(path: Path) -> str:
    return path.read_text(encoding="utf-8") if path.exists() else ""


# Claude Code ----------------------------------------------------------


def claude_settings() -> Path:
    return home() / ".claude" / "settings.json"


CLAUDE_EDIT_RULES = ("Edit(~/.wt/**)",)


def claude_plan() -> Plan:
    target = claude_settings()
    before = read_text(target)
    try:
        data = json.loads(before) if before.strip() else {}
    except json.JSONDecodeError as exc:
        raise AccessError(f"{posix(target)} is not valid JSON ({exc}); fix it by hand first") from exc
    if not isinstance(data, dict):
        raise AccessError(f"{posix(target)} must hold a JSON object")
    permissions = data.setdefault("permissions", {})
    if not isinstance(permissions, dict):
        raise AccessError(f"{posix(target)}: permissions must be an object")
    root = wt_root()
    missing: list[str] = []

    dirs = permissions.setdefault("additionalDirectories", [])
    if not isinstance(dirs, list):
        raise AccessError(f"{posix(target)}: permissions.additionalDirectories must be a list")
    granted = any(isinstance(d, str) and same_path(os.path.expanduser(d), root) for d in dirs)
    if not granted:
        dirs.append(posix(root))
        missing.append(f"additionalDirectories {posix(root)}")

    allow = permissions.setdefault("allow", [])
    if not isinstance(allow, list):
        raise AccessError(f"{posix(target)}: permissions.allow must be a list")
    absolute_rule = f"Edit(//{posix(root).lstrip('/')}/**)"  # `//` anchors an absolute path
    for rule in CLAUDE_EDIT_RULES:
        if rule not in allow and absolute_rule not in allow:
            allow.append(rule)
            missing.append(f"allow {rule}")

    after = before if not missing else json.dumps(data, indent=2, ensure_ascii=False) + "\n"
    return Plan("claude", target, before, after, missing)


# Codex ------------------------------------------------------------------


def codex_home() -> Path:
    override = os.environ.get("CODEX_HOME")
    return Path(override) if override else home() / ".codex"


def go_cache_roots() -> list[str]:
    go = shutil.which("go")
    if not go:
        return []
    done = subprocess.run([go, "env", "GOCACHE", "GOMODCACHE"], capture_output=True, text=True, encoding="utf-8")
    if done.returncode != 0:
        return []
    return [posix(line.strip()) for line in done.stdout.splitlines() if line.strip()]


def codex_rules() -> list[str]:
    hooks = posix(codex_home() / NO_HOOKS)
    return [
        'prefix_rule(pattern=["git", "add"], decision="allow")',
        'prefix_rule(pattern=["git", "fetch", "origin"], decision="allow")',
        f'prefix_rule(pattern=["git", "-c", "core.hooksPath={hooks}", "commit"], decision="allow")',
    ]


def toml_string(value: str) -> str:
    return json.dumps(value, ensure_ascii=False)


SECTION = "sandbox_workspace_write"
HEADER = re.compile(rf"^\s*\[\s*{SECTION}\s*\]\s*(#.*)?$")
ANY_HEADER = re.compile(r"^\s*\[")
ROOTS_LINE = re.compile(r"^(\s*writable_roots\s*=\s*)\[(.*)\](\s*(#.*)?)$")


def add_roots(text: str, wanted: list[str]) -> str:
    """Add writable roots with a minimal textual edit, or refuse when unsafe."""
    lines = text.splitlines()
    header_at = next((i for i, line in enumerate(lines) if HEADER.match(line)), None)
    if header_at is None:
        block = ["", f"[{SECTION}]", f"writable_roots = [{', '.join(toml_string(r) for r in wanted)}]"]
        body = text.rstrip("\n")
        return (body + "\n" if body else "") + "\n".join(block if body else block[1:]) + "\n"
    end = next((i for i in range(header_at + 1, len(lines)) if ANY_HEADER.match(lines[i])), len(lines))
    for i in range(header_at + 1, end):
        match = ROOTS_LINE.match(lines[i])
        if match:
            inner = match.group(2).strip().rstrip(",")
            joined = ", ".join([inner, *(toml_string(r) for r in wanted)] if inner else [toml_string(r) for r in wanted])
            lines[i] = f"{match.group(1)}[{joined}]{match.group(3)}"
            return "\n".join(lines) + "\n"
        if re.match(r"^\s*writable_roots\s*=", lines[i]):
            raise AccessError("writable_roots spans several lines; add the roots by hand")
    lines.insert(header_at + 1, f"writable_roots = [{', '.join(toml_string(r) for r in wanted)}]")
    return "\n".join(lines) + "\n"


def codex_plan() -> Plan:
    target = codex_home() / "config.toml"
    before = read_text(target)
    try:
        data = tomllib.loads(before)
    except tomllib.TOMLDecodeError as exc:
        raise AccessError(f"{posix(target)} is not valid TOML ({exc}); fix it by hand first") from exc
    if "permissions" in data or "default_permissions" in data:
        raise AccessError(
            f"{posix(target)} uses permission profiles; add '~/.wt' = \"write\" and the Go caches "
            "to the active profile by hand, then rerun check"
        )
    mode = data.get("sandbox_mode")
    if mode not in (None, "workspace-write"):
        raise AccessError(f"sandbox_mode is {mode!r}; work-issue grants apply to workspace-write only")
    section = data.get(SECTION, {})
    if not isinstance(section, dict):
        raise AccessError(f"{posix(target)}: {SECTION} must be a table")
    current = section.get("writable_roots", [])
    if not isinstance(current, list):
        raise AccessError(f"{posix(target)}: writable_roots must be a list")
    have = [os.path.expanduser(r) for r in current if isinstance(r, str)]
    wanted = [r for r in ["~/.wt", *go_cache_roots()] if not any(same_path(os.path.expanduser(r), h) for h in have)]
    missing = [f"writable_roots {r}" for r in wanted]
    if wanted and f"{SECTION}." in before.replace(" ", "") and not any(HEADER.match(l) for l in before.splitlines()):
        raise AccessError(f"{posix(target)} sets {SECTION} with dotted keys; add the roots by hand")
    after = add_roots(before, wanted) if wanted else before

    rules_path = codex_home() / "rules" / "default.rules"
    rules_before = read_text(rules_path)
    present = {line.strip() for line in rules_before.splitlines()}
    new_rules = [rule for rule in codex_rules() if rule not in present]
    missing += [f"rule {rule}" for rule in new_rules]
    rules_after = rules_before
    if new_rules:
        rules_after = (rules_before.rstrip("\n") + "\n" if rules_before.strip() else "")
        rules_after += "# work-issue: local git writes in ~/.wt worktrees (never push)\n" + "\n".join(new_rules) + "\n"
    plan = Plan("codex", target, before, after, missing)
    plan.extra_files[rules_path] = (rules_before, rules_after)
    return plan


def plan_for(harness: str) -> Plan:
    return claude_plan() if harness == "claude" else codex_plan()


def atomic_write(path: Path, text: str) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    if path.exists():
        shutil.copy2(path, path.with_name(path.name + ".bak-wt"))
    fd, tmp = tempfile.mkstemp(dir=path.parent, prefix=path.name + ".", suffix=".tmp")
    try:
        with os.fdopen(fd, "w", encoding="utf-8", newline="\n") as handle:
            handle.write(text)
        os.replace(tmp, path)
    except BaseException:
        Path(tmp).unlink(missing_ok=True)
        raise


def cmd_check(harness: str) -> int:
    plan = plan_for(harness)
    if not plan.missing:
        print(f"{harness}: granted")
        return EXIT_OK
    print(f"{harness}: missing")
    for item in plan.missing:
        print(f"  - {item}")
    return EXIT_MISSING


def cmd_apply(harness: str, dry_run: bool) -> int:
    plan = plan_for(harness)
    if not plan.missing:
        print(f"{harness}: granted, nothing to change")
        return EXIT_OK
    if dry_run:
        print(plan.diff())
        if harness == "claude":
            print("apply this diff with the edit tool; the harness asks for confirmation")
        return EXIT_MISSING
    if harness == "claude":
        raise AccessError("the Claude Code settings are written with the edit tool, never by this script")
    wt_root().mkdir(parents=True, exist_ok=True)  # Codex drops writable roots that do not exist
    atomic_write(plan.target, plan.after)
    for path, (_, new) in plan.extra_files.items():
        atomic_write(path, new)
    if plan_for(harness).missing:
        raise AccessError("the grant did not verify after writing; inspect the files and the .bak-wt backups")
    print(f"{harness}: granted; relaunch Codex so it reads the new configuration")
    return EXIT_OK


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    sub = parser.add_subparsers(dest="command", required=True)
    root = sub.add_parser("root")
    root.add_argument("--repo", required=True)
    root.add_argument("--create", action="store_true")
    find = sub.add_parser("find")
    find.add_argument("--issue", required=True, type=int)
    check = sub.add_parser("check")
    check.add_argument("--harness", required=True, choices=["claude", "codex"])
    apply = sub.add_parser("apply")
    apply.add_argument("--harness", required=True, choices=["claude", "codex"])
    mode = apply.add_mutually_exclusive_group(required=True)
    mode.add_argument("--dry-run", action="store_true")
    mode.add_argument("--yes", action="store_true")
    args = parser.parse_args(argv)
    try:
        if args.command == "root":
            return cmd_root(args.repo, args.create)
        if args.command == "find":
            return cmd_find(args.issue, Path.cwd())
        if args.command == "check":
            return cmd_check(args.harness)
        return cmd_apply(args.harness, args.dry_run)
    except AccessError as exc:
        print(f"error: {exc}", file=sys.stderr)
        return EXIT_ERROR


if __name__ == "__main__":
    sys.exit(main())
