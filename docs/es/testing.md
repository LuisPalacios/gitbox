# Pruebas

Esta guía cubre cómo ejecutar y escribir pruebas para gitbox. Para el inventario de pruebas (qué cubre cada paquete) y los detalles internos del harness, consulta [testing-reference.md](testing-reference.md). Recuento actual: 592 funciones de prueba de primer nivel — 517 en `pkg/` y 75 en `cmd/gui/`.

## Pre-push hook

El repo incluye una red de seguridad: un pre-push hook que ejecuta una comprobación `gofmt -s`, análisis estático y todas las pruebas unitarias antes de cada `git push`. Primero construye el frontend de la GUI cuando falta `cmd/gui/frontend/dist`.

Git no recoge hooks personalizados automáticamente, así que después de clonar el repo ejecuto esto una vez:

```bash
git config core.hooksPath .githooks
```

A partir de ahora cada `git push` ejecuta los checks. Para saltarlo temporalmente (no recomendado): `git push --no-verify`.

## Niveles de prueba

Hay tres niveles de pruebas, cada uno con algo más de preparación que el anterior:

- **Pruebas unitarias de paquete** — las pruebas bajo `pkg/`. Se ejecutan al instante, no necesitan setup y prueban lógica en aislamiento sin tocar la red ni ningún proveedor. `pkg/ops/ops_test.go` cubre las operaciones de la aplicación contra un `XDG_CONFIG_HOME` temporal, un `GIT_CONFIG_GLOBAL` temporal y una carpeta SSH temporal, así que nada cambia en el host real.
- **Pruebas de GUI** — las pruebas Go bajo `cmd/gui/`. Cubren la lógica del lado de Wails que no necesita ventana: lanzamiento de terminales y de AI harness, acciones de navegador y de carpeta, workspaces y contenedores multi-repo. Necesitan el frontend compilado en `cmd/gui/frontend/dist`, porque el paquete lo embebe.
- **Prueba de escenario** — `TestScenario_FullLifecycle` en `pkg/ops/scenario_test.go` recorre el ciclo de vida completo contra un proveedor real: añadir cuenta → comprobar credenciales → discover → clone → status → pull y fetch → editar cuenta + reconfigurar clone → CRUD de mirror → reclonar → renombrar → borrar. Necesita el fixture de pruebas descrito más abajo.

## Antes de empezar: el build del frontend

`cmd/gui` embebe `cmd/gui/frontend/dist`, así que `go vet ./...` y `go test ./...` no consiguen compilar ese paquete hasta que la carpeta existe. Un `wails build` la crea. Para crearla sin un build completo:

```bash
cd cmd/gui/frontend
npm ci
npm run build
```

Las pruebas de paquete por sí solas (`go test ./pkg/...`) no la necesitan.

## Antes de empezar: el fixture de pruebas

La prueba de escenario necesita hablar con un proveedor Git real. El proyecto usa un archivo llamado `test-gitbox.json` en la raíz del repo — una configuración normal de gitbox con un campo extra por cuenta: una clave `_test` que guarda el token de ese proveedor. El test runner lo lee, inyecta los tokens como variables de entorno y ejecuta todo en directorios temporales descartables para no tocar nunca la máquina real.

**Las pruebas unitarias funcionan sin este archivo.** Si ejecutas la prueba de escenario sin él, falla con un mensaje claro que te dice que lo crees (o que uses `go test -short` para saltarla).

### Prepararlo

Copia la plantilla:

```bash
cp json/test-gitbox.json.example test-gitbox.json
```

El archivo está gitignored — contiene secretos reales y no debe commitearse nunca.

Edita la sección `accounts` con cuentas reales de proveedores. Para cada una, añade una clave `_test` con un Personal Access Token:

```json
"github-personal": {
   :
   "_test": {
     "token": "ghp_xxxxxxxxxxxxxxxxxxxx"
   }
}
```

