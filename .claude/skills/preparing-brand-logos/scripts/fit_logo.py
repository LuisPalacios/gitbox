#!/usr/bin/env python3
# /// script
# requires-python = ">=3.11"
# dependencies = [
#     "svgelements>=1.9",  # artwork bounding box (shapes, transforms, <use>)
#     "pillow>=10",        # PNG fallback: alpha-trim and size
# ]
# ///
"""Fit a downloaded logo into gitbox's colour or single-colour brand template.

Wraps an SVG (or, as a fallback, a PNG) in the 150x150 Boxy-SVG frame used by
assets/svg/brand/logo-<name>.svg: hidden 120x120 guide box at 15,15 and the
artwork scaled to touch it, centred. The source markup is copied verbatim
inside a transform group, so nothing is redrawn. --mono writes the -dark
template instead and repaints every colour as currentColor (good for Simple
Icons marks, or for a colour logo whose silhouette alone is recognizable).

Usage:
    uv run fit_logo.py <source.svg|png> <out.svg> [--source-url URL]
                       [--bbox content|viewbox|X,Y,W,H] [--max-png 128] [--mono]

--bbox content (default) fits the painted artwork; viewbox fits the source
viewBox, padding included; X,Y,W,H is an explicit box in source user units.
"""

import argparse
import base64
import io
import re
import sys
from pathlib import Path

FRAME = 150
BOX = 120
MARGIN = 15

HEADER = (
    '<?xml version="1.0" encoding="utf-8"?>\n'
    '<svg height="100%" style="fill-rule:evenodd;clip-rule:evenodd;stroke-linecap:round;'
    'stroke-linejoin:round;" width="100%" version="1.1" viewBox="0 0 150 150" '
    'xmlns="http://www.w3.org/2000/svg" xmlns:bx="https://boxy-svg.com"{ns}>\n'
    '  <defs>\n'
    '    <bx:grid x="0" y="0" width="15" height="15"/>\n'
    '  </defs>\n'
    '  <rect width="120" height="120" style="fill: none; stroke-width: 0.2px; '
    'stroke: rgb(84, 84, 84); pointer-events: none; visibility: hidden;" x="15" y="15"/>\n'
)

MONO_HEADER = (
    '<svg viewBox="0 0 150 150" xmlns="http://www.w3.org/2000/svg"{ns} fill="currentColor" '
    'fill-rule="evenodd" clip-rule="evenodd">\n'
    '  <rect x="15" y="15" width="120" height="120" style="fill: none; stroke-width: 0.2px; '
    'stroke: rgb(84, 84, 84); pointer-events: none; visibility: hidden;"/>\n'
)

# Paint values that --mono turns into currentColor (none, url(#…) and
# currentColor itself are left alone).
PAINT = re.compile(r'((?:fill|stroke|stop-color|color)\s*[:=]\s*["\']?)(#[0-9a-fA-F]{3,8}|rgba?\([^)]*\)|[a-zA-Z]+)')

# The template root sets evenodd fill and round caps for hand-drawn logos.
# Imported art expects SVG defaults, so the wrapper group resets them.
RESET_STYLE = 'fill-rule:nonzero;clip-rule:nonzero;stroke-linecap:butt;stroke-linejoin:miter'

# Root attributes that describe the old canvas, not the artwork.
CANVAS_ATTRS = {
    'version', 'width', 'height', 'viewBox', 'x', 'y', 'preserveAspectRatio', 'id',
    'enable-background', 'xml:space', 'baseProfile', 'overflow', 'class',
}


def fmt(v: float) -> str:
    return f'{v:.4f}'.rstrip('0').rstrip('.')


def fit_matrix(x: float, y: float, w: float, h: float) -> str:
    s = BOX / max(w, h)
    tx = MARGIN + (BOX - w * s) / 2 - x * s
    ty = MARGIN + (BOX - h * s) / 2 - y * s
    return f'matrix({fmt(s)}, 0, 0, {fmt(s)}, {fmt(tx)}, {fmt(ty)})'


