#!/usr/bin/env python3
# /// script
# requires-python = ">=3.11"
# dependencies = []
# ///
"""Prepare minimal roadmap checkbox edits; never contact GitHub."""

from __future__ import annotations

import argparse
import hashlib
import json
import re
import sys
from pathlib import Path


def complete(body: str, issues: list[int], repository: str) -> str:
    """Mark unique leading issue references, preserving all other bytes as text."""
    if not issues or any(n <= 0 for n in issues):
        raise ValueError("Provide positive issue numbers")
    if not re.fullmatch(r"[\w.-]+/[\w.-]+", repository):
        raise ValueError("Repository must be owner/name")
    wanted = set(issues)
    matches: dict[int, list[int]] = {n: [] for n in wanted}
    lines = body.splitlines(keepends=True)
    fence: str | None = None
    marker = re.compile(r"^ {0,3}(`{3,}|~{3,})")
    task = re.compile(r"^\s*[-*+] \[([ xX])\] +(.+)")
    reference = re.compile(
        r"^(?:#(?P<short>\d+)(?![\w/])|"
        + re.escape(f"https://github.com/{repository}/issues/")
        + r"(?P<url>\d+)(?![\w/])|"
        + r"\[#(?P<label>[0-9]+)\]\("
        + re.escape(f"https://github.com/{repository}/issues/")
        + r"(?P<link>\d+)\))"
    )
    for index, line in enumerate(lines):
        boundary = marker.match(line)
        if boundary:
            token = boundary.group(1)
            if fence is None:
                fence = token
            elif token[0] == fence[0] and len(token) >= len(fence) and not line[boundary.end():].strip():
                fence = None
            continue
        if fence is not None:
            continue
        checkbox = task.match(line)
        if checkbox is None:
            continue
        ref = reference.match(checkbox.group(2))
        if ref is None:
            continue
        number = int(ref.group("short") or ref.group("url") or ref.group("link"))
        if ref.group("label") is not None and int(ref.group("label")) != number:
            if number in wanted or int(ref.group("label")) in wanted:
                raise ValueError("Issue link label and target disagree")
            continue
        if number in matches:
            matches[number].append(index)
    for number, indexes in matches.items():
        if len(indexes) != 1:
            raise ValueError(f"Issue #{number} needs exactly one leading checkbox reference; found {len(indexes)}")
    for indexes in matches.values():
        index = indexes[0]
        lines[index] = re.sub(r"\[[ xX]\]", "[x]", lines[index], count=1)
    return "".join(lines)


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--body", required=True, type=Path)
    parser.add_argument("--repository", required=True)
    parser.add_argument("--issues", required=True, nargs="+", type=int)
    args = parser.parse_args()
    try:
        raw = args.body.read_bytes()
        body = complete(raw.decode("utf-8"), args.issues, args.repository)
    except (OSError, UnicodeError, ValueError) as exc:
        print(str(exc), file=sys.stderr)
        return 1
    print(json.dumps({"source_sha256": hashlib.sha256(raw).hexdigest(), "body": body}, ensure_ascii=False))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