Puedes añadir tantas cuentas como quieras. El test runner elige la primera que tiene sources con repos y un token válido. Las cuentas sin clave `_test` son solo display — aparecen para pruebas UI pero no se hacen llamadas API contra ellas.

### Crear un token

Creas tokens en la web de tu proveedor, igual que harías para gitbox:

| Proveedor           | Dónde crearlo                                          | Permisos necesarios                                    |
| ------------------- | ------------------------------------------------------ | ------------------------------------------------------ |
| **GitHub**          | Settings → Developer settings → Personal access tokens | `repo` (full), `read:user`                             |
| **Gitea / Forgejo** | Settings → Applications → Manage Access Tokens         | Repository: Read+Write, User: Read, Organization: Read |
| **GitLab**          | Preferences → Access Tokens                            | scope `api`                                            |
| **Bitbucket**       | Personal settings → App passwords                      | Repositories: Read+Write                               |

### Verificar tu setup

Antes de ejecutar la prueba de escenario, **ejecuta el script de setup al menos una vez** para verificar tokens y generar claves SSH:

```bash
./scripts/setup-credentials.sh
```

Esto verifica tokens API, genera pares de claves SSH por host y prueba conexiones SSH. Si un token está mal o expirado, lo verás aquí — mucho más rápido que depurar una prueba fallida. El script es idempotente y seguro de ejecutar varias veces. Cuando todo muestre `ok` verde, estás listo para la prueba de escenario.

Para ejecutar credential setup también en máquinas remotas: `./scripts/setup-credentials.sh all`. Consulta [multiplatform.md](multiplatform.md) para el workflow cross-platform completo.

### Cuentas GCM

Las credenciales GCM viven en el keyring del OS y salen de un login interactivo en navegador — no hay token que poner en un archivo. No añadas una clave `_test` a cuentas GCM — la prueba de escenario solo elige cuentas con token, y las cuentas GCM siguen disponibles para comprobaciones manuales en `--test-mode`.

### Pruebas de mirrors (opcional)

Para comprobar a mano operaciones de mirror en `--test-mode`, añade una sección `mirrors` con un par de cuentas real:

```json
"mirrors": {
  "gh-to-forgejo": {
    "account_src": "github-personal",
    "account_dst": "forgejo-testuser",
    "repos": {}
  }
}
```

Ambas cuentas necesitan tokens en sus claves `_test`. El token de destino necesita acceso de escritura porque configurar un mirror crea ahí el repo de destino. La prueba de escenario no necesita esta sección — su paso de mirror solo crea y borra un grupo de mirror en la config.

### Seguridad

El test runner **siempre sobrescribe** `global.roots` con un único directorio temporal descartable — todos los clones y archivos de config van ahí y se borran después de cada prueba.

Para `credential_ssh.ssh_folder`, la prueba de escenario lee la ruta desde `test-gitbox.json` para poder encontrar claves SSH reales. Esta ruta **no debe ser `~/.ssh`** — apúntala a una ubicación aislada como `~/.gitbox-test/ssh`.

El test runner lo exige: si el fixture apunta `ssh_folder` a `~/.ssh` o una raíz de `global.roots` a `~/.config/gitbox`, las pruebas fallan inmediatamente.

## Ejecutar las pruebas

Recomiendo ejecutar pruebas incrementalmente, ganando confianza a medida que avanzas.

### Paso 1: pruebas unitarias (sin credenciales)

Confirma que el código compila y que la lógica básica funciona. Sin red, sin proveedores, sin `test-gitbox.json`.

```bash
go test -short ./...
```

Las pruebas de probing WSL en `pkg/git` se saltan por defecto en Windows. Para ejercitarlas, define `GITBOX_TEST_WSL=1` y vuelve a ejecutar `go test ./pkg/git/`. El probe ejecuta `wsl.exe --status` y salta limpiamente si WSL no está instalado; en sistemas no Windows las pruebas afirman que los helpers devuelven false / error.

