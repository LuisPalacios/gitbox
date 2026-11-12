#!/usr/bin/env python3
# /// script
# requires-python = ">=3.11"
# dependencies = [
#     "pyyaml>=6.0",  # frontmatter and ai/harness.yaml
# ]
# ///
"""Render the Claude Code and Codex harnesses from the neutral masters in ai/.

ai/ is the only hand-edited source. Every master is rendered once per harness:
harness blocks keep only that harness's prose, tokens expand to its paths, and
`only:` globs in ai/harness.yaml keep a file out of the other harness. Files
that are not Markdown are copied byte for byte.

Usage:
    uv run ai/scripts/sync.py list
    uv run ai/scripts/sync.py diff
    uv run ai/scripts/sync.py sync
    uv run ai/scripts/sync.py verify [--quiet]

Options:
    --root PATH   repository root (default: two levels above this script)
    --quiet       print only failures (verify), for scripts/health.sh
"""

from __future__ import annotations

import argparse
import fnmatch
import re
import shutil
import sys
from dataclasses import dataclass, field
from pathlib import Path

import yaml

CONFIG = "ai/harness.yaml"
# Files that never belong in a render: OS and interpreter debris.
IGNORED_NAMES = {".DS_Store", "Thumbs.db", "__pycache__"}
INDEXES = ("rules", "context", "skills")
BLOCK_RE = re.compile(r"^<!-- harness:([a-z]+) -->$")
BLOCK_END = "<!-- /harness -->"
TOKEN_RE = re.compile(r"\{\{(!*)([A-Z_]+)\}\}")  # each leading ! escapes one level: {{!NAME}} -> {{NAME}}
LINK_RE = re.compile(r"\]\(([^)\s]+)\)")
FENCE_RE = re.compile(r"^\s*(```|~~~)")


class HarnessError(Exception):
    """A problem that stops sync before it writes anything."""


@dataclass
class Report:
    failures: list[str] = field(default_factory=list)
    warnings: list[str] = field(default_factory=list)


@dataclass
class Harness:
    """One AI tool's native layout, from ai/harness.yaml."""

    key: str
    name: str
    entry: str
    title: str
    rules: str
    context: str
    skills: str
    invoke: str
    max_bytes: int | None
    warn_bytes: int | None

    def tokens(self) -> dict[str, str]:
        return {
            "HARNESS": self.name,
            "ENTRY": self.entry,
            "RULES": self.rules,
            "CONTEXT": self.context,
            "SKILLS": self.skills,
            "INVOKE": self.invoke,
        }

    def dest(self, kind: str) -> str:
        return {"rules": self.rules, "context": self.context, "skills": self.skills}[kind]


# ── Markdown rendering ──────────────────────────────────────────────


def split_frontmatter(text: str) -> tuple[str, dict, str]:
    """Returns (raw frontmatter block incl. fences, parsed mapping, body)."""
    if not text.startswith("---"):
        return "", {}, text
    match = re.match(r"---\r?\n(.*?)\r?\n---\r?\n?", text, re.S)
    if not match:
        return "", {}, text
    data = yaml.safe_load(match.group(1)) or {}
    if not isinstance(data, dict):
        raise HarnessError("frontmatter is not a mapping")
    return match.group(0), data, text[match.end():]


def select_blocks(text: str, harness: str, known: set[str], where: str) -> str:
    """Keeps shared lines and this harness's blocks; drops the other harnesses' blocks."""
    out, active, fenced = [], None, False
    for n, line in enumerate(text.split("\n"), 1):
        stripped = line.strip()
        if FENCE_RE.match(line):
            fenced = not fenced
        start = None if fenced else BLOCK_RE.match(stripped)
        if start:
            if active is not None:
                raise HarnessError(f"{where}:{n}: harness block opened inside another")
            if start.group(1) not in known:
                raise HarnessError(f"{where}:{n}: unknown harness {start.group(1)!r}")
            active = start.group(1)
            continue
        if not fenced and stripped == BLOCK_END:
            if active is None:
                raise HarnessError(f"{where}:{n}: {BLOCK_END} without an opening block")
            active = None
            continue
        if active is None or active == harness:
            out.append(line)
    if active is not None:
        raise HarnessError(f"{where}: harness block {active!r} is never closed")
    return collapse_blank_lines("\n".join(out))


