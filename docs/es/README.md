# Documentación

[Read in English](../README.md)

Esta carpeta contiene la documentación de usuario en español. Mantengo los nombres de comandos, flags, claves JSON, proveedores y valores internos en inglés para que puedas copiar los ejemplos directamente en la terminal y compararlos con la salida real de `gitbox`.

## Guías de usuario

- [Guía de la GUI](gui-guide.md): instala y usa `GitboxApp`, añade cuentas, gestiona repositorios, mirrors, workspaces y acciones de mantenimiento desde la interfaz gráfica.
- [Guía de la CLI](cli-guide.md): empieza desde cero con `gitbox init`, añade cuentas, descubre repos, clona, sincroniza y automatiza tareas desde la terminal.
- [Credenciales](credentials.md): elige entre GCM, SSH y tokens, configura cada tipo y resuelve problemas habituales de autenticación.
- [Completado de shell](completion.md): instala autocompletado para Bash, Zsh, Fish o PowerShell.

## Referencia

- [Referencia de comandos y configuración](reference.md): lista de comandos, flags, formato de configuración y casos de resolución de problemas.
- [Ejemplo JSON anotado](../../json/gitbox.jsonc): archivo `gitbox.json` de ejemplo con comentarios.
- [JSON Schema](../../json/gitbox.schema.json): esquema para editores y validación.

## Documentos técnicos en inglés

Algunos documentos de mantenimiento todavía viven solo en inglés porque están dirigidos a desarrollo, empaquetado o pruebas:

- [Arquitectura](../architecture.md)
- [Guía de desarrollo](../developer-guide.md)
- [Pruebas](../testing.md)
- [Pruebas, referencia interna](../testing-reference.md)
- [Multiplataforma](../multiplatform.md)
- [Firma en macOS](../macos-signing.md)
