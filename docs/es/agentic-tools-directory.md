# Directorio del ecosistema agentic

La lista autoritativa de harnesses de IA, orquestadores, CLIs e IDEs conocidos vive en [`pkg/harness/tools-directory.md`](../../pkg/harness/tools-directory.md).

Ese archivo se embebe en el binario GUI mediante `//go:embed` y se parsea al arrancar: las filas cuyo `Category` es `Agentic CLI`, `AI Harness`, `Headless Harness`, `Agentic IDE` o `Agentic IDE / CLI`, y cuya celda `Executable / CLI Command` contiene un único identificador entre backticks (por ejemplo `` `claude` ``, `` `aider` ``, `` `cursor` ``), se autodetectan en `PATH` y se añaden a las entradas de menú "Open in AI harness" en la GUI.

Para añadir o quitar un harness detectado, edita [`pkg/harness/tools-directory.md`](../../pkg/harness/tools-directory.md) en vez de añadir un archivo nuevo aquí — mantener una única fuente de verdad evita divergencias entre la documentación para usuarios y la lista embebida que el binario parsea realmente.

Consulta [gui-guide.md → acciones de AI harness](gui-guide.md#acciones-de-ai-harness) para ver cómo el menú usa esta lista en runtime.