def collapse_blank_lines(text: str) -> str:
    """A dropped block must not leave two blank lines behind (MD012)."""
    out, fenced = [], False
    for line in text.split("\n"):
        if FENCE_RE.match(line):
            fenced = not fenced
        if not fenced and line.strip() == "" and out and out[-1].strip() == "":
            continue
        out.append(line)
    return "\n".join(out)


def expand_tokens(text: str, harness: Harness, where: str) -> str:
    tokens = harness.tokens()

    def sub(m: re.Match) -> str:
        escaped, name = m.groups()
        if escaped:
            return "{{" + escaped[1:] + name + "}}"
        if name not in tokens:
            raise HarnessError(f"{where}: unknown token {{{{{name}}}}}")
        return tokens[name]

    return TOKEN_RE.sub(sub, text)


def render_frontmatter(raw: str, data: dict, harness: Harness, known: set[str]) -> str:
    """Merges the harness's own frontmatter keys; untouched bytes when there are none."""
    if not any(k in data for k in known):
        return raw
    merged = {k: v for k, v in data.items() if k not in known}
    own = data.get(harness.key) or {}
    if not isinstance(own, dict):
        raise HarnessError(f"frontmatter key {harness.key!r} must be a mapping")
    merged.update(own)
    dumped = yaml.safe_dump(merged, sort_keys=False, allow_unicode=True, width=10_000)
    return f"---\n{dumped}---\n"


def render_markdown(text: str, harness: Harness, known: set[str], where: str) -> str:
    raw, data, body = split_frontmatter(text)
    head = render_frontmatter(raw, data, harness, known)
    body = select_blocks(body, harness.key, known, where)
    return expand_tokens(head + body, harness, where)


def first_sentence(text: str) -> str:
    text = " ".join(text.split())
    match = re.match(r"(.+?[.!?])(\s|$)", text)
    return match.group(1) if match else text


def table(header: tuple[str, str], rows: list[tuple[str, str]]) -> str:
    """A compact Markdown table: padding would spend the Codex size budget."""
    rows = [tuple(c.replace("|", "\\|") for c in row) for row in rows]
    return "\n".join(f"| {a} | {b} |" for a, b in [header, ("---", "---"), *rows])


# ── Engine ──────────────────────────────────────────────────────────


