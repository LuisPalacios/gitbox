#!/usr/bin/env python3
# /// script
# requires-python = ">=3.11"
# dependencies = []
# ///
"""Fix Markdown structure in editable repository files.

Resolve generated harness targets to their masters before selecting files.
Use --list for a JSON preview or --check for read-only structural validation.
"""

import argparse
import json
import os
import re
import sys
from pathlib import Path

# These are generated destinations, not additional sources of guidance.
RENDERED = {
    "AGENTS.md": "ai/main.md",
    ".claude/CLAUDE.md": "ai/main.md",
    ".claude/rules": "ai/rules",
    ".claude/context": "ai/context",
    ".claude/skills": "ai/skills",
    ".codex/rules": "ai/rules",
    ".codex/context": "ai/context",
    ".agents/skills": "ai/skills",
}
EXCLUDED_DIRS = {
    ".git", ".agent", ".agents", ".claude", ".codex", ".continuity",
    "node_modules", ".venv", "venv", "__pycache__", "build", "dist", "tmp",
    "themes", "public",
}


def repository_root() -> Path:
    """Find the same repository from a master or either rendered script."""
    return next(p for p in Path(__file__).resolve().parents if (p / "ai/harness.yaml").is_file())


def is_safe(path: Path, root: Path) -> bool:
    """Reject symlinks, generated outputs, dependencies and private scratch files."""
    try:
        relative = path.relative_to(root)
    except ValueError:
        return False
    if any(part in EXCLUDED_DIRS for part in relative.parts):
        return False
    if relative == Path("AGENTS.md") or path.name.startswith("CONTINUITY"):
        return False
    return not any(p.is_symlink() for p in [path, *path.parents] if p != root and p.is_relative_to(root))


def select_targets(targets: list[Path], root: Path) -> list[Path]:
    """Resolve explicit generated targets to masters; never traverse rendered trees."""
    root = root.resolve()
    selected = set()
    for target in targets:
        target = Path(os.path.abspath(target))
        try:
            relative = target.relative_to(root)
        except ValueError:
            raise ValueError(f"Target is outside the repository: {target}") from None
        # Reject a symlink before mapping a rendered path to its master.
        if any(p.is_symlink() for p in [target, *target.parents] if p != root and p.is_relative_to(root)):
            raise ValueError(f"Symlink targets are not supported: {target}")
        for rendered, master in RENDERED.items():
            prefix = Path(rendered)
            if relative == prefix or relative.is_relative_to(prefix):
                target = root / master / relative.relative_to(prefix)
                break
        if not is_safe(target, root):
            raise ValueError(f"Target is excluded from formatting: {target}")
        if target.is_file():
            if target.suffix != ".md":
                raise ValueError(f"Expected a Markdown file: {target}")
            selected.add(target)
        elif target.is_dir():
            for current, dirs, files in os.walk(target, followlinks=False):
                directory = Path(current)
                dirs[:] = sorted(d for d in dirs if is_safe(directory / d, root))
                selected.update(directory / name for name in files
                                if name.endswith(".md") and is_safe(directory / name, root))
        else:
            raise ValueError(f"Target or its master does not exist: {target}")
    return sorted(selected)


def fix_markdown_with_context(content: str) -> str:
    """Add missing fence languages and demote duplicate H1s outside code blocks."""
    lines = content.split('\n')
    result = []
    inside_code_block = False
    opening_fence_char = ''
    opening_fence_len = 0
    found_h1 = False

    for line in lines:
        # Detect an opening or closing fence.
        fence_match = re.match(r'^(`{3,}|~{3,})(\s*)(\S*)(.*)$', line)

        if fence_match:
            fence_chars = fence_match.group(1)
            lang = fence_match.group(3)

            if not inside_code_block:
                # Opening fence.
                inside_code_block = True
                opening_fence_char = fence_chars[0]
                opening_fence_len = len(fence_chars)
                if not lang:
                    # MD040: add a default language to opening fences only.
                    result.append(f'{fence_chars}text')
                else:
                    result.append(line)
            else:
                # Per CommonMark: closing fence must have no info string,
                # same fence character, and length >= opening fence
                if (not lang
                        and fence_chars[0] == opening_fence_char
                        and len(fence_chars) >= opening_fence_len):
                    inside_code_block = False
                result.append(line)
        elif not inside_code_block and line.startswith('# ') and not line.startswith('## '):
            # MD025: H1 outside a code block.
            if found_h1:
                # Demote subsequent H1 headings.
                result.append('#' + line)
            else:
                # Keep the first H1 heading.
                found_h1 = True
                result.append(line)
        else:
            # Keep other lines unchanged.
            result.append(line)

    return '\n'.join(result)


def process_file(filepath: Path) -> bool:
    """Apply structural corrections to a selected editable file; report changes."""
    original = filepath.read_text(encoding='utf-8')

    # Preserve fenced examples while correcting structure.
    content = fix_markdown_with_context(original)

    # Leave unchanged files untouched.
    if content != original:
        filepath.write_text(content, encoding='utf-8')
        return True

    return False


def main() -> int:
    parser = argparse.ArgumentParser(description="Fix Markdown structure in editable repository files.")
    parser.add_argument("targets", nargs="+", type=Path)
    parser.add_argument("--list", action="store_true", help="Print the selected master paths as JSON without writing")
    parser.add_argument("--check", action="store_true", help="Report structural changes without writing")
    args = parser.parse_args()
    try:
        files = select_targets(args.targets, repository_root())
        if args.list:
            print(json.dumps([str(p) for p in files]))
            return 0
        changed = 0
        for path in files:
            if args.check:
                original = path.read_text(encoding="utf-8")
                needs_change = fix_markdown_with_context(original) != original
            else:
                needs_change = process_file(path)
            if needs_change:
                changed += 1
                print(f"{'Needs formatting' if args.check else 'Fixed'}: {path}")
        return int(args.check and changed > 0)
    except (OSError, ValueError) as error:
        print(f"Error: {error}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
