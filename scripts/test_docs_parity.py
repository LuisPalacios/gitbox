#!/usr/bin/env python3
# /// script
# requires-python = ">=3.11"
# dependencies = []
# ///
"""Exercise the documentation parity command with isolated documents."""

import subprocess
from pathlib import Path
from tempfile import TemporaryDirectory
from unittest import TestCase, main


CHECKER = Path(__file__).with_name("docs_parity.py")
ENGLISH = """# Example

[Second section](other.md#details-1) and [outside][external].

[external]: https://example.org/guide#start

```text
## Ignored
[Ignored](different)
```

<img src="example.png">
"""
SPANISH = """# Ejemplo

[Segunda sección](other.md#sección-1) y [fuera][external].

[external]: https://example.org/guide#start

```text
Contenido distinto del ejemplo.
```

![Ejemplo](../example.png)
"""


class DocsParityTest(TestCase):
    root: Path  # pyright: ignore[reportUninitializedInstanceVariable]

    def setUp(self) -> None:  # pyright: ignore[reportImplicitOverride]
        directory = TemporaryDirectory()
        self.addCleanup(directory.cleanup)
        self.root = Path(directory.name)
        (self.root / "docs/es").mkdir(parents=True)
        self.write("docs/guide.md", ENGLISH)
        self.write("docs/es/guide.md", SPANISH)
        self.write("docs/other.md", "# Details\n\n## Details\n")
        self.write("docs/es/other.md", "# Sección\n\n## Sección\n")
        self.write("docs/rationale.md", "# English-only rationale\n")
        self.write("docs/.parity-ignore", "# Existing exception.\nrationale.md\n")

    def write(self, path: str, content: str) -> None:
        _ = (self.root / path).write_text(content, encoding="utf-8")

    def run_checker(self, root: Path | None = None) -> subprocess.CompletedProcess[str]:
        original = {path: path.read_bytes() for path in self.root.rglob("*") if path.is_file()}
        result = subprocess.run(
            ["uv", "run", "-q", str(CHECKER), "--root", str(root or self.root)],
            capture_output=True,
            encoding="utf-8",
            check=False,
        )
        self.assertEqual(original, {path: path.read_bytes() for path in self.root.rglob("*") if path.is_file()})
        return result

    def test_matching_localized_targets_and_duplicate_anchors(self) -> None:
        result = self.run_checker()
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertEqual(result.stdout + result.stderr, "")

    def test_reports_all_structural_differences(self) -> None:
        self.write("docs/orphan.md", "# Missing counterpart")
        spanish = SPANISH.replace("# Ejemplo\n", "").replace("```text", "```bash")
        spanish = spanish.replace("![Ejemplo](../example.png)", "")
        spanish = spanish.replace("other.md#sección-1", "other.md#sección")
        spanish = spanish.replace("#start", "#end")
        self.write("docs/es/guide.md", spanish)
        result = self.run_checker()
        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
        for finding in ("heading level", "fenced code language", "image count", "missing Spanish counterpart"):
            self.assertIn(finding, result.stdout)
        for target in ("other.md#details-1", "other.md#secci%C3%B3n", "#start", "#end"):
            self.assertIn(target, result.stdout)
        self.assertIn("docs/guide.md:7 ↔ docs/es/guide.md:6: fenced code language", result.stdout)

    def test_checks_link_multiplicity_and_location(self) -> None:
        self.write("docs/es/guide.md", SPANISH.replace("y [fuera][external].", "y [fuera][external].\n\n[De nuevo][external]."))
        result = self.run_checker()
        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
        self.assertEqual(result.stdout.count("link target missing"), 1)
        self.assertIn("docs/es/guide.md:5: link target missing in EN", result.stdout)

    def test_missing_docs_directory_is_an_error(self) -> None:
        result = self.run_checker(self.root / "missing")
        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
        self.assertIn("documentation directory is missing", result.stdout)


if __name__ == "__main__":
    _ = main()
