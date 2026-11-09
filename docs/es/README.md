# Documentación

[Read the documentation in English](../README.md)

## Para usuarios

| Doc                            | Qué contiene                                                                                    |
| ------------------------------ | ----------------------------------------------------------------------------------------------- |
| [Guía de la GUI](gui-guide.md) | Instalación, primer arranque, cuentas, sincronización, acciones, mirrors, contenedores, ajustes |
| [Credenciales](credentials.md) | Configuración de GCM, token y SSH, scopes de token, resolución de problemas                     |

## Para colaboradores

Empieza por la guía de desarrollo y luego sigue con las pruebas. Multiplataforma es opcional, pero recomendable.

| Doc                                                             | Qué contiene                                                                                      |
| --------------------------------------------------------------- | ------------------------------------------------------------------------------------------------- |
| [Guía de desarrollo](developer-guide.md)                        | Requisitos previos, compilación, comprobación de salud, ampliación del código, proceso de release |
| [Pruebas](testing.md)                                           | Niveles de prueba, el fixture de pruebas, checklists pre-PR y de release                          |
| [Referencia de pruebas](testing-reference.md)                   | Inventario de pruebas por paquete y detalles internos del harness                                 |
| [Multiplataforma](multiplatform.md)                             | Compilar, enviar y hacer smoke tests en hosts remotos por SSH                                     |
| [Flujo de worktrees](worktree-workflow.md)                      | Trabajo paralelo por issue con una sesión de agente por worktree                                  |
| [Arquitectura](architecture.md)                                 | Diseño, responsabilidades de los paquetes, referencia del archivo de config                       |
| [Firma de releases](release-signing.md)                         | Cómo se firman y verifican las releases, configuración del agente, forks                          |
| [Firma en macOS](macos-signing.md)                              | Firma de código y notarización opcionales de Apple en CI                                          |
| [Directorio del ecosistema agentic](agentic-tools-directory.md) | Cómo lee su catálogo la detección de AI harnesses                                                 |

## Referencia

| Doc                                                                        | Qué contiene                                                       |
| -------------------------------------------------------------------------- | ------------------------------------------------------------------ |
| [Referencia del archivo de config](architecture.md#4-formato-de-config-v5) | Todas las claves de `gitbox.json`, estructura de carpetas, backups |
| [Ejemplo anotado](../../json/gitbox.jsonc)                                 | Un `gitbox.json` completo con comentarios                          |
| [JSON Schema](../../json/gitbox.schema.json)                               | Schema para validación y autocompletado en el editor               |
