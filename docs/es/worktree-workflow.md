# Flujo paralelo basado en worktrees

Uso este flujo cuando quiero trabajar en dos o más issues de GitHub a la vez, cada uno en su propia sesión de Claude Code, cada uno en su propia rama, sin que las sesiones se pisen. Lo impulsan dos skills: `/work-issue <N>` crea un worktree hermano y lleva el issue por plan, código, test, push y PR; `/merge-pr <PR#>` gestiona el merge y la limpieza.

Los skills aplican las puertas que me importan — nada llega a GitHub sin que yo lo diga — así no tengo que volver a explicar las reglas en cada sesión.

## Qué me da este flujo

- Una sesión de Claude por issue, completamente aislada a nivel de filesystem.
- Un `gh auth switch` automático al usuario correcto para el clone, así nunca hago push desde la cuenta equivocada.
- Una pausa explícita antes de cada paso de publicación (push, PR, merge).
- Una ruta de limpieza clara: cuando el PR se mergea, desaparecen el worktree y la rama local.

## Abrir una sesión por issue

Desde el clone principal:

1. Abro una ventana nueva de Claude Code.
2. Escribo `/work-issue 42` (o el número de issue que sea).
3. El skill deriva un nombre de rama como `fix/42-my-slug`, propone un worktree en `../gitbox-42-my-slug` y me pide confirmación.
4. Al confirmar crea el worktree, hace `cd` dentro de él y avanza por las fases.

Si quiero trabajar en el issue 43 al mismo tiempo, abro una segunda ventana de Claude Code, ejecuto `/work-issue 43` en _esa_ ventana y ahora tengo dos sesiones paralelas en dos worktrees separados.

## Las puertas

El skill pausa en cada puerta y me espera. No tengo que memorizarlas — me dice qué viene después — pero este es el flujo para saber qué esperar:

