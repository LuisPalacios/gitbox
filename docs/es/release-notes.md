# Notas de la próxima versión

## Los hooks globales de Git ya no pausan todos los clones

Gitbox 2.8.0 y 2.8.1 pausaban el fetch y el pull de todos los clones cuando la configuración global de Git asignaba `core.hooksPath`, por ejemplo una carpeta personal de hooks `commit-msg` y `pre-commit`, y solo mostraban `unsafe_config (core.hooksPath)`. Gitbox ahora confía en la configuración global y de sistema de Git. Solo pausa un clon cuando su propia configuración apunta `core.hooksPath` fuera del clon, asigna un programa a `core.fsmonitor` o tiene una regla `url.*.insteadOf` que envía `origin` a otro servidor. La fila explica entonces el ajuste, dónde está y cómo quitarlo, y las tarjetas de resultado cuentan esos clones como omitidos por seguridad. Consulta [Credenciales](credentials.md).

## El modo remoto está siempre activo

El selector de equipo junto al título aparece en cada ventana, sin opción ni ajuste. Gitbox ignora la opción retirada `--experimental-remote`, y el siguiente guardado quita `experimental_remote` de `gitbox.json`. Un `gitbox.json` sin claves remotas carga como antes. Consulta [Modo remoto](remote-mode.md#el-selector-de-equipo).

GitboxApp 2.7.4 y anteriores quitan la lista `remotes` de `gitbox.json` al guardarlo, sin copia de seguridad. Actualiza todas las copias de GitboxApp de una máquina antes de añadir remotos en ella.

## La desinstalación espera la aprobación y luego se detiene

Cuando una desinstalación desde la ventana Equipos remotos espera al agente SSH, la ventana lo dice con el nombre del equipo y, con 1Password, señala su icono de la barra de menús o de la bandeja. Un inicio de sesión que sigue esperando a los 75 segundos se detiene con `ssh_login`, y la desinstalación sigue con el siguiente equipo. Las conexiones, las reinstalaciones y los diagnósticos comparten el mismo límite. Un rechazo de 1Password tras un inicio de sesión lento se muestra como el error del agente, con el mismo aviso. Tras una desinstalación, el equipo sigue en la lista y su ficha muestra **No instalado** en lugar de la versión antigua del servidor.
