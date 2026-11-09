# Flujo paralelo basado en worktrees

Uso este flujo para trabajar en varios issues de GitHub a la vez, cada uno en su propia sesión de Claude Code o Codex y en su propia rama, sin que las sesiones se pisen. Lo impulsan dos skills: `/work-issue <N>` crea un worktree hermano y lleva el issue por plan, código, verificación, push y PR; `/merge-pr <PR#>` hace el merge del PR y limpia.

Los skills aplican mi regla de publicación: nada llega a GitHub hasta que yo lo digo. En Codex ejecuto `$work-issue <N>` y `$merge-pr <PR#>` con los mismos argumentos y puertas. Ambas herramientas cargan las instrucciones y skills compartidas que describo en [developer-guide.md](developer-guide.md#trabajar-con-claude-code-y-codex).

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
6. **Auto-verificar**: `go vet ./...`, las pruebas enfocadas en lo que cambió y el build de la GUI con un smoke check `GitboxApp --version`. Los cambios solo de documentación se saltan el build.
7. **Puerta de smoke test**: me da el commit, la ruta del binario que debo lanzar y una comprobación concreta, además del comando de push que pretende ejecutar. Yo pruebo. Si algo falla, lo arregla y me ofrece un build nuevo. Cuando está bien, digo "push it".
8. **Sincronizar con main**: hace fetch y, si `origin/main` se movió, ofrece `git rebase origin/main`.
9. **Puerta de push**: ejecuta `git push -u origin <branch>`.
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
4. Pone una puerta sobre CI: todos los checks deben pasar. Se detiene ante un check fallido e informa de su log; también se detiene mientras los checks siguen en ejecución, y lo vuelvo a lanzar más tarde.
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
