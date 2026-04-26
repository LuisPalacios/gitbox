# Guia CLI

[Read in English](../cli-guide.md)

## Configuracion inicial

Inicio la configuracion con:

```bash
gitbox init
```

Puedo seleccionar Espanol para la salida humana con `--lang es`, `GITBOX_LANG=es` o guardando `global.language` en la configuracion:

```bash
gitbox --lang es --help
gitbox global update --language es
```

## Flujo habitual

- Creo una cuenta con `gitbox account add`.
- Configuro credenciales con `gitbox credential`.
- Descubro repos con `gitbox discover`.
- Clono con `gitbox clone`.
- Reviso estado con `gitbox status`.

Los nombres de comandos, flags, claves JSON, proveedores y valores de estado no se traducen.