### Paso 2: comprobación de tipos del frontend

Comprueba el frontend Svelte con `svelte-check`, la misma comprobación que ejecuta el workflow de PR.

```bash
cd cmd/gui/frontend
npm run check
```

También ejecuto `npm test` desde `cmd/gui/frontend` para las regresiones puras del árbol de clones: propiedad entre sources, prevención de duplicados, plegado de clones anidados, orden alfabético y recuperación ante referencias obsoletas a padres. La misma suite cubre la traducción de estados (la forma, el color, la etiqueta del glosario, el tipo Simple, el rango de atención y los contadores de cada estado, y si su fila abre el panel de detalle), la salud de la cuenta, la vista guardada y la lista de Requiere atención, los resúmenes de Fetch de todo y Pull de todo, y las tarjetas de resultado. La comprobación de salud ejecuta estas pruebas como `frontend-test`.

### Paso 3: escenario de ciclo completo

La grande. Ejecuta el ciclo de vida completo de una cuenta a través de `pkg/ops` contra la primera cuenta del fixture con token: crea la cuenta, comprueba credenciales, descubre repos, clona uno, comprueba status, hace pull, hace fetch, edita la cuenta y reconfigura el clone, crea y borra un grupo de mirror, reclona, renombra la cuenta y lo borra todo. Cada paso guarda y recarga la config, igual que la GUI persiste después de cada acción.

```bash
go test -v -run TestScenario ./pkg/ops/
```

### Paso 4: todo

Ejecuta en verbose, ignora caché:

```bash
go test -v -p 1 -count=1 ./...
```

### Paso 5: modo de prueba interactivo

Ejecuta la app contra el fixture en lugar de mi config real:

```bash
GitboxApp --test-mode
```

El flag `--test-mode` lee `test-gitbox.json` (buscando hacia arriba desde el directorio actual), construye una config descartable en un directorio temporal, sobrescribe `global.roots` e inyecta los tokens del fixture como variables de entorno. Nada toca mi `~/.config/gitbox/` real ni los clones existentes, y el directorio temporal se borra cuando la app sale.

### Comprobaciones de regresión del arranque

Ejecuto las pruebas de instantánea de arranque y descubrimiento de workspaces de `cmd/gui` para verificar la anidación antes de que existan los clones, los datos de respuesta independientes, el acceso a config durante un escaneo bloqueado, la conservación de ediciones concurrentes y los reintentos cuando cambian las entradas del descubrimiento. Mido por separado la construcción de la instantánea con `go test ./cmd/gui/ -run '^$' -bench BenchmarkStartupSnapshot -benchmem`. Este benchmark mide el trabajo del backend, no el tiempo de arranque visible.

Para medir el arranque interactivo, registro por separado el lanzamiento del proceso, la aparición de la ventana, las primeras filas configuradas y el estado completo, junto con la plataforma, el número de clones, la disposición de contenedores y el número de ejecuciones. Distingo los arranques nativos de producción de la navegación del navegador contra un backend `wails dev` ya en ejecución. Necesito un escritorio Windows interactivo para medir el renderizado de WebView2; los tiempos del backend por SSH no bastan para demostrar el objetivo de las primeras filas. Mantengo abiertos los objetivos sin medir hasta completar esa comprobación de escritorio.

### Comprobaciones de regresión del refresco

Ejecuto `go test -race ./cmd/gui/ ./pkg/credential/ ./pkg/heal/ ./pkg/git/` para la coordinación del refresco, la cancelación, los resultados obsoletos y el mantenimiento de credenciales. Uso canales para bloquear trabajo inyectado y solapar disparadores sin un runtime Wails. Las pruebas de proveedores usan servidores HTTP locales para verificar la concurrencia limitada, los sondeos compartidos, los plazos y la conservación del estado upstream tras errores o rotación de credenciales.

