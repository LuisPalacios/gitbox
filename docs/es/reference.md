# Referencia

[Read in English](../reference.md)

## Idioma

gitbox resuelve el idioma de textos humanos en este orden:

- Flag global `--lang en|es`.
- Variable `GITBOX_LANG`.
- Campo `global.language` en `gitbox.json`.
- Locale del sistema operativo.
- Ingles como fallback.

## Configuracion

El archivo vive en `~/.config/gitbox/gitbox.json` y usa `version: 2`.

```json
{
    "version": 2,
    "global": {
        "folder": "~/00.git",
        "language": "es"
    },
    "accounts": {},
    "sources": {}
}
```

No se traducen claves JSON, comandos, flags, nombres de proveedores, codigos de salida ni valores de estado.

## Comandos

- `gitbox init`
- `gitbox global show`
- `gitbox global update --language es`
- `gitbox account`
- `gitbox source`
- `gitbox repo`
- `gitbox clone`
- `gitbox status`
- `gitbox mirror`
- `gitbox workspace`
- `gitbox doctor`
