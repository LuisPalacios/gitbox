#!/usr/bin/env python3
# /// script
# requires-python = ">=3.11"
# dependencies = [
#     "pyyaml>=6.0",  # frontmatter and harness.yaml
# ]
# ///
"""Mirror the .claude/ harness into Codex and check it for drift.

.claude/ is the only hand-edited source. This script renders AGENTS.md,
copies .claude/rules and .claude/context into .codex/, copies .claude/skills
into .agents/skills, and fills the generated index blocks of CLAUDE.md.

Usage:
    uv run .claude/skills/mirroring-harness/scripts/sync.py list
    uv run .claude/skills/mirroring-harness/scripts/sync.py diff
    uv run .claude/skills/mirroring-harness/scripts/sync.py sync
    uv run .claude/skills/mirroring-harness/scripts/sync.py verify [--quiet]

Options:
    --root PATH   repository root (default: four levels above this script)
    --quiet       print only failures (verify), for scripts/health.sh
"""

from __future__ import annotations

import argparse
import re
import shutil
import sys
from dataclasses import dataclass, field
from pathlib import Path

import yaml

CONFIG = ".claude/harness.yaml"
# Files that never belong in a mirror: OS and interpreter debris.
IGNORED_NAMES = {".DS_Store", "Thumbs.db", "__pycache__"}
INDEXES = ("rules", "context", "skills")
LINK_RE = re.compile(r"\]\(([^)\s]+)\)")
FENCE_RE = re.compile(r"^\s*(```|~~~)")


class HarnessError(Exception):
    """A problem that stops sync before it writes anything."""


@dataclass
class Report:
    failures: list[str] = field(default_factory=list)
    warnings: list[str] = field(default_factory=list)


# ── Source model ────────────────────────────────────────────────────


def split_frontmatter(text: str) -> tuple[dict, str]:
    """Returns the YAML frontmatter as a dict (empty when absent) and the body."""
    if not text.startswith("---"):
        return {}, text
    match = re.match(r"---\r?\n(.*?)\r?\n---\r?\n?", text, re.S)
    if not match:
        return {}, text
    data = yaml.safe_load(match.group(1)) or {}
    if not isinstance(data, dict):
        raise HarnessError("frontmatter is not a mapping")
    return data, text[match.end():]


def first_sentence(text: str) -> str:
    text = " ".join(text.split())
    match = re.match(r"(.+?[.!?])(\s|$)", text)
    return match.group(1) if match else text


@dataclass
class Rule:
    name: str
    paths: list[str]


@dataclass
class Context:
    name: str
    read_when: str


@dataclass
class Skill:
    name: str
    description: str