Trazo el mantenimiento previo al fetch con `GIT_TRACE2_EVENT` contra clones aislados: los clones SSH, token y GCM sin cambios deben lanzar dos comandos Git de solo lectura cada uno. Compruebo las fechas de modificación de config y archivos de credenciales, la rotación de tokens, los permisos, la cancelación de helpers, las transiciones de tipo, los worktrees enlazados y los avisos cuando los includes de config sobrescriben las reparaciones. Registro la plataforma, el número de clones, el número de procesos y el tiempo transcurrido por separado de la duración del fetch de red.

### Mediciones del refresco (2026-10-15)

Comparé el mantenimiento previo al fetch de `da83922` con la implementación de #131 usando el mismo fixture aislado: 24 clones locales, una cuenta y ningún fetch de red. Ejecuté una pasada secuencial en caliente por plataforma, tipo de credencial y estado. Para los casos con diferencias, cambié ambos campos de identidad, origin y helper antes de cada pasada. Usé Go 1.26.1 para el harness independiente; Windows usó Git 2.53.0.windows.1.

Medí el bucle completo de mantenimiento en el proceso padre y conté los eventos de inicio de procesos Git con Trace2. Los tiempos incluyen creación de procesos, ejecución Git, operaciones de archivos y sobrecarga del trazado. Estas pasadas no separan el tiempo interno de Git, no miden el pintado de la GUI ni estiman un refresco de red completo con ocho workers.

| Plataforma    | Estado de los clones | Credencial | Procesos Git antes → después | Segundos antes → después |
| ------------- | -------------------- | ---------- | ---------------------------- | ------------------------ |
| macOS arm64   | sin cambios          | SSH        | 216 → 48                     | 0.902 → 0.191            |
| macOS arm64   | sin cambios          | Token      | 240 → 48                     | 1.013 → 0.202            |
| macOS arm64   | sin cambios          | GCM        | 192 → 48                     | 0.923 → 0.278            |
| Windows amd64 | sin cambios          | SSH        | 216 → 48                     | 9.831 → 2.037            |
| Windows amd64 | sin cambios          | Token      | 240 → 48                     | 12.396 → 2.382           |
| Windows amd64 | sin cambios          | GCM        | 192 → 48                     | 9.386 → 2.202            |
| macOS arm64   | con diferencias      | SSH        | 288 → 264                    | 1.311 → 1.216            |
| macOS arm64   | con diferencias      | Token      | 312 → 288                    | 1.457 → 1.319            |
| macOS arm64   | con diferencias      | GCM        | 264 → 288                    | 1.222 → 1.402            |
| Windows amd64 | con diferencias      | SSH        | 288 → 264                    | 12.774 → 11.085          |
| Windows amd64 | con diferencias      | Token      | 312 → 288                    | 13.576 → 12.089          |
| Windows amd64 | con diferencias      | GCM        | 264 → 288                    | 11.105 → 13.073          |

Mantengo los clones sin cambios en dos procesos Git de solo lectura cada uno. La reparación de diferencias con GCM añade validación de la config efectiva tras las escrituras, por lo que su caso con diferencias cuesta más que la referencia; conservo esas comprobaciones para evitar fetches con ajustes que sobrescriben un include o la config de un worktree.

En una pasada adicional de Windows con 24 clones SSH sin cambios, registré 8.918 → 1.963 s en el proceso padre, frente a 5.421 → 1.193 s de tiempo interno Git sumado de los campos `exit.t_abs` de las sesiones Trace2 de nivel superior. La diferencia restante fue de 3.497 → 0.770 s. Esa diferencia incluye inicio de procesos, IPC y operaciones de archivos del llamador; no la atribuyo solo al antivirus ni a `HideWindow`. Este dato procede de una muestra distinta de las filas anteriores.

