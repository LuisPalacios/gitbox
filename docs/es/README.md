# Documentación

[Read the documentation in English](../README.md)

## Primeros colaboradores

Si eres nuevo en el proyecto, lee estos documentos en orden:

1. [Guía de desarrollo](developer-guide.md) — requisitos previos y compilación desde el código fuente
2. [Pruebas](testing.md) — ejecución de pruebas, preparación del fixture de pruebas, checklists pre-PR y de release
3. [Multiplataforma](multiplatform.md) — flujo de build, envío y prueba multiplataforma (opcional pero recomendado)

## Guías de usuario

| Doc                            | Qué contiene                                                                     |
| ------------------------------ | -------------------------------------------------------------------------------- |
| [Guía GUI](gui-guide.md)       | App de escritorio: instalación, cuentas, discovery, mirrors, workspaces, ajustes |
| [Credenciales](credentials.md) | Configuración detallada de Token, GCM y SSH, resolución de problemas             |

## Guías de desarrollo

| Doc                                                             | Qué contiene                                                                          |
| --------------------------------------------------------------- | ------------------------------------------------------------------------------------- |
| [Guía de desarrollo](developer-guide.md)                        | Compilar desde el código fuente, hooks de git, releases, contribución                 |
| [Multiplataforma](multiplatform.md)                             | Flujo de build, envío y prueba multiplataforma                                        |
| [Pruebas](testing.md)                                           | Niveles de prueba, configuración de fixtures, checklists pre-PR y de release          |
| [Flujo de worktrees](worktree-workflow.md)                      | Trabajo paralelo por issue: una sesión de Claude por worktree, push/merge con puertas |
| [Referencia de pruebas](testing-reference.md)                   | Inventario de pruebas, detalles internos del harness                                  |
| [Arquitectura](architecture.md)                                 | Diseño técnico, diagrama de componentes, referencia del archivo de config             |
| [Firma en macOS](macos-signing.md)                              | Configuración de firma y notarización para releases de macOS                          |
| [Firma de releases](release-signing.md)                         | Cómo se firman las releases, configuración del agente SSH, tu propia clave en un fork |
| [Directorio del ecosistema agentic](agentic-tools-directory.md) | Dónde vive la lista de AI harnesses detectados automáticamente                        |

## Referencia

| Doc                                                                        | Qué contiene                                                       |
| -------------------------------------------------------------------------- | ------------------------------------------------------------------ |
| [Referencia del archivo de config](architecture.md#4-formato-de-config-v3) | Todas las claves de `gitbox.json`, estructura de carpetas, backups |
| [Ejemplo JSON anotado](../../json/gitbox.jsonc)                            | Ejemplo del archivo `gitbox.json`                                  |
| [JSON Schema](../../json/gitbox.schema.json)                               | El schema usado en el archivo `gitbox.json`                        |