class Engine:
    def __init__(self, root: Path):
        self.root = root
        cfg_path = root / CONFIG
        if not cfg_path.is_file():
            raise HarnessError(f"{CONFIG} not found under {root}")
        self.cfg = yaml.safe_load(cfg_path.read_text(encoding="utf-8"))
        self.masters = root / self.cfg["masters"]
        self.main = self.masters / self.cfg["main"]
        self.harnesses = [Harness(key=k, **v) for k, v in self.cfg["harnesses"].items()]
        self.known = {h.key for h in self.harnesses}
        only = self.cfg.get("only") or {}
        unknown = set(only) - self.known
        if unknown:
            raise HarnessError(f"{CONFIG}: `only:` names unknown harness {sorted(unknown)}")
        self.only = {k: list(v or []) for k, v in only.items()}

    def rel(self, path: Path) -> str:
        return path.relative_to(self.root).as_posix()

    # Sources ---------------------------------------------------------

    def masters_of(self, kind: str) -> list[Path]:
        return walk(self.masters / kind)

    def wanted(self, master: Path, harness: Harness) -> bool:
        """False when an `only:` glob reserves this master for another harness."""
        rel = master.relative_to(self.masters).as_posix()
        for other, globs in self.only.items():
            if other != harness.key and any(fnmatch.fnmatch(rel, g) for g in globs):
                return False
        return True

    # Rendering -------------------------------------------------------

    def render_file(self, master: Path, harness: Harness) -> bytes:
        if master.suffix != ".md":
            return master.read_bytes()
        text = master.read_bytes().decode("utf-8")
        return render_markdown(text, harness, self.known, self.rel(master)).encode("utf-8")

    def tree(self, harness: Harness) -> dict[Path, tuple[bytes, Path]]:
        """Every rendered file of one harness, except the entry file: dest -> (bytes, master)."""
        files = {}
        for kind in INDEXES:
            base = self.masters / kind
            for master in self.masters_of(kind):
                if self.wanted(master, harness):
                    dest = self.root / harness.dest(kind) / master.relative_to(base)
                    files[dest] = (self.render_file(master, harness), master)
        return files

    def index(self, kind: str, harness: Harness, tree: dict[Path, tuple[bytes, Path]]) -> str:
        """An index table built from the rendered frontmatter of this harness's files."""
        base = self.root / harness.dest(kind)
        rows = []
        if kind == "skills":
            entries = sorted(p for p in tree if p.name == "SKILL.md" and p.parent.parent == base)
        else:
            entries = sorted(p for p in tree if p.parent == base and p.suffix == ".md")
        for dest in entries:
            _, data, _ = split_frontmatter(tree[dest][0].decode("utf-8"))
            master = self.rel(tree[dest][1])
            if kind == "rules":
                paths = data.get("paths")
                if not paths or not isinstance(paths, list):
                    raise HarnessError(f"{master}: frontmatter needs a `paths:` list")
                rows.append((", ".join(f"`{p}`" for p in paths), f"`{self.rel(dest)}`"))
            elif kind == "context":
                when = data.get("read-when")
                if not when or not isinstance(when, str):
                    raise HarnessError(f"{master}: frontmatter needs a `read-when:` sentence")
                rows.append((" ".join(when.split()), f"`{self.rel(dest)}`"))
            else:
                self.check_skill(master, dest.parent.name, data)
                rows.append((f"`{harness.invoke}{data['name']}`", first_sentence(data["description"])))
        header = {"rules": ("When editing", "Read first"), "context": ("Task", "Read first"), "skills": ("Skill", "Purpose")}
        return table(header[kind], rows)

    def check_skill(self, master: str, folder: str, data: dict) -> None:
        limit = self.cfg["budgets"]["description_chars"]
        name, desc = data.get("name"), data.get("description")
        if name != folder:
            raise HarnessError(f"{master}: `name` must be {folder!r}, got {name!r}")
        if not desc or not isinstance(desc, str):
            raise HarnessError(f"{master}: `description` is missing")
        if len(desc) > limit:
            raise HarnessError(f"{master}: description is {len(desc)} chars, limit {limit}")

    def entry(self, harness: Harness, tree: dict[Path, tuple[bytes, Path]]) -> bytes:
        where = self.rel(self.main)
        text = render_markdown(self.main.read_bytes().decode("utf-8"), harness, self.known, where)
        for kind in INDEXES:
            start, end = f"<!-- index:{kind} -->", f"<!-- /index:{kind} -->"
            pattern = re.compile(re.escape(start) + r".*?" + re.escape(end), re.S)
            found = len(pattern.findall(text))
            if found > 1:
                raise HarnessError(f"{where}: more than one {start} block for {harness.key}")
            if found:
                body = self.index(kind, harness, tree)
                text = pattern.sub(lambda _: f"{start}\n\n{body}\n\n{end}", text)
        lines = text.split("\n")
        if not lines[0].startswith("# "):
            raise HarnessError(f"{where}: first line must be the H1 title")
        banner = " ".join(self.cfg["banner"].split())
        return "\n".join([f"# {harness.title}", "", f"> {banner}", *lines[1:]]).encode("utf-8")

    # Plan ------------------------------------------------------------

    def desired(self) -> dict[Path, tuple[bytes, Path]]:
        files: dict[Path, tuple[bytes, Path]] = {}
        for harness in self.harnesses:
            tree = self.tree(harness)
            files.update(tree)
            files[self.root / harness.entry] = (self.entry(harness, tree), self.main)
        return files

    def owned_dirs(self) -> list[Path]:
        return [self.root / h.dest(k) for h in self.harnesses for k in INDEXES]

    def owned_roots(self) -> list[Path]:
        """Paths that must be real files or folders, never symlinks."""
        roots = [self.root / h.entry for h in self.harnesses]
        for d in self.owned_dirs():
            roots += [d, d.parent]
        return [r for r in dict.fromkeys(roots) if r != self.root]

    def orphans(self, desired: dict) -> list[Path]:
        out = []
        for d in self.owned_dirs():
            if d.is_dir() and not d.is_symlink():
                out += [p for p in walk(d) if p not in desired]
        return out

    def changes(self) -> tuple[list[tuple[str, Path]], dict]:
        desired = self.desired()
        changes = []
        for path, (data, _) in desired.items():
            if path.is_symlink():
                changes.append(("replace symlink", path))
            elif not path.exists():
                changes.append(("create", path))
            elif path.read_bytes() != data:
                changes.append(("update", path))
        changes += [("delete", p) for p in self.orphans(desired)]
        return changes, desired

    # Checks ----------------------------------------------------------

    def check(self, desired: dict) -> Report:
        report = Report()
        for path in [self.masters, *self.masters.rglob("*")]:
            if path.is_symlink():
                report.failures.append(f"{self.rel(path)}: symlink in the masters")
        for path in self.owned_roots():
            if path.is_symlink():
                report.failures.append(f"{self.rel(path)}: is a symlink; run sync to replace it with real files")
        for harness in self.harnesses:
            size = len(desired[self.root / harness.entry][0])
            if harness.max_bytes and size > harness.max_bytes:
                report.failures.append(f"{harness.entry}: {size} bytes, {harness.name} reads only {harness.max_bytes}")
            elif harness.warn_bytes and size > harness.warn_bytes:
                report.warnings.append(f"{harness.entry}: {size} bytes, over the {harness.warn_bytes} warning budget")
        max_lines = self.cfg["budgets"]["skill_lines"]
        planned = {p.resolve() for p in desired} | {d.resolve() for p in desired for d in p.parents}
        for path, (data, _) in desired.items():
            if path.suffix != ".md":
                continue
            text = data.decode("utf-8")
            if path.name == "SKILL.md" and text.count("\n") > max_lines:
                report.warnings.append(f"{self.rel(path)}: {text.count(chr(10))} lines, budget {max_lines}")
            for target in broken_links(path, text, self.root, planned):
                report.failures.append(f"{self.rel(path)}: link to {target} does not resolve inside the repo")
        return report