class Harness:
    def __init__(self, root: Path):
        self.root = root
        cfg_path = root / CONFIG
        if not cfg_path.is_file():
            raise HarnessError(f"{CONFIG} not found under {root}")
        self.cfg = yaml.safe_load(cfg_path.read_text(encoding="utf-8"))
        src, codex = self.cfg["source"], self.cfg["codex"]
        self.main = root / src["main_file"]
        self.rules_dir = root / src["rules_dir"]
        self.context_dir = root / src["context_dir"]
        self.skills_dir = root / src["skills_dir"]
        self.agents_file = root / codex["main_file"]
        self.codex_rules = root / codex["rules_dir"]
        self.codex_context = root / codex["context_dir"]
        self.codex_skills = root / codex["skills_dir"]

    def rel(self, path: Path) -> str:
        return path.relative_to(self.root).as_posix()

    # Sources ---------------------------------------------------------

    def rules(self) -> list[Rule]:
        out = []
        for path in sorted(self.rules_dir.glob("*.md")):
            meta, _ = split_frontmatter(read_text(path))
            paths = meta.get("paths")
            if not paths or not isinstance(paths, list):
                raise HarnessError(f"{self.rel(path)}: frontmatter needs a `paths:` list")
            out.append(Rule(path.name, [str(p) for p in paths]))
        return out

    def contexts(self) -> list[Context]:
        out = []
        for path in sorted(self.context_dir.glob("*.md")):
            meta, _ = split_frontmatter(read_text(path))
            when = meta.get("read-when")
            if not when or not isinstance(when, str):
                raise HarnessError(f"{self.rel(path)}: frontmatter needs a `read-when:` sentence")
            out.append(Context(path.name, " ".join(when.split())))
        return out

    def skills(self) -> list[Skill]:
        limit = self.cfg["budgets"]["description_chars"]
        out = []
        for skill_md in sorted(self.skills_dir.glob("*/SKILL.md")):
            meta, _ = split_frontmatter(read_text(skill_md))
            folder = skill_md.parent.name
            name, desc = meta.get("name"), meta.get("description")
            if name != folder:
                raise HarnessError(f"{self.rel(skill_md)}: `name` must be {folder!r}, got {name!r}")
            if not desc or not isinstance(desc, str):
                raise HarnessError(f"{self.rel(skill_md)}: `description` is missing")
            if len(desc) > limit:
                raise HarnessError(f"{self.rel(skill_md)}: description is {len(desc)} chars, limit {limit}")
            out.append(Skill(name, desc))
        return out

    # Rendering -------------------------------------------------------

    def render_index(self, kind: str, codex: bool) -> str:
        if kind == "rules":
            base = self.codex_rules if codex else self.rules_dir
            rows = [(", ".join(f"`{p}`" for p in r.paths), f"`{self.rel(base / r.name)}`") for r in self.rules()]
            if codex:
                lead = "Codex does not load rules automatically. **Before editing a file that matches a pattern, read its rule.**"
                return lead + "\n\n" + table(("When editing", "Read first"), rows)
            lead = "Claude Code loads each rule automatically when it reads a matching file."
            return lead + "\n\n" + table(("Files", "Rule"), rows)
        if kind == "context":
            base = self.codex_context if codex else self.context_dir
            rows = [(c.read_when, f"`{self.rel(base / c.name)}`") for c in self.contexts()]
            return table(("Task", "Read first"), rows)
        prefix = "$" if codex else "/"
        rows = [(f"`{prefix}{s.name}`", first_sentence(s.description)) for s in self.skills()]
        return table(("Skill", "Purpose"), rows)

    def fill_indexes(self, text: str, codex: bool) -> str:
        for kind in INDEXES:
            start, end = f"<!-- index:{kind} -->", f"<!-- /index:{kind} -->"
            pattern = re.compile(re.escape(start) + r".*?" + re.escape(end), re.S)
            if len(pattern.findall(text)) != 1:
                raise HarnessError(f"{self.rel(self.main)}: needs exactly one {start} … {end} block")
            body = self.render_index(kind, codex)
            text = pattern.sub(lambda _: f"{start}\n\n{body}\n\n{end}", text)
        return text

    def render_claude(self) -> str:
        return self.fill_indexes(read_text(self.main), codex=False)

    def render_agents(self) -> str:
        text = self.fill_indexes(read_text(self.main), codex=True)
        lines = text.split("\n")
        if not lines or not lines[0].startswith("# "):
            raise HarnessError(f"{self.rel(self.main)}: first line must be the H1 title")
        banner = " ".join(self.cfg["banner"].split())
        lines[0] = f"# {self.cfg['codex']['entry_title']}"
        return "\n".join([lines[0], "", f"> {banner}", *lines[1:]])

    # Plan ------------------------------------------------------------

    def desired(self) -> dict[Path, bytes]:
        """Every generated file and its exact content."""
        files: dict[Path, bytes] = {
            self.main: self.render_claude().encode("utf-8"),
            self.agents_file: self.render_agents().encode("utf-8"),
        }
        for src_dir, dst_dir in (
            (self.rules_dir, self.codex_rules),
            (self.context_dir, self.codex_context),
            (self.skills_dir, self.codex_skills),
        ):
            for path in walk(src_dir):
                files[dst_dir / path.relative_to(src_dir)] = path.read_bytes()
        return files

    def mirror_dirs(self) -> list[Path]:
        return [self.codex_rules, self.codex_context, self.codex_skills]

    def orphans(self, desired: dict[Path, bytes]) -> list[Path]:
        out = []
        for d in self.mirror_dirs():
            if d.is_dir() and not d.is_symlink():
                out += [p for p in walk(d) if p not in desired]
        return out

    def changes(self) -> tuple[list[tuple[str, Path]], dict[Path, bytes]]:
        desired = self.desired()
        changes = []
        for path, data in desired.items():
            if path.is_symlink():
                changes.append(("replace symlink", path))
            elif not path.exists():
                changes.append(("create", path))
            elif path.read_bytes() != data:
                changes.append(("update", path))
        changes += [("delete", p) for p in self.orphans(desired)]
        return changes, desired

    # Checks ----------------------------------------------------------

    def check(self, desired: dict[Path, bytes]) -> Report:
        report = Report()
        sources = [self.main, self.rules_dir, self.context_dir, self.skills_dir]
        for top in sources:
            for path in [top, *(top.rglob("*") if top.is_dir() else [])]:
                if path.is_symlink():
                    report.failures.append(f"{self.rel(path)}: symlink in the source harness")
        for path in [self.agents_file, *self.mirror_dirs(), *(d.parent for d in self.mirror_dirs())]:
            if path.is_symlink():
                report.failures.append(f"{self.rel(path)}: is a symlink; run sync to replace it with real files")

        codex = self.cfg["codex"]
        size = len(desired[self.agents_file])
        if size > codex["max_bytes"]:
            report.failures.append(f"{self.rel(self.agents_file)}: {size} bytes, Codex reads only {codex['max_bytes']}")
        elif size > codex["warn_bytes"]:
            report.warnings.append(f"{self.rel(self.agents_file)}: {size} bytes, over the {codex['warn_bytes']} warning budget")

        max_lines = self.cfg["budgets"]["skill_lines"]
        for skill_md in sorted(self.skills_dir.glob("*/SKILL.md")):
            n = read_text(skill_md).count("\n")
            if n > max_lines:
                report.warnings.append(f"{self.rel(skill_md)}: {n} lines, budget {max_lines}")

        # A link may point at a mirror this sync is about to write.
        planned = {p.resolve() for p in desired} | {d.resolve() for p in desired for d in p.parents}
        for path, data in desired.items():
            if path.suffix == ".md":
                for target in broken_links(path, data.decode("utf-8"), self.root, planned):
                    report.failures.append(f"{self.rel(path)}: link to {target} does not resolve inside the repo")
        return report


