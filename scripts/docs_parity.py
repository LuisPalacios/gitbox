#!/usr/bin/env python3
# /// script
# requires-python = ">=3.11"
# dependencies = ["markdown-it-py==4.0.0"]
# ///
"""Check the structure of the English and Spanish Gitbox documentation."""

import sys
from argparse import ArgumentParser, Namespace
from collections import defaultdict
from dataclasses import dataclass, field
from html.parser import HTMLParser
from io import TextIOWrapper
from itertools import zip_longest
from pathlib import Path, PurePosixPath
from posixpath import normpath
from urllib.parse import unquote, urlsplit

from markdown_it import MarkdownIt
from markdown_it.token import Token


@dataclass
class Document:
    path: str
    end: int
    headings: list[tuple[str, int]] = field(default_factory=list)
    anchors: dict[str, int] = field(default_factory=dict)
    fences: list[tuple[str, int]] = field(default_factory=list)
    links: list[tuple[str, int]] = field(default_factory=list)
    images: list[tuple[str, int]] = field(default_factory=list)


class HtmlStructure(HTMLParser):
    def __init__(self, document: Document, line: int):
        super().__init__()
        self.document: Document = document
        self.line: int = line

    def handle_starttag(self, tag: str, attrs: list[tuple[str, str | None]]) -> None:  # pyright: ignore[reportImplicitOverride]
        if tag == "img":
            self.document.images.append(("image", self.line + self.getpos()[0] - 1))
        if tag == "a":
            target = dict(attrs).get("href")
            if target is not None:
                self.document.links.append((target, self.line + self.getpos()[0] - 1))


def heading_slug(children: list[Token]) -> str:
    title = "".join(token.content for token in children if token.type in {"text", "code_inline", "image"})
    return "".join(char for char in title.lower() if char.isalnum() or char in " _-").replace(" ", "-")


def read_document(root: Path, path: Path, parser: MarkdownIt) -> Document:
    source = path.read_text(encoding="utf-8")
    document = Document(path.relative_to(root).as_posix(), len(source.splitlines()) + 1)
    tokens = parser.parse(source)
    line = 1
    for index, token in enumerate(tokens):
        if token.map is not None:
            line = token.map[0] + 1
        if token.type == "heading_open":
            document.headings.append((token.tag, line))
            slug = heading_slug(tokens[index + 1].children or [])
            anchor, suffix = slug, 1
            while anchor in document.anchors:
                anchor = f"{slug}-{suffix}"
                suffix += 1
            document.anchors[anchor] = len(document.headings)
        elif token.type == "fence":
            info = token.info.split()
            document.fences.append((info[0] if info else "(untagged)", line))
        elif token.type == "html_block":
            HtmlStructure(document, line).feed(token.content)
        elif token.type == "inline":
            for child in token.children or []:
                if child.type == "link_open":
                    href = child.attrGet("href")
                    if isinstance(href, str):
                        document.links.append((href, line))
                elif child.type == "image":
                    document.images.append(("image", line))
                elif child.type == "html_inline":
                    HtmlStructure(document, line).feed(child.content)
                elif child.type in {"softbreak", "hardbreak"}:
                    line += 1
    return document


def link_key(target: str, source: Document, documents: dict[str, Document]) -> tuple[str, str, str]:
    url = urlsplit(target)
    if url.scheme or url.netloc or target.startswith("/"):
        return "url", target, ""
    path = source.path
    if url.path:
        path = normpath(str(PurePosixPath(source.path).parent / unquote(url.path)))
    destination = documents.get(path)
    canonical = path
    if destination and path.startswith("docs/es/"):
        canonical = "docs/" + path.removeprefix("docs/es/")
    if url.query:
        canonical += f"?{url.query}"
    fragment = unquote(url.fragment)
    if not fragment:
        return "local", canonical, ""
    ordinal = destination.anchors.get(fragment) if destination else None
    return "local", canonical, f"heading {ordinal}" if ordinal is not None else f"#{fragment}"


def link_locations(document: Document, documents: dict[str, Document]) -> dict[tuple[str, str, str], list[tuple[str, int]]]:
    locations: dict[tuple[str, str, str], list[tuple[str, int]]] = defaultdict(list)
    for target, line in document.links:
        locations[link_key(target, document, documents)].append((target, line))
    return locations


def compare_sequence(
    english: Document, spanish: Document, label: str,
    left: list[tuple[str, int]], right: list[tuple[str, int]],
) -> list[str]:
    differences: list[str] = []
    for first, second in zip_longest(left, right):
        en_value, en_line = first or ("missing", english.end)
        es_value, es_line = second or ("missing", spanish.end)
        if first is None or second is None or en_value != es_value:
            differences.append(
                f"{english.path}:{en_line} ↔ {spanish.path}:{es_line}: {label}: {en_value} / {es_value}"
            )
    return differences


def compare_documents(english: Document, spanish: Document, documents: dict[str, Document]) -> list[str]:
    differences = [
        *compare_sequence(english, spanish, "heading level", english.headings, spanish.headings),
        *compare_sequence(english, spanish, "fenced code language", english.fences, spanish.fences),
        *compare_sequence(english, spanish, "image count", english.images, spanish.images),
    ]
    en_links, es_links = link_locations(english, documents), link_locations(spanish, documents)
    for key in sorted(en_links.keys() | es_links.keys()):
        left, right = en_links.get(key, []), es_links.get(key, [])
        matched = min(len(left), len(right))
        differences.extend(
            f"{english.path}:{line} ↔ {spanish.path}:{spanish.end}: link target missing in ES: {target}"
            for target, line in left[matched:]
        )
        differences.extend(
            f"{english.path}:{english.end} ↔ {spanish.path}:{line}: link target missing in EN: {target}"
            for target, line in right[matched:]
        )
    return differences


def compare_docs(root: Path) -> list[str]:
    docs = root / "docs"
    if not docs.is_dir():
        return ["docs:1: documentation directory is missing"]
    exceptions = docs / ".parity-ignore"
    ignored: set[str] = set()
    if exceptions.is_file():
        ignored = {
            entry for line in exceptions.read_text(encoding="utf-8").splitlines()
            if (entry := line.partition("#")[0].strip())
        }
    parser = MarkdownIt().enable(["table", "strikethrough"])
    english = sorted(path for path in docs.glob("*.md") if path.is_file())
    documents: dict[str, Document] = {}
    for path in [*english, *sorted((docs / "es").glob("*.md"))]:
        if path.is_file():
            document = read_document(root, path, parser)
            documents[document.path] = document
    differences: list[str] = []
    for path in english:
        en, es = f"docs/{path.name}", f"docs/es/{path.name}"
        if es not in documents:
            if path.name not in ignored:
                differences.append(f"{en}:1 ↔ {es}:1: missing Spanish counterpart")
            continue
        differences.extend(compare_documents(documents[en], documents[es], documents))
    return differences


class Options(Namespace):
    root: Path = Path(__file__).resolve().parents[1]


def main() -> int:
    if isinstance(sys.stdout, TextIOWrapper):
        sys.stdout.reconfigure(encoding="utf-8")
    parser = ArgumentParser(description=__doc__)
    _ = parser.add_argument("--root", type=Path, help="repository root (default: this checkout)")
    args = Options()
    _ = parser.parse_args(namespace=args)
    try:
        differences = compare_docs(args.root)
    except (OSError, ValueError) as error:
        parser.exit(1, f"{error}\n")
    for difference in differences:
        print(difference)
    return int(bool(differences))


if __name__ == "__main__":
    raise SystemExit(main())
