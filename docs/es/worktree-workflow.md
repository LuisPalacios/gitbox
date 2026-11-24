# Flujo paralelo basado en worktrees

Uso este flujo para trabajar en varios issues de GitHub a la vez, cada uno en su propia sesión de Claude Code o Codex y en su propia rama, sin que las sesiones se pisen. Lo impulsan dos skills: `/work-issue <N>` crea un worktree hermano y lleva el issue por plan, código, verificación, push y PR; `/merge-pr <PR#>` hace el merge del PR y limpia.

Los skills aplican mi regla de publicación: nada llega a GitHub hasta que yo lo digo. En Codex ejecuto `$work-issue <N>` y `$merge-pr <PR#>` con los mismos argumentos y puertas. Ambas herramientas cargan las instrucciones y skills compartidas que describo en [developer-guide.md](developer-guide.md#trabajar-con-claude-code-y-codex).

## Issues independientes y work units

Uso dos formas de trabajo: un issue independiente sin padre ni hijos, o una work unit que contiene tareas. Una work unit lleva la etiqueta `work-unit` y no tiene padre. Sus tareas son sub-issues directas de GitHub en el mismo repositorio, sin hijos propios. Mantengo exactamente estos dos niveles; no añado un paraguas por encima de la work unit. Un título, una referencia en el cuerpo o una checklist no establece una relación entre padre e hijo.

Creo el contenedor con `$wu-container <description>` en Codex o `/wu-container <description>` en Claude Code. El skill redacta su propósito, el problema, el alcance y las exclusiones, requisitos de alto nivel con identificadores estables `HLR1`, y criterios de aceptación completos con identificadores `AC1` vinculados a esos requisitos. Comprueba el trabajo existente y presenta el issue concreto antes de cualquier publicación que necesite mi aprobación. Para un issue independiente uso `wu-container --independent`; implemento ese issue con `work-issue`.

Después uso `wu-tasks <N>` para descomponer una work unit. El skill lee el contenedor completo y sus hijos existentes, propone tareas que cubran sus criterios de aceptación e identifica dependencias y un orden de ejecución útil. Redacta los cuerpos de las tareas antes de solicitar cualquier autorización de publicación que falte. Crea y vincula solo las tareas aprobadas, verifica las relaciones nativas y el orden de la lista, y reanuda el trabajo parcial inspeccionando los efectos existentes antes de reintentar. No reescribe los requisitos ni los criterios de aceptación del contenedor para ajustarlos a las tareas.

## Un supervisor para un lote

Uso `--count` para elegir cuántas tareas directas de una work unit puede procesar el supervisor. El valor predeterminado es de tres issues para todo el lote. Uso `--concurrency` para solicitar entre 1 y 25 workers simultáneos. Por defecto toma el menor valor entre el número de issues y 25. Por ejemplo, puedo poner 30 issues en cola y solicitar 20 workers. El supervisor informa del límite de la sesión cuando lo conoce y del número de workers que ha lanzado.

El supervisor comprueba las dependencias, las sesiones activas, las PR abiertas y los archivos afectados antes de cada lanzamiento. Si dispone de cuatro plazas para workers, lanza hasta cuatro tareas independientes. En cuanto termina una, puede empezar la siguiente tarea lista. La issue terminada sigue reservada durante la revisión o mientras tenga una PR abierta. Sus dependientes esperan hasta que el supervisor verifica el merge y la resolución completa de la issue. Los ciclos y los requisitos previos sin resolver aparecen en el informe del lote.

En Codex paso el número de issue de la work unit y declaro trabajo que ya está en marcha en otra sesión:

```text
$wu-coordinate-tasks <N> --count 3 --active none
$wu-coordinate-tasks <N> --count 30 --concurrency 20 --active <issue-numbers>
$wu-coordinate-tasks <N> --resume <owner>
```

En Claude Code sustituyo `$wu-coordinate-tasks` por `/wu-coordinate-tasks`. Ahí el supervisor me pide que ejecute `/add-dir` para cada worktree nuevo antes de lanzar su agente, también en los lanzamientos posteriores, porque los worktrees hermanos quedan fuera del directorio de trabajo de la sesión. El número de work unit es obligatorio, también al reanudar: si lo omito, el supervisor lo pregunta y nunca deduce un contenedor ni recurre al backlog global. Si omito la información sobre sesiones activas, la pregunta una vez antes de reservar el lote. Las casillas aportan contexto; las relaciones nativas y el estado actual de issues y PR determinan qué trabajo pertenece a esta unidad y sigue pendiente. No necesito abrir una conversación por agente.

La invocación autoriza selección, planificación, implementación, verificación y commits locales. Puedo autorizar explícitamente la publicación del lote seleccionado con `--publish`, que cubre los pushes de sus ramas y la creación o actualización de PR después de superar las comprobaciones. `--update-work-unit` autoriza actualizaciones mínimas de casillas de finalización en ese contenedor tras verificar la resolución. Creo tareas adicionales mediante `wu-tasks`; el coordinador no las crea. Estas opciones no autorizan un merge, un push forzado, cambios de credenciales reales ni staging remoto. El supervisor mantiene la publicación y las preguntas en la conversación principal; los agentes no publican. Preguntar cómo funciona el skill no inicia un lote.

`wu-coordinate-tasks` sustituye a `coordinate-issues`; `--roadmap`, `--no-roadmap`, `--create-subissues` y `--update-roadmap` ya no se aplican. Los roadmaps existentes, como #214, necesitan una revisión explícita y una conversión autorizada antes de cumplir los requisitos de una work unit. Los skills no los etiquetan silenciosamente ni convierten sus referencias en sub-issues nativas.

### Reservas y recuperación

Las sesiones de Claude y Codex que cooperan comparten un registro SQLite atómico en `<git-common-dir>/gitbox-coordination/registry.sqlite`. El registro cubre un clon local y sus worktrees; no coordina clones o máquinas independientes. Sigo declarando las sesiones externas para que el supervisor tenga en cuenta sus issues y archivos asignados.

El supervisor reserva juntas las tareas seleccionadas antes de crear worktrees. Un conflicto deja toda la solicitud sin reservar, para que pueda reconsiderar el lote. El registro también excluye otro lote para la misma work unit hasta que el supervisor libere todas las reservas de sus tareas. Registra el propietario y su generación, work unit, worktree, rama, agente, progreso y evidencias de verificación. Un heartbeat antiguo no libera una reserva: el supervisor comprueba los agentes que siguen activos, el estado de Git y las PR antes de recuperarla con una generación nueva. Una PR abierta mantiene su tarea ocupada. Tras una interrupción, paso el número de work unit y el propietario registrado para reanudar; el supervisor comprueba los efectos existentes antes de reintentar un push o la creación de una PR.

El helper exige `work_unit` al iniciar un lote. Acepta una reserva sin ámbito con `external: true` solo para excluir trabajo de otra sesión. El registro conserva su campo histórico `roadmap` por compatibilidad; ese campo por sí solo nunca demuestra la pertenencia nativa. Al reanudar un lote antiguo, el supervisor valida la work unit elegida y cada tarea pendiente contra GitHub antes de continuar. Conserva los registros antiguos y el trabajo sin terminar.

Puedo ejecutar sesiones de supervisor separadas para distintas work units. Sus tareas siguen compartiendo un repositorio y pueden solaparse en archivos, API o resultados generados, así que el supervisor comprueba esos límites antes de delegar trabajo.

Las sesiones de GUI, la configuración compartida, las operaciones de credenciales y los destinos de staging necesitan coordinación exclusiva. Una reserva no concede permiso para usar un recurso. El supervisor serializa los smoke tests de GUI porque las instancias en modo de prueba comparten una identidad de aplicación, y comprueba qué binario y ventana está probando.

### Verificación y finalización

El supervisor vincula cada comprobación obligatoria al commit y la base probados, con PASS, FAIL o PENDING y las evidencias del comando o interacción. Un rebase o un cambio nuevo invalida las evidencias anteriores. Un smoke automatizado puede satisfacer un criterio interactivo cuando verifica el binario y commit correctos, el fixture aislado, la ventana, las acciones y el comportamiento esperado. Una cadena de versión o una captura por sí sola no demuestra que funcione la interacción. Solo realizo los pasos obligatorios que las herramientas disponibles no pueden verificar.

Las comprobaciones obligatorias fallidas o pendientes bloquean la publicación. Cuando la publicación cuenta con autorización y pasan las comprobaciones locales, el supervisor abre la PR y confirma que el gate local pasó sobre su head antes de declararla lista. Se detiene antes del merge; uso `merge-pr` por separado. Con autorización para actualizar la work unit, el flujo de merge o un supervisor reanudado marca solo las entradas existentes de la checklist cuya PR integrada resuelve toda la tarea. El helper opcional de checklist conserva el nombre de archivo `roadmap.py`; una checklist nunca sustituye la pertenencia nativa. Una PR parcial o un gate o una CI correctos por sí solos no completan una tarea, y terminar un lote no cierra automáticamente su contenedor ni demuestra todos sus criterios de aceptación.

## Qué me da este flujo

- Una sesión por issue, cada una en su propio worktree y su propia rama.
- Un `gh auth switch` automático a la cuenta propietaria del `origin` de este clon, así nunca hago push desde la cuenta equivocada. Los skills la derivan de `remote.origin.url` y nunca fijan un usuario en el código, así que también funcionan en un fork.
- Una pausa explícita antes de cada paso de publicación: push, PR y merge.
- Una salida limpia: cuando el PR se mergea, desaparecen el worktree y la rama local.

## Abrir una sesión por issue

Desde el clon principal:

1. Abro una ventana nueva de Claude Code o Codex.
2. Escribo `/work-issue 42`.
3. El skill lee el issue y muestra su título, sus etiquetas, el nombre de la rama y la ruta del worktree. La rama es `<type>/<N>-<slug>`: `fix` para un issue `bug`, `feat` para un `enhancement` y `chore` en los demás casos. El slug sale del título, y el worktree va a `../gitbox-<N>-<slug>`.
4. Cuando confirmo que es el issue correcto, el skill crea el worktree desde `origin/main` y continúa allí.

Para trabajar en el issue 43 al mismo tiempo, abro una segunda ventana y ejecuto `/work-issue 43` en ella. También puedo abrir una sesión nueva directamente dentro de un worktree; el skill reanuda desde la fase correcta.

## Las puertas

El skill se detiene en cada puerta y me dice qué viene después:

1. **Preflight**: comprueba que se ejecuta en el clon principal, cambia la cuenta de `gh`, hace fetch de `origin`, lee el issue y propone la rama y la ruta del worktree.
2. **Crear worktree**: tras mi confirmación ejecuta `git worktree add ../gitbox-<N>-<slug> -b <branch> origin/main`.
3. **Entender**: relee el issue, lo resume en cinco líneas y me pregunta si estoy listo para planificar.
4. **Plan**: adopta mi plan o escribe uno conmigo. Consulta [Proporcionar mi propio plan](#proporcionar-mi-propio-plan).
5. **Implementar**: ciclos de edición y commit. El último commit lleva `Closes #<N>`. Todo se queda en local.
6. **Auto-verificar**: `go vet ./...`, las pruebas enfocadas en lo que cambió y el build de la GUI con un smoke check `GitboxApp --version`. Los cambios solo de documentación se saltan el build. Después, `./scripts/gate.sh` verifica el commit final en macOS, Linux y Windows (consulta [Gate local](local-gate.md)).
7. **Puerta de smoke test**: me da el commit, la ruta del binario que debo lanzar y una comprobación concreta, además del comando de push que pretende ejecutar. Yo pruebo. Si algo falla, lo arregla y me ofrece un build nuevo. Cuando está bien, digo "push it".
8. **Sincronizar con main**: hace fetch y, si `origin/main` se movió, ofrece `git rebase origin/main`.
9. **Puerta de push**: ejecuta `git push -u origin <branch>` y después `./scripts/gate.sh --report` para mostrar el resultado del gate en el commit.
10. **Puerta de PR**: redacta un título y un cuerpo anonimizados (`Closes #<N>`, qué cambió, plan de pruebas), espera mi aprobación y ejecuta `gh pr create`.
11. **Parar**: informa de la URL del PR y me dice que ejecute `/merge-pr` cuando esté listo.

El skill nunca hace merge. El merge es una acción separada y deliberada.

## Proporcionar mi propio plan

Cuando ya tengo un plan, paso su fuente como segundo argumento:

```text
/work-issue 42 ./plans/42.md      # archivo local
/work-issue 42 https://.../42     # URL
/work-issue 42 issue              # una sección del cuerpo del issue
```

Con `issue`, el skill lee la sección del cuerpo del issue titulada `Plan`, `Implementation`, `Approach` o `Proposed solution`. Sin segundo argumento, busca igualmente esas secciones.

Un plan está completo cuando cubre tres cosas:

- Qué archivos cambian y por qué.
- Pasos de prueba o verificación.
- Decisiones arquitectónicas o trade-offs no obvios.

Un plan completo se adopta tal cual: el skill lo resume, me pide confirmación y se salta plan mode. Un plan parcial sirve de semilla para plan mode, donde el skill señala los huecos y los rellenamos. Sin plan, el skill planifica desde cero. En cualquier caso, apruebo el plan antes de que implemente nada.

## Ejecutar dos sesiones en paralelo

Los árboles de código están aislados, pero parte del estado se comparte entre worktrees:

- `~/.config/gitbox/gitbox.json` es un solo archivo. Nunca ejecuto la configuración de credenciales ni los flujos de primer arranque en dos sesiones a la vez; se pelean por el mismo archivo.
- El agente SSH, Git Credential Manager y la config de git del sistema son globales. Los pushes no dan problemas; dos configuraciones de credenciales a la vez, sí.
- `go test -short ./...` en paralelo no da problemas. La prueba de escenario de `go test ./...` trabaja contra el mismo fixture de proveedor, así que la ejecuto en una sola sesión cada vez.
- Dos worktrees no pueden tener la misma rama en checkout; git lo rechaza.
- Nunca hago push a la misma rama remota desde dos worktrees.

## Worktrees y la detección de clones propia de Gitbox

Cuando el clon principal vive bajo una de las raíces de clonado de Gitbox, un worktree hermano queda también dentro de esa raíz. La pasada de reconcile lo encuentra y lo lista en **Clones sin ubicar** como "un worktree de git de otro clon". Un worktree no es un clon propio: su `.git` es un archivo puntero al clon principal, así que Gitbox nunca ofrece adoptarlo.

Tres formas de convivir con ello:

1. Ignorarlo. La fila desaparece cuando `/merge-pr` elimina el worktree.
2. Hacer clic en **Ignorar** en su fila de Clones sin ubicar para ocultarlo.
3. Crear el worktree fuera de todas las raíces de clonado con `git worktree add` a mano. El skill siempre usa la ruta hermana.

## Cuando main avanza debajo de mí

Cuando se mergea un PR mientras sigo trabajando en el mío, normalmente desde mi otra sesión, `origin/main` se mueve. La puerta Sync-with-main hace fetch, lista los commits nuevos y ofrece `git rebase origin/main`. El skill me muestra cada archivo en conflicto y no resuelve por su cuenta los conflictos no triviales.

Después de un rebase, auto-verificar y la puerta de smoke test se repiten. Mi "push it" anterior no se conserva, porque el rebase cambió lo que probé. Si los commits rebasados ya estaban publicados, el skill hace push con `--force-with-lease`.

## Mergear

Cuando estoy listo:

```text
/merge-pr <PR#>
```

Se ejecuta en cualquier sesión, la que creó el worktree o una nueva. El skill:

1. Lee el PR y encuentra su worktree por el nombre de la rama.
2. Comprueba que el worktree no tiene cambios sin commit ni sin push, y se detiene si los tiene, salvo que yo lo fuerce. Esta comprobación se hace antes del merge porque `gh pr merge --delete-branch` también borra la rama local y su worktree.
3. Hace fetch y, si la rama base se movió, ofrece un rebase y vuelve a hacer push con `--force-with-lease`.
4. Pone una puerta sobre el gate local: el commit head necesita un marcador del gate correcto o un estado `local-gate`, y ofrece ejecutar el gate cuando no lo hay. Un check de GitHub fallido también lo detiene e informa de su log; un check que sigue en ejecución no.
5. Me muestra `gh pr merge <PR#> --squash --delete-branch` y espera mi autorización.
6. Después del merge, vuelve al clon principal, avanza `main` en fast-forward, elimina cualquier worktree y rama local que `gh` haya dejado y poda los metadatos obsoletos de worktrees.

Si el cuerpo del PR dice `Closes #N`, GitHub cierra el issue al hacer el merge.

## Referencia de limpieza

Para limpiar a mano (trabajo abandonado, una sesión que se cayó):

```bash
# Eliminar un worktree. Se niega si está dirty.
git worktree remove ../gitbox-42-my-slug

# Forzar la eliminación aunque esté dirty.
git worktree remove --force ../gitbox-42-my-slug

# Podar metadatos de worktrees borrados del disco sin que git lo sepa.
git worktree prune

# Borrar la rama local.
git branch -D fix/42-my-slug

# Listar los worktrees registrados.
git worktree list
```

## FAQ

**¿Puedo trabajar en una rama sin issue?** No con estos skills: `/work-issue` requiere un número de issue. Para trabajo exploratorio abro antes un issue `chore:` corto.

**¿Qué pasa si abandono el trabajo a mitad del flujo?** Dejo el worktree donde está. Cuando vuelvo a ejecutar `/work-issue <N>`, el skill lee el estado de git (rama, worktrees, último commit, PR abierto) y reanuda en la fase correcta. Para descartar el trabajo, ejecuto `git worktree remove --force` y `git branch -D`.

**¿Cómo continúo un worktree en otra máquina?** Hago push de la rama y hago checkout de ella allí de forma normal. No hace falta replicar la estructura de worktrees.

**¿Puedo saltarme la fase de plan?** Paso un plan completo como segundo argumento. El skill lo adopta y solo me pide confirmación antes de implementar.

**¿Qué pasa si `/merge-pr` falla después del merge pero antes de limpiar?** Lo vuelvo a ejecutar desde el clon principal. Ve que el PR ya está mergeado y pasa directamente a la limpieza.
