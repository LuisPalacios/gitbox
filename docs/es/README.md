# Documentación

[Read the documentation in English](../README.md)

## Primeros colaboradores

Si eres nuevo en el proyecto, lee estos documentos en orden:

1. [Guía de desarrollo](developer-guide.md) — requisitos previos y compilación desde el código fuente
2. [Pruebas](testing.md) — ejecución de pruebas, preparación del fixture de pruebas, checklists pre-PR y de release
3. [Multiplataforma](multiplatform.md) — flujo de build, despliegue y prueba multiplataforma (opcional pero recomendado)

## Guías de usuario

| Doc                                  | Qué contiene                                                       |
| ------------------------------------ | ------------------------------------------------------------------ |
| [Guía GUI](gui-guide.md)             | App de escritorio: cuentas, mirrors, creación de repos, discovery  |
| [Guía CLI](cli-guide.md)             | Paso a paso: init, cuentas, credenciales, discover, mirrors        |
| [Credenciales](credentials.md)       | Configuración detallada de Token, GCM y SSH                        |
| [Completado de shell](completion.md) | Autocompletado de la CLI con Tab para Bash, Zsh, Fish y PowerShell |

## Guías de desarrollo

| Doc                                           | Qué contiene                                                                          |
| --------------------------------------------- | ------------------------------------------------------------------------------------- |
| [Guía de desarrollo](developer-guide.md)      | Compilar desde el código fuente, hooks de git, contribución                           |
| [Multiplataforma](multiplatform.md)           | Flujo de build, despliegue y prueba multiplataforma                                   |
| [Pruebas](testing.md)                         | Niveles de prueba, configuración de fixtures, checklists pre-PR y de release          |
| [Flujo de worktrees](worktree-workflow.md)    | Trabajo paralelo por issue: una sesión de Claude por worktree, push/merge con puertas |
| [Referencia de pruebas](testing-reference.md) | Inventario completo de pruebas, detalles internos del harness                         |
| [Arquitectura](architecture.md)               | Diseño técnico, diagrama de componentes                                               |
| [Firma en macOS](macos-signing.md)            | Configuración de firma y notarización para releases de macOS                          |

## Referencia

| Doc                                             | Qué contiene                                                         |
| ----------------------------------------------- | -------------------------------------------------------------------- |
| [Referencia](reference.md)                      | Todos los comandos, formato de configuración, estructura de carpetas |
| [Ejemplo JSON anotado](../../json/gitbox.jsonc) | Ejemplo del archivo `gitbox.json`                                    |
| [JSON Schema](../../json/gitbox.schema.json)    | El schema usado en el archivo `gitbox.json`                          |
