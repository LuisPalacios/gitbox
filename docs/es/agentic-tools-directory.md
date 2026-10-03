# Directorio del ecosistema agentic

La lista autoritativa de harnesses de IA, orquestadores, CLIs e IDEs conocidos vive en [`pkg/harness/tools-directory.md`](../../pkg/harness/tools-directory.md).

Ese archivo se embebe en el binario GUI mediante `//go:embed` y se parsea al arrancar: las filas cuyo `Category` es `Agentic CLI`, `AI Harness`, `Headless Harness`, `Agentic IDE` o `Agentic IDE / CLI`, y cuya celda `Executable / CLI Command` contiene uno o más identificadores entre backticks (por ejemplo `` `claude` ``, `` `aider` ``, o "`` `agent` `` or `` `cursor-agent` ``" para una CLI renombrada), se sondean en el host y se añaden a las entradas de menú "Open in AI harness" en la GUI.

Dos columnas dirigen la detección más allá del nombre del binario:

- `Executable / CLI Command` puede listar varios nombres. El primero es el binario principal; el resto son alternativas que se sondean en orden, de modo que una herramienta renombrada, o que se distribuye con otro nombre en Windows, sigue resolviéndose.
- `Well-known locations` lista directorios de instalación por herramienta que no están en un PATH típico, cada uno como su propio token entre backticks. `~`, `$VAR` y `%VAR%` se expanden cuando corre el sondeo. Usa `*N/A*` para herramientas que viven en PATH o en `~/.local/bin`, que gitbox ya sondea en todos los sistemas junto con los prefijos de Homebrew en macOS.

La detección corre en segundo plano (poco después de arrancar la GUI, cada diez minutos y al recuperar el foco la ventana). Una herramienta detectada aterriza en `global.ai_harnesses` con `source: "detected"`; cuando su binario desaparece después, la entrada se marca con `missing: true` y se oculta, nunca se borra, para que una reinstalación la restaure con los `args` del usuario intactos.

Para añadir o quitar un harness detectado, edita [`pkg/harness/tools-directory.md`](../../pkg/harness/tools-directory.md) en vez de añadir un archivo nuevo aquí — mantener una única fuente de verdad evita divergencias entre la documentación para usuarios y la lista embebida que el binario parsea realmente.

Consulta [gui-guide.md → acciones de AI harness](gui-guide.md#acciones-de-ai-harness) para ver cómo el menú usa esta lista en runtime.