# ── Helpers ─────────────────────────────────────────────────────────


def read_text(path: Path) -> str:
    return path.read_bytes().decode("utf-8")


def walk(top: Path) -> list[Path]:
    """Regular files under top, skipping OS and interpreter debris."""
    if not top.is_dir():
        return []
    return sorted(
        p for p in top.rglob("*")
        if (p.is_file() or p.is_symlink())
        and not any(part in IGNORED_NAMES for part in p.relative_to(top).parts)
    )


def table(header: tuple[str, str], rows: list[tuple[str, str]]) -> str:
    """A compact Markdown table: padding would spend the Codex size budget."""
    rows = [tuple(c.replace("|", "\\|") for c in row) for row in rows]
    out = [header, ("---", "---"), *rows]
    return "\n".join(f"| {a} | {b} |" for a, b in out)


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


def write(path: Path, data: bytes, source: Path | None) -> None:
    if path.is_symlink() or (path.exists() and path.is_dir()):
        remove(path)
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_bytes(data)
    if source is not None:
        shutil.copymode(source, path)


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


def cmd_list(h: Harness) -> int:
    print(f"Entry file: {h.rel(h.main)} -> {h.rel(h.agents_file)}")
    print("\nRules:")
    for r in h.rules():
        print(f"  {r.name}: {', '.join(r.paths)}")
    print("\nContext:")
    for c in h.contexts():
        print(f"  {c.name}: {c.read_when}")
    print("\nSkills:")
    for s in h.skills():
        print(f"  {s.name}: {first_sentence(s.description)}")
    return 0


def cmd_diff(h: Harness) -> int:
    changes, _ = h.changes()
    if not changes:
        print("Mirrors are in sync.")
    for action, path in changes:
        print(f"  {action:<16} {h.rel(path)}")
    return 0


def sources_for(h: Harness) -> dict[Path, Path]:
    """Maps each copied mirror file back to its source, for file modes."""
    out = {}
    for src_dir, dst_dir in ((h.rules_dir, h.codex_rules), (h.context_dir, h.codex_context), (h.skills_dir, h.codex_skills)):
        for path in walk(src_dir):
            out[dst_dir / path.relative_to(src_dir)] = path
    return out


def cmd_sync(h: Harness) -> int:
    changes, desired = h.changes()
    report = h.check(desired)
    # A symlink where a mirror belongs is what sync exists to fix.
    report.failures = [f for f in report.failures if "run sync to replace" not in f]
    if report.failures:
        print_report(report, quiet=False)
        print("sync wrote nothing.")
        return 1
    # Replace symlinked mirror roots before writing through them.
    for d in [h.agents_file, *h.mirror_dirs(), *(d.parent for d in h.mirror_dirs())]:
        if d.is_symlink():
            d.unlink()
    changes, desired = h.changes()
    sources = sources_for(h)
    for action, path in changes:
        if action == "delete":
            remove(path)
        else:
            write(path, desired[path], sources.get(path))
        print(f"  {action:<16} {h.rel(path)}")
    for d in h.mirror_dirs():
        prune_empty(d)
    if not changes:
        print("Mirrors were already in sync.")
    return cmd_verify(h, quiet=False)


def cmd_verify(h: Harness, quiet: bool) -> int:
    changes, desired = h.changes()
    report = h.check(desired)
    for action, path in changes:
        report.failures.append(f"{h.rel(path)}: drift ({action}); run sync")
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
    parser.add_argument("--root", type=Path, default=Path(__file__).resolve().parents[4])
    parser.add_argument("--quiet", action="store_true")
    args = parser.parse_args(argv)
    try:
        h = Harness(args.root.resolve())
        if args.command == "list":
            return cmd_list(h)
        if args.command == "diff":
            return cmd_diff(h)
        if args.command == "sync":
            return cmd_sync(h)
        return cmd_verify(h, args.quiet)
    except HarnessError as err:
        print(f"FAIL  {err}")
        return 1


if __name__ == "__main__":
    sys.exit(main())