def split_root(text: str) -> tuple[str, str]:
    """Return (root opening-tag attribute string, inner markup)."""
    text = re.sub(r'<\?xml[^>]*\?>|<!DOCTYPE[^>[]*(\[[^]]*\])?>', '', text)
    # A <title> would show as a tooltip wherever the logo is inlined.
    text = re.sub(r'<title\b[^>]*>.*?</title>', '', text, flags=re.S)
    m = re.search(r'<svg\b([^>]*?)/?>', text, re.S)
    if not m:
        sys.exit('error: no <svg> root element found')
    end = text.rfind('</svg>')
    inner = text[m.end():end if end > m.end() else len(text)]
    return m.group(1), inner.strip('\n')


def parse_attrs(attr_text: str) -> dict[str, str]:
    return {k: v for k, _, v in re.findall(r'([\w:.-]+)\s*=\s*(["\'])(.*?)\2', attr_text, re.S)}


def viewbox_of(attrs: dict[str, str]) -> tuple[float, float, float, float]:
    if 'viewBox' in attrs:
        x, y, w, h = (float(v) for v in re.split(r'[\s,]+', attrs['viewBox'].strip()))
        return x, y, w, h
    num = lambda v: float(re.match(r'[\d.]+', v).group())  # noqa: E731
    try:
        return 0.0, 0.0, num(attrs['width']), num(attrs['height'])
    except (KeyError, AttributeError):
        sys.exit('error: source has neither viewBox nor numeric width/height; pass --bbox X,Y,W,H')


def content_bbox(path: Path, vb: tuple[float, float, float, float]) -> tuple[float, float, float, float] | None:
    """Painted-area bbox in source user units, or None when it can't be computed."""
    from svgelements import SVG, Group, Matrix, Shape, Use

    # Parse with a viewport equal to the viewBox so bbox units are user units.
    doc = SVG.parse(str(path), width=vb[2], height=vb[3], reify=True)
    root_m = Matrix(doc.viewbox.transform(doc)) if doc.viewbox is not None else Matrix()
    inv = ~root_m
    boxes = []

    # Walk the render tree only: clip paths, masks and patterns aren't Groups
    # (or <use> instances), so their shapes (often huge clip rectangles) never count as artwork.
    def walk(node) -> None:
        for el in node:
            if isinstance(el, (Group, Use)):
                walk(el)
            elif isinstance(el, Shape):
                if str(el.values.get('visibility', '')) == 'hidden':
                    continue
                try:
                    b = el.bbox(with_stroke=True)
                except Exception:  # noqa: BLE001 - malformed shape; skip it
                    continue
                if b is None:
                    continue
                x0, y0, x1, y1 = b
                p0 = inv.point_in_matrix_space((x0, y0))
                p1 = inv.point_in_matrix_space((x1, y1))
                boxes.append((min(p0[0], p1[0]), min(p0[1], p1[1]), max(p0[0], p1[0]), max(p0[1], p1[1])))

    walk(doc)
    if not boxes:
        return None
    # The root viewport clips everything outside the viewBox (this also
    # covers shapes trimmed by a canvas-sized clip-path), so clamp to it.
    x0 = max(min(b[0] for b in boxes), vb[0])
    y0 = max(min(b[1] for b in boxes), vb[1])
    x1 = min(max(b[2] for b in boxes), vb[0] + vb[2])
    y1 = min(max(b[3] for b in boxes), vb[1] + vb[3])
    if x1 - x0 <= 0 or y1 - y0 <= 0:
        return None
    return x0, y0, x1 - x0, y1 - y0


def to_mono(markup: str) -> str:
    keep = {'none', 'transparent', 'currentcolor', 'inherit', 'url'}
    return PAINT.sub(lambda m: m.group(0) if m.group(2).lower() in keep else m.group(1) + 'currentColor', markup)


