#!/usr/bin/env python3
# /// script
# requires-python = ">=3.11"
# dependencies = []
# ///
"""Run installed Markdown tools against one shared, editable target list."""

from __future__ import annotations

import argparse
import shutil
import subprocess
import sys
from pathlib import Path

sys.dont_write_bytecode = True
from fix_md_extra import fix_markdown_with_context, process_file, repository_root, select_targets


def run_pipeline(targets: list[Path], root: Path, check: bool = False) -> int:
    files = select_targets(targets, root)
    if not files:
        print("No editable Markdown files selected.")
        return 0
    # Resolve every dependency before making any edits. Never install implicitly.
    binaries = {name: shutil.which(name) for name in ("markdownlint-cli2", "prettier")}
    missing = [name for name, path in binaries.items() if path is None]
    if missing:
        raise ValueError(f"Install requires approval; missing tools: {', '.join(missing)}")
    skill = root / "ai/skills/fixing-markdown"
    config = skill / ".markdownlint-cli2.jsonc"
    paths = [str(path) for path in files]
    lint = [binaries["markdownlint-cli2"], "--config", str(config)]
    pretty = [binaries["prettier"], "--config", str(skill / ".prettierrc")]
    if not check:
        # No config sets fix: nested configs must not override these CLI modes.
        result = subprocess.run([*lint, "--fix", *paths], cwd=root, check=False)
        # Exit 1 means remaining lint findings; an execution/config failure stops edits.
        if result.returncode not in (0, 1):
            return result.returncode
        for path in files:
            process_file(path)
        result = subprocess.run([*pretty, "--write", *paths], cwd=root, check=False)
        if result.returncode:
            return result.returncode
    # Every final check is read-only, including the structural helper.
    lint_result = subprocess.run([*lint, *paths], cwd=root, check=False)
    pretty_result = subprocess.run([*pretty, "--check", *paths], cwd=root, check=False)
    structural = []
    for path in files:
        content = path.read_text(encoding="utf-8")
        if fix_markdown_with_context(content) != content:
            structural.append(path)
            print(f"Needs structural formatting: {path}")
    return int(bool(lint_result.returncode or pretty_result.returncode or structural))


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("targets", nargs="+", type=Path)
    parser.add_argument("--check", action="store_true", help="Check without changing files")
    args = parser.parse_args()
    try:
        return run_pipeline(args.targets, repository_root(), args.check)
    except (OSError, ValueError) as error:
        print(f"Error: {error}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