# ── Helpers ─────────────────────────────────────────────────────────


def walk(top: Path) -> list[Path]:
    """Regular files under top, skipping OS and interpreter debris."""
    if not top.is_dir():
        return []
    return sorted(
        p for p in top.rglob("*")
        if (p.is_file() or p.is_symlink())
        and not any(part in IGNORED_NAMES for part in p.relative_to(top).parts)
    )


def broken_links(path: Path, text: str, root: Path, planned: set[Path]) -> list[str]:
    """Relative Markdown links outside code fences that miss or leave the repo."""
    broken, fenced = [], False
    for line in text.splitlines():
        if FENCE_RE.match(line):
            fenced = not fenced
            continue
        if fenced:
            continue
        line = re.sub(r"`[^`]*`", "", line)
        for target in LINK_RE.findall(line):
            if re.match(r"[a-z][a-z0-9+.-]*:", target, re.I) or target.startswith("#"):
                continue
            resolved = (path.parent / target.split("#", 1)[0]).resolve()
            if not resolved.is_relative_to(root.resolve()) or not (resolved.exists() or resolved in planned):
                broken.append(target)
    return broken


def remove(path: Path) -> None:
    if path.is_symlink() or path.is_file():
        path.unlink()
    elif path.is_dir():
        shutil.rmtree(path)