También ejecuté regresiones nativas de sondeos en Windows amd64 con Go 1.27.0 y servidores HTTP locales. Doce upstreams compartidos por dos sources produjeron doce peticiones, una búsqueda de credenciales y un máximo de cuatro peticiones concurrentes. Con un timeout reducido de veinte milisegundos por petición, doce peticiones bloqueadas terminaron en 61.34 ms. Con un presupuesto total de veinte milisegundos, el sondeo se detuvo en 20.57 ms después de cuatro peticiones. Estas comprobaciones del fixture verifican la planificación y la cancelación; los límites de producción siguen en diez segundos por petición y treinta segundos por sondeo, y estas cifras no predicen la latencia real del proveedor.

## Checklist pre-PR

Ejecuta esto antes de cada push o PR. El pre-push hook se encarga de gofmt + vet + unit tests, y el workflow de PR ejecuta `scripts/health.sh` como barrera.

```text
- [ ] ./scripts/health.sh                  (todos los comprobadores: Go, frontend, scripts, workflows, docs)
- [ ] go vet ./...
- [ ] go test -short ./...
- [ ] cd cmd/gui/frontend && npm run check
- [ ] cd cmd/gui && wails build             (build local de la GUI)
- [ ] ./scripts/smoke.sh                   (GitboxApp --version en todas las plataformas configuradas)
```

Si el cambio toca un área específica, verifica al menos en la máquina dev:

```text
- [ ] Cambios de config → lanzar GitboxApp, confirmar que la config carga y que el cambio persiste tras reiniciar
- [ ] Cambios de credenciales → verificar las palabras de salud de la cuenta en la barra lateral y en la tarjeta de cuenta
- [ ] Cambios GUI → lanzar GitboxApp, verificar que la pantalla cambiada renderiza
- [ ] Windows → no aparece ninguna ventana de consola al ejecutar la acción cambiada
```

## Checklist completa de release

Ejecuta antes de crear un tag de release. Combina pasos automatizados + interactivos en todas las plataformas.

### Automatizado

```text
- [ ] go vet ./...
- [ ] go test -short ./...              (pruebas unitarias)
- [ ] go test ./...                     (prueba de escenario, requiere test-gitbox.json)
- [ ] cd cmd/gui/frontend && npm run check
- [ ] ./scripts/ship.sh                 (construye y prepara la GUI en cada remoto)
- [ ] ./scripts/smoke.sh all            (GitboxApp --version en todas las plataformas)
```

### Verificación GUI (interactiva, todas las plataformas)

Lanza `GitboxApp` en cada plataforma (`./scripts/run-commands.sh` imprime los comandos):

