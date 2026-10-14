#!/usr/bin/env python3
# /// script
# requires-python = ">=3.11"
# dependencies = []
# ///
"""Write <folder>/preview.html: every logo-*.svg in the folder on light and
dark backgrounds, at the app's real sizes (16, 24) and at 150px with the
canvas edge outlined (the app clips anything past it).

Usage:
    uv run preview_logos.py <folder>
"""

import html
import sys
from pathlib import Path

PAGE = """<!doctype html>
<meta charset="utf-8">
<title>Logo preview</title>
<style>
  body {{ font: 13px -apple-system, Segoe UI, sans-serif; margin: 16px; background: #888; }}
  table {{ border-collapse: collapse; }}
  td, th {{ padding: 8px 12px; text-align: center; vertical-align: middle; }}
  th {{ text-align: left; font-weight: 600; }}
  .light {{ background: #ffffff; color: #1f2328; }}
  .dark {{ background: #1e1e1e; color: #d4d4d4; }}
  .big {{ display: block; width: 150px; height: 150px; outline: 1px dashed #e5534b; }}
</style>
<table>
<tr><th>file</th><th class="light">16</th><th class="light">24</th><th class="light">150 + edge</th>
<th class="dark">16</th><th class="dark">24</th><th class="dark">150 + edge</th></tr>
{rows}
</table>
"""

ROW = """<tr><th>{name}</th>{cells}</tr>"""


def cells(src: str) -> str:
    out = []
    for theme in ('light', 'dark'):
        out.append(f'<td class="{theme}"><img src="{src}" width="16" height="16"></td>')
        out.append(f'<td class="{theme}"><img src="{src}" width="24" height="24"></td>')
        out.append(f'<td class="{theme}"><img class="big" src="{src}"></td>')
    return ''.join(out)


def main() -> None:
    if len(sys.argv) != 2:
        sys.exit(__doc__)
    folder = Path(sys.argv[1])
    files = sorted(folder.glob('logo-*.svg'))
    if not files:
        sys.exit(f'error: no logo-*.svg in {folder}')
    rows = '\n'.join(ROW.format(name=html.escape(f.name), cells=cells(html.escape(f.name))) for f in files)
    out = folder / 'preview.html'
    out.write_text(PAGE.format(rows=rows), encoding='utf-8')
    print(f'{out}: {len(files)} logos')


if __name__ == '__main__':
    main()