def wrap_svg(src: Path, bbox_mode: str, mono: bool) -> tuple[str, str, str]:
    """Return (extra xmlns declarations, wrapper group, note)."""
    root_attr_text, inner = split_root(src.read_text(encoding='utf-8'))
    attrs = parse_attrs(root_attr_text)
    vb = viewbox_of(attrs)

    if bbox_mode == 'viewbox':
        box, how = vb, 'source viewBox'
    elif bbox_mode == 'content':
        # Text extents depend on installed fonts and aren't measured, so a
        # source with <text> keeps its own viewBox.
        box = None if re.search(r'<text\b', inner) else content_bbox(src, vb)
        how = 'painted artwork'
        if box is None:
            box, how = vb, 'source viewBox (content bbox unavailable)'
    else:
        box = tuple(float(v) for v in bbox_mode.split(','))
        how = 'explicit box'
        if len(box) != 4:
            sys.exit('error: --bbox X,Y,W,H needs four numbers')

    ns = ''.join(
        f' {k}="{v}"' for k, v in attrs.items()
        if k.startswith('xmlns:') and k not in ('xmlns:bx',)
    )
    if 'xlink:' in inner and 'xmlns:xlink' not in attrs:
        ns += ' xmlns:xlink="http://www.w3.org/1999/xlink"'

    keep = {k: v for k, v in attrs.items()
            if k not in CANVAS_ATTRS and not k.startswith('xmlns') and k != 'xmlns'}
    style = RESET_STYLE + (';' + keep.pop('style') if 'style' in keep else '')
    extra = ''.join(f' {k}="{v}"' for k, v in keep.items())

    if mono:
        inner, style, extra = to_mono(inner), to_mono(style), to_mono(extra)
    note = f'fitted to the {how} {", ".join(fmt(v) for v in box)}'
    group = (f'  <g transform="{fit_matrix(*box)}" style="{style}"{extra}>\n'
             f'{inner}\n  </g>\n')
    return ns, group, note


def wrap_png(src: Path, max_px: int) -> tuple[str, str, str]:
    from PIL import Image

    img = Image.open(src).convert('RGBA')
    crop = img.getchannel('A').getbbox() or (0, 0, img.width, img.height)
    img = img.crop(crop)
    # Logos render at 16-24 CSS px, so 128 px covers 3x screens with room to spare.
    if max(img.size) > max_px:
        img.thumbnail((max_px, max_px), Image.LANCZOS)
    buf = io.BytesIO()
    img.save(buf, format='PNG', optimize=True)
    data = base64.b64encode(buf.getvalue()).decode('ascii')
    w, h = img.size
    group = (f'  <g transform="{fit_matrix(0, 0, w, h)}">\n'
             f'    <image width="{w}" height="{h}" href="data:image/png;base64,{data}"/>\n'
             '  </g>\n')
    return '', group, f'embedded PNG {w}x{h} (alpha-trimmed); redraw as vectors before shipping'


def main() -> None:
    ap = argparse.ArgumentParser(description=__doc__.split('\n\n')[0])
    ap.add_argument('source', type=Path)
    ap.add_argument('out', type=Path)
    ap.add_argument('--source-url', default='')
    ap.add_argument('--bbox', default='content')
    ap.add_argument('--max-png', type=int, default=128, help='downscale an embedded PNG to this size')
    ap.add_argument('--mono', action='store_true', help='write the single-colour -dark template')
    args = ap.parse_args()

    if not args.source.is_file():
        sys.exit(f'error: {args.source} not found')
    if args.source.suffix.lower() == '.png':
        if args.mono:
            sys.exit('error: --mono needs a vector source; draw the -dark file by hand instead')
        ns, group, note = wrap_png(args.source, args.max_png)
    else:
        ns, group, note = wrap_svg(args.source, args.bbox, args.mono)

    comment = f'  <!-- Source: {args.source_url or args.source.name}; {note}. -->\n'
    out = (MONO_HEADER if args.mono else HEADER).format(ns=ns) + comment + group + '</svg>\n'
    args.out.parent.mkdir(parents=True, exist_ok=True)
    args.out.write_text(out, encoding='utf-8')
    print(f'{args.out}: {note}')


if __name__ == '__main__':
    main()