def prune_empty(top: Path) -> None:
    if not top.is_dir() or top.is_symlink():
        return
    for d in sorted((p for p in top.rglob("*") if p.is_dir()), key=lambda p: len(p.parts), reverse=True):
        if not any(d.iterdir()):
            d.rmdir()


# ── Commands ────────────────────────────────────────────────────────


def cmd_list(e: Engine) -> int:
    print(f"Masters: {e.rel(e.masters)}/ (entry {e.rel(e.main)})")
    for harness in e.harnesses:
        tree = e.tree(harness)
        skills = sorted(p.parent.name for p in tree if p.name == "SKILL.md")
        print(f"\n{harness.name}: {harness.entry}, {harness.rules}/, {harness.context}/, {harness.skills}/")
        print(f"  {len(tree)} files; skills: {', '.join(skills)}")
    if any(e.only.values()):
        print("\nHarness-only masters:")
        for key, globs in e.only.items():
            for g in globs:
                print(f"  {key}: {g}")
    return 0


def cmd_diff(e: Engine) -> int:
    changes, _ = e.changes()
    if not changes:
        print("Both harnesses are in sync with ai/.")
    for action, path in changes:
        print(f"  {action:<16} {e.rel(path)}")
    return 0


def cmd_sync(e: Engine) -> int:
    changes, desired = e.changes()
    report = e.check(desired)
    # A symlink where a render belongs is what sync exists to fix.
    report.failures = [f for f in report.failures if "run sync to replace" not in f]
    if report.failures:
        print_report(report, quiet=False)
        print("sync wrote nothing.")
        return 1
    for root in e.owned_roots():
        if root.is_symlink():
            root.unlink()
    changes, desired = e.changes()
    for action, path in changes:
        if action == "delete":
            remove(path)
        else:
            data, master = desired[path]
            if path.is_symlink() or path.is_dir():
                remove(path)
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(data)
            shutil.copymode(master, path)
        print(f"  {action:<16} {e.rel(path)}")
    for d in e.owned_dirs():
        prune_empty(d)
    if not changes:
        print("Both harnesses were already in sync.")
    return cmd_verify(e, quiet=False)


def cmd_verify(e: Engine, quiet: bool) -> int:
    changes, desired = e.changes()
    report = e.check(desired)
    for action, path in changes:
        hint = f"edit {e.rel(desired[path][1])} and run sync" if path in desired else "run sync"
        report.failures.append(f"{e.rel(path)}: drift ({action}); {hint}")
    print_report(report, quiet)
    if not report.failures and not quiet:
        print("verify: ok")
    return 1 if report.failures else 0


def print_report(report: Report, quiet: bool) -> None:
    for f in report.failures:
        print(f"FAIL  {f}")
    if not quiet:
        for w in report.warnings:
            print(f"warn  {w}")


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    parser.add_argument("command", choices=["list", "diff", "sync", "verify"])
    parser.add_argument("--root", type=Path, default=Path(__file__).resolve().parents[2])
    parser.add_argument("--quiet", action="store_true")
    args = parser.parse_args(argv)
    try:
        e = Engine(args.root.resolve())
        return {"list": cmd_list, "diff": cmd_diff, "sync": cmd_sync}.get(args.command, lambda x: cmd_verify(x, args.quiet))(e)
    except HarnessError as err:
        print(f"FAIL  {err}")
        return 1


if __name__ == "__main__":
    sys.exit(main())