```text
- [ ] La app abre; en Windows sin console flash
- [ ] Primer arranque guiado con una config nueva (`XDG_CONFIG_HOME=<carpeta temporal vacía>` en macOS y Linux): carpeta → conectar (inicio de sesión con el navegador; **Use a token instead**; **Change details** conserva cualquier carpeta existente) → elegir proyectos → **Download**; la pantalla principal se abre en Simple y los repos elegidos se clonan. En Simple, **Conectar una cuenta** en la barra lateral ejecuta los mismos pasos de conectar y elegir; en Advanced, **Añadir cuenta** abre el formulario completo
- [ ] Dashboard muestra la barra lateral (Requiere atención, Todos los repos, cuentas con logo, salud y conteo) y la vista seleccionada
- [ ] Barra lateral: cambiar de vista (Requiere atención, Todos los repos, cada cuenta, Mirrors, Workspaces) funciona; el botón de arriba a la izquierda y ⌘B / Ctrl+B la pliegan al riel y la despliegan; por debajo de 780 px muestra el riel y el botón la abre sobre el contenido; la vista y el riel sobreviven a un reinicio
- [ ] Tarjeta de cuenta: tres líneas (marca de salud y nombre, palabras de salud y acción de arreglo, conteos con Buscar y + Crear en los dos modos); una credencial rota muestra su arreglo en la tarjeta y al principio de Requiere atención
- [ ] Editar y renombrar cuenta
- [ ] Credential setup para cada tipo (token/gcm/ssh)
- [ ] Discovery: Find projects → seleccionar → Add & Pull
- [ ] Clone, Pull de todo, Fetch de todo actualizan los indicadores de status; la tira de actividad muestra el alcance mientras se ejecutan y una tarjeta de resultado los resume
- [ ] Panel de detalle de repo muestra rama, ahead/behind, archivos cambiados
- [ ] La vista Mirrors muestra grupos y status, con Descubrir y Comprobar todo en su cabecera
- [ ] Settings: Raíces de clonado (añadir una raíz, hacerla predeterminada, quitarla), System check, Terminals Manager
- [ ] Con dos raíces, el Clonar de una fila pregunta qué raíz y acaba ahí; Pull de todo no pregunta
- [ ] Vista compacta y vuelta a la vista completa
- [ ] Simple / Advanced: el interruptor de la barra superior (gris Simple, rojo Advanced) cambia de modo al instante; las filas en Simple solo muestran punto, nombre, frase de estado, Update o Download cuando hace falta, los iconos de IA / editor / terminal y ⋯; las filas en Advanced mantienen todo el detalle y muestran los mismos tres iconos antes del botón de fetch; los anillos de la cabecera en Advanced son compactos (icono, insignia de atención, tooltip al pasar el ratón, sin etiqueta); una config nueva acaba en Simple tras el onboarding
- [ ] Contraer/expandir: la cabecera de cuenta y un clon padre (chevron o nombre) se pliegan y despliegan; los pliegues sobreviven a un reinicio; un chip de mirror o de workspace se abre en su tarjeta y vuelve a plegarse con un clic fuera o Escape
```

### Flujos de credenciales (interactivos, por plataforma)

```text
- [ ] Windows: token, gcm (navegador), ssh (key gen)
- [ ] macOS: token, gcm (navegador), ssh
- [ ] Linux: token, gcm (navegador en una sesión de escritorio), ssh
```

### Actualización y upgrade (al menos 1 plataforma)

```text
- [ ] ./scripts/sign-release.sh --check pasa (la clave de releases está en el agente SSH)
- [ ] La píldora de actualización aparece cuando existe un release más nuevo, y la actualización se aplica tras reiniciar
- [ ] Una instalación v1 se actualiza a v2 y conserva el gitbox.json existente
```

### Notas específicas por plataforma

| Área                | Windows                                   | macOS                     | Linux                                  |
| ------------------- | ----------------------------------------- | ------------------------- | -------------------------------------- |
| Store GCM           | Windows Credential Manager                | macOS Keychain            | `secretservice` o `gpg`                |
| SSH agent           | OpenSSH agent o Pageant                   | System ssh-agent          | System ssh-agent                       |
| Binario Git         | `git.exe` (Git for Windows)               | `/usr/bin/git` o Homebrew | `git` del sistema                      |
| Abrir browser (GCM) | Funciona siempre                          | Funciona incluso vía SSH  | Necesita `DISPLAY` o `WAYLAND_DISPLAY` |
| Framework GUI       | Wails + WebView2                          | Wails + WebKit            | Wails + WebKitGTK                      |
| Ruta config         | `%APPDATA%/gitbox/` o `~/.config/gitbox/` | `~/.config/gitbox/`       | `~/.config/gitbox/`                    |

## Añadir checks nuevos

Cuando añado una feature nueva, actualizo este archivo:

1. Añadir la prueba automatizada relevante y actualizar el [inventario de pruebas](testing-reference.md)
2. Añadir un paso de verificación manual a la sección GUI adecuada anterior
3. Si la feature es sensible a plataforma, añadir una nota a la tabla específica por plataforma

Uso `/test-plan` en Claude Code o `$test-plan` en Codex para automatizar los checks pre-PR y guiar los pasos interactivos.
