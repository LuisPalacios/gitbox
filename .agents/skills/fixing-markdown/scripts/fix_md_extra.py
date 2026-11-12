#!/usr/bin/env python3
# /// script
# requires-python = ">=3.11"
# dependencies = []
# ///
"""
fix_md_extra.py — Correcciones de markdown que markdownlint no puede auto-fixear.

Arregla:
- MD040: Code blocks sin lenguaje especificado → añade 'text'
- MD025: Múltiples encabezados H1 → degrada los duplicados a H2

Uso:
    python fix_md_extra.py <archivo.md>
    python fix_md_extra.py <carpeta>  # procesa todos los .md recursivamente
"""

import re
import sys
from pathlib import Path


def fix_markdown_with_context(content: str) -> str:
    """
    Aplica correcciones MD040 y MD025 respetando el contexto de code blocks.

    - MD040: Añade 'text' a code blocks sin lenguaje (solo aperturas, no cierres)
    - MD025: Degrada H1 duplicados a H2 (solo fuera de code blocks)
    """
    lines = content.split('\n')
    result = []
    inside_code_block = False
    opening_fence_char = ''
    opening_fence_len = 0
    found_h1 = False

    for line in lines:
        # Detectar líneas de fence (``` con posible lenguaje)
        fence_match = re.match(r'^(`{3,}|~{3,})(\s*)(\S*)(.*)$', line)

        if fence_match:
            fence_chars = fence_match.group(1)
            lang = fence_match.group(3)

            if not inside_code_block:
                # Apertura de code block
                inside_code_block = True
                opening_fence_char = fence_chars[0]
                opening_fence_len = len(fence_chars)
                if not lang:
                    # MD040: No tiene lenguaje, añadir 'text'
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
            # MD025: H1 fuera de code block
            if found_h1:
                # Ya encontramos un H1 antes, degradar este a H2
                result.append('#' + line)
            else:
                # Primer H1, lo mantenemos
                found_h1 = True
                result.append(line)
        else:
            # Línea normal, no modificar
            result.append(line)

    return '\n'.join(result)


def process_file(filepath: Path) -> bool:
    """
    Procesa un archivo markdown aplicando todas las correcciones.

    Retorna True si el archivo fue modificado.
    """
    original = filepath.read_text(encoding='utf-8')

    # Aplicar correcciones con awareness de contexto
    content = fix_markdown_with_context(original)

    # Solo escribir si hubo cambios
    if content != original:
        filepath.write_text(content, encoding='utf-8')
        return True

    return False


def main():
    if len(sys.argv) < 2:
        print(__doc__)
        sys.exit(1)

    target = Path(sys.argv[1])

    # Excluir .agent/ siempre (convención del skill)
    def should_skip(path: Path) -> bool:
        return '.agent' in path.parts

    if target.is_file():
        # Archivo único
        if should_skip(target):
            print(f"Saltando {target} (directorio .agent/)")
            sys.exit(0)

        if process_file(target):
            print(f"Corregido: {target}")
        else:
            print(f"Sin cambios: {target}")

    elif target.is_dir():
        # Carpeta: procesar todos los .md recursivamente
        modified = 0
        skipped = 0

        for md_file in target.rglob('*.md'):
            if should_skip(md_file):
                skipped += 1
                continue

            if process_file(md_file):
                print(f"Corregido: {md_file}")
                modified += 1

        print(f"\nResumen: {modified} archivo(s) modificado(s), {skipped} saltado(s)")

    else:
        print(f"Error: {target} no existe")
        sys.exit(1)


if __name__ == '__main__':
    main()
