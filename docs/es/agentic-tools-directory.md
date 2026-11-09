# Directorio del ecosistema agentic

Gitbox decide qué harnesses de IA detecta a partir de una única tabla Markdown, [`pkg/harness/tools-directory.md`](../../pkg/harness/tools-directory.md). La tabla se embebe en `GitboxApp` con `//go:embed` y la parsea `pkg/harness`, así que añado o quito un harness detectado editando una fila ahí, nunca cambiando código Go ni añadiendo un archivo aquí.

Esta página es la referencia de cómo el parser lee esa tabla y de cómo la detección convierte las filas en entradas de `global.ai_harnesses`. Para ver cómo usa el menú esas entradas, consulta [gui-guide.md → Acciones de AI harness](gui-guide.md#acciones-de-ai-harness).

## Formato de fila

El parser (`pkg/harness/harness.go`) lee cada fila de la tabla con barras verticales excepto la cabecera (la fila que contiene `Tool Name`) y la fila de alineación. Lee cuatro columnas por posición:

- **Tool Name** (1.ª) — el nombre que se muestra en el menú y la clave en `global.ai_harnesses`. Se quitan las marcas de negrita, así que `**Claude Code**` se convierte en `Claude Code`.
- **Category** (3.ª) — decide si la fila se detecta, está retirada o es solo de referencia. Debe coincidir exactamente con uno de los valores de abajo.
- **Executable / CLI Command** (5.ª) — los nombres de binario que se sondean.
- **Well-known locations** (8.ª) — directorios de instalación fuera de un PATH típico.

Las demás columnas (creador, compatibilidad de OS, caso de uso, URL) son texto de referencia y nunca se parsean.

## Categorías

Una fila se autodetecta cuando su `Category` es una de:

- `Agentic CLI`
- `AI Harness`
- `Headless Harness`
- `Agentic IDE`
- `Agentic IDE / CLI`

Los IDEs agentic (Cursor, Devin Desktop) cuentan como herramientas de IA, no como editores: se lanzan desde una terminal en una carpeta, así que sus entradas aparecen en la sección de AI harness del menú, no en la sección de editores.

Cualquier otra categoría (`Agentic Framework`, `Orchestrator`, `Harness Builder`, plataformas en la nube, etc.) es solo de referencia. Esas herramientas no se lanzan desde una terminal en una carpeta, así que el detector las omite.

## Nombres de ejecutable

El parser conserva cada token entre backticks de la celda `Executable / CLI Command` que parece un nombre de binario simple: una letra seguida de letras, dígitos, `_`, `.` o `-`. Los tokens con espacios o rutas, como `` `python devika.py` ``, se descartan, y una fila sin ningún token válido nunca se detecta. El texto fuera de los backticks, como `_(or Docker)_`, se ignora.

Una celda puede listar varios nombres, por ejemplo `` `agent` or `cursor-agent` `` para la CLI renombrada de Cursor, o `` `devin-desktop` or `windsurf` ``. El primer nombre es el binario principal; el resto son alternativas, que se sondean en orden, de modo que una herramienta renombrada, o que se distribuye con un nombre distinto en cada plataforma, sigue resolviéndose.

## Ubicaciones conocidas

La celda `Well-known locations` lista directorios de instalación por herramienta que no están en un PATH típico, cada uno como su propio token entre backticks, por ejemplo `` `%LOCALAPPDATA%\agy\bin` `` para Antigravity en Windows. `~`, `$VAR` y `%VAR%` se expanden cuando se ejecuta el sondeo. Una celda sin ningún token entre backticks, como `_N/A_`, no añade directorios.

Para cada nombre de binario, el sondeo comprueba estos lugares en orden y se detiene en el primer acierto:

1. Las ubicaciones conocidas de la fila.
2. Los prefijos de Homebrew, en macOS.
3. `PATH`.
4. `~/.local/bin`, en todos los sistemas (los instaladores nativos de Claude Code, Codex, Antigravity, Goose y las herramientas gestionadas por uv ponen ahí los binarios, y una GUI arrancada desde el Dock, un menú de escritorio o el menú Inicio no hereda el PATH de la shell).

Las herramientas que viven en `PATH` o en `~/.local/bin` necesitan por tanto `_N/A_`, no una ubicación. El sondeo nunca modifica el entorno del proceso, así que es seguro ejecutarlo mientras hay fetches y clones en curso.

## Filas retiradas

Cuando un fabricante retira una CLI, conservo su fila y pongo `Category` a `Retired CLI` (Gemini CLI es el ejemplo actual). Las filas retiradas nunca se detectan, y la siguiente sincronización elimina cualquier entrada de `global.ai_harnesses` cuyo `name` coincida con una fila retirada, así un binario muerto deja de aparecer en el menú.

## Detección y entradas de config

La detección se ejecuta en segundo plano, fuera del camino de arranque: unos dos segundos después de abrirse la ventana, después cada diez minutos, y cada vez que la ventana recupera el foco (como mucho una vez por minuto). El menú se refresca cuando la lista cambia, sin reiniciar.

Cada pasada (`harness.Sync` en `pkg/harness/sync.go`) reconcilia `global.ai_harnesses` con la tabla y el host:

- Una fila detectada sin entrada de config se añade con `source: "detected"` y la ruta resuelta del binario como `command`.
- Cada entrada tiene un `name`, un `command` (una ruta de binario o un nombre que está en el PATH) y un array `args` opcional para flags específicos del harness. La mayoría de harnesses no necesitan flags.
- Cuando el `command` de una entrada deja de resolverse, la entrada se marca con `missing: true` y se oculta del menú, pero nunca se borra. Para una entrada detectada, gitbox primero vuelve a probar los binarios y las ubicaciones conocidas de la fila; una reinstalación, incluso en otra carpeta, quita la marca y actualiza `command`, y `args` no se toca.
- Las entradas escritas a mano (`source: "user"`) se marcan y se ocultan igual, pero gitbox nunca reescribe su `command` ni sus `args`.
- Las entradas sin `source` reciben uno en la primera pasada: `detected` cuando el nombre de archivo de `command` coincide con uno de los binarios de la fila, `user` en caso contrario.
- Los nombres duplicados se reducen a la primera entrada. Las entradas del catálogo van primero, en el orden de la tabla, seguidas de las entradas de herramientas fuera de la tabla en su orden original.

## Añadir un harness

1. Añade una fila a [`pkg/harness/tools-directory.md`](../../pkg/harness/tools-directory.md) con una de las categorías detectadas y el nombre del binario entre backticks en la celda `Executable / CLI Command`.
2. Lista los nombres de binario alternativos en la misma celda, el principal primero.
3. Rellena `Well-known locations` solo cuando el instalador pone el binario fuera de `PATH`, `~/.local/bin` y los prefijos de Homebrew; si no, usa `_N/A_`.
4. Opcionalmente, dale un logo: prepara `logo-<name>.svg` con la skill `preparing-brand-logos`, como se describe en [developer-guide.md → Logos de herramientas e iconos de la barra superior](developer-guide.md#logos-de-herramientas-e-iconos-de-la-barra-superior), y asocia el harness a él en `harnessLogo` (`cmd/gui/frontend/src/lib/BrandIcon.svelte`). Sin esa asociación, la entrada muestra el icono genérico de IA.
5. Ejecuta `go test ./pkg/harness/`.