- **Preflight** — el skill confirma el usuario de gh, trae origin, lee el issue, propone una rama + ruta de worktree.
- **Crear worktree** — después de mi confirmación, ejecuta `git worktree add` y cambia al directorio nuevo.
- **Entender** — relee el issue y lo resume en cinco líneas. Yo digo "ready" antes de planificar.
- **Plan (o adoptar un plan existente)** — si paso una fuente de plan, la lee y la evalúa. Si no, entra en plan mode desde cero. Consulta [Proporcionar mi propio plan](#proporcionar-mi-propio-plan) más abajo.
- **Implementar** — ciclos de edición y commit. Todo se queda local.
- **Auto-verificar** — `go vet`, pruebas enfocadas, ambos binarios compilados.
- **Puerta de smoke-test** — el skill me entrega las rutas de los binarios y una comprobación concreta. Yo pruebo. Si algo falla, se lo digo y lo arregla. Si está bien digo "push it".
- **Sincronizar con main** — comprueba si `origin/main` se movió y ofrece rebase.
- **Puerta de push** — apruebo, hace push de la rama.
- **Puerta de PR** — redacta título y cuerpo (anonimizados), apruebo, ejecuta `gh pr create`.
- **Parar** — reporta la URL del PR y me dice que ejecute `/merge-pr` cuando esté listo.

En ningún punto el skill mergea. El merge es una acción separada y deliberada.

## Proporcionar mi propio plan

Si ya he pensado el issue y tengo un plan escrito — en el cuerpo del issue, en un markdown local o en una URL — se lo paso al skill:

```bash
/work-issue 42 ./plans/42.md      # archivo local
/work-issue 42 https://.../42     # URL
/work-issue 42 issue              # usar una sección "## Plan" dentro del cuerpo del issue
```

El skill lee el plan y evalúa si cubre las tres cosas que necesita:

- Qué archivos cambiarán y por qué.
- Pasos de prueba o verificación.
- Decisiones arquitectónicas o trade-offs no obvios.

Si el plan cubre las tres, el skill lo adopta literalmente y se salta plan mode. Si es un boceto, entra en plan mode con mi plan como semilla y rellenamos los huecos juntos. Si no hay plan, planifica desde cero.

## Ejecutar dos sesiones en paralelo

Algunas cosas se comparten entre worktrees aunque los árboles de código estén aislados. Tengo esto presente:

- `~/.config/gitbox/gitbox.json` es un solo archivo. Nunca ejecuto dos flujos interactivos TUI de credenciales o init-wizard al mismo tiempo — pelean por el mismo archivo.
- El SSH agent y Git Credential Manager son globales. No es un problema para pushes, pero no ejecuto dos flujos de configuración de credenciales a la vez.
- `go test -short ./...` en paralelo va bien. `go test ./...` (pruebas de integración completas) lee fixtures compartidos y puede colisionar — las ejecuto en serie.
- Dos worktrees no pueden tener la misma rama checked out. Git lo rechaza. El mensaje de error es obvio.
- No hago push a la misma rama remota desde dos worktrees. Si alguna vez lo necesito, uno de ellos debe hacer force-push with lease después de que el otro aterrice.

## Worktrees y la detección de orphans propia de gitbox

Si mi folder root gestionado por gitbox es `~/00.git/github-<me>/<me>/`, entonces un worktree hermano en `~/00.git/github-<me>/<me>/gitbox-42-my-slug` queda _dentro_ de ese árbol gestionado. Cuando ejecuto `gitbox status`, el flujo adopt o las pantallas discovery de GUI/TUI, listan el worktree como un repo orphan — no encaja con el layout esperado de ninguna cuenta.

No está realmente orphaned. El worktree tiene un archivo puntero `.git` válido y git lo ve correctamente. gitbox escanea por estructura de directorios, no leyendo el contenido de `.git`, así que no puede distinguir un worktree de un clone no registrado.

Tres formas de convivir con ello:

1. Ignorar el ruido. El worktree desaparece cuando `/merge-pr` limpia después del merge.
2. Colocar worktrees fuera de cualquier carpeta gestionada por gitbox. Hoy no es el default del skill — usa la ruta hermana porque editores y gestores de archivos se comportan mejor ahí — pero puedo usar `git worktree add` manualmente a otra ubicación si el ruido me molesta en una rama concreta.
3. Abrir un issue de seguimiento si empieza a molestar. Aún no hay flag CLI para excluir patrones de worktree del discovery.

## Cuando main avanza debajo de mí

Si alguien (normalmente yo en otra sesión de Claude) aterriza un PR mientras sigo trabajando en el mío, la rama `origin/main` se mueve y necesito hacer rebase. El skill gestiona esto en la puerta Sync-with-main — hace fetch, comprueba commits nuevos y ofrece `git rebase origin/main`. Los conflictos se me muestran por archivo; el skill no auto-resuelve conflictos no triviales.

Después de un rebase, se repiten auto-verify y la puerta de smoke-test. Mi "ok, push it" anterior no se conserva — el rebase reescribe historia, así que el skill vuelve a preguntar.

## Mergear

Cuando estoy listo:

```bash
/merge-pr <PR#>
```

Esto se ejecuta en cualquier sesión de Claude — ya sea la que creó el worktree o una ventana nueva. El skill:

1. Verifica que el PR está rebased sobre el último `origin/main` (vuelve a hacer push con `--force-with-lease` si no).
2. Pone una puerta sobre CI — todos los checks deben estar green.
3. Me muestra el comando exacto `gh pr merge --squash --delete-branch` y espera autorización.
4. Después del merge, elimina el worktree y la rama local, y poda metadatos stale de worktrees.

Se niega a eliminar un worktree que tiene estado sin commit o sin push salvo que yo lo fuerce explícitamente.

## Referencia de limpieza

Si necesito limpiar manualmente (trabajo abandonado, crash del skill, lo que sea):

```bash
# Eliminar un worktree. Se niega si está dirty.
git worktree remove ../gitbox-42-my-slug

# Forzar eliminación aunque esté dirty.
git worktree remove --force ../gitbox-42-my-slug

# Podar metadatos de worktrees que se borraron del disco sin que git lo sepa.
git worktree prune

# Borrar la rama local.
git branch -D fix/42-my-slug

# Listar lo que está registrado ahora.
git worktree list
```

## FAQ

**¿Puedo trabajar en una rama sin issue?** Hoy no. `/work-issue` requiere un número de issue. Si quiero hacer trabajo exploratorio sin issue, abro uno primero — un solo issue `chore:` cuesta poco y mantiene consistente el flujo.

**¿Qué pasa si abandono el trabajo a mitad del flujo?** Dejo el worktree donde está. La próxima vez que invoque `/work-issue <N>`, el skill detecta el worktree existente desde `git worktree list` y reanuda en la fase correcta. Si quiero eliminar el trabajo, `git worktree remove --force` + `git branch -D`.

**¿Cómo comparto un worktree WIP con otra máquina?** Hago push de la rama a origin. En la otra máquina, clono y hago checkout de la rama normalmente — no hace falta replicar la estructura de worktrees.

**¿Puedo saltarme la fase de plan?** Sí. O paso un plan existente como segundo argumento, o le digo al skill durante la puerta Understand que el plan es trivial. Aun así confirmará conmigo antes de implementar.

**¿Qué pasa si `/merge-pr` falla después del merge pero antes de limpiar?** Lo vuelvo a invocar. Primero comprueba el estado del PR — si ya está `MERGED`, salta directamente a la limpieza.
