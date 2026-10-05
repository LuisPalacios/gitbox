# Pruebas

Esta guía cubre cómo ejecutar y escribir pruebas para gitbox. Para el inventario de pruebas (qué cubre cada paquete) y los detalles internos del harness, consulta [testing-reference.md](testing-reference.md). Recuento actual: 459 funciones de prueba de primer nivel — 431 en `pkg/` y 28 en `cmd/gui/`.

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

El test runner **siempre sobrescribe** `global.folder` con un directorio temporal descartable — todos los clones y archivos de config van ahí y se borran después de cada prueba.

Para `credential_ssh.ssh_folder`, la prueba de escenario lee la ruta desde `test-gitbox.json` para poder encontrar claves SSH reales. Esta ruta **no debe ser `~/.ssh`** — apúntala a una ubicación aislada como `~/.gitbox-test/ssh`.

El test runner lo exige: si el fixture apunta `ssh_folder` a `~/.ssh` o `global.folder` a `~/.config/gitbox`, las pruebas fallan inmediatamente.

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

El flag `--test-mode` lee `test-gitbox.json` (buscando hacia arriba desde el directorio actual), construye una config descartable en un directorio temporal, sobrescribe `global.folder` e inyecta los tokens del fixture como variables de entorno. Nada toca mi `~/.config/gitbox/` real ni los clones existentes, y el directorio temporal se borra cuando la app sale.

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
- [ ] Cambios de credenciales → verificar badges de estado de credenciales en las tarjetas de cuenta
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
- [ ] Dashboard muestra tarjetas de cuenta, badges de credenciales y repos
- [ ] Cambio de tab (Accounts ↔ Mirrors ↔ Workspaces) funciona
- [ ] Editar y renombrar cuenta
- [ ] Credential setup para cada tipo (token/gcm/ssh)
- [ ] Discovery: Find projects → seleccionar → Add & Pull
- [ ] Clone, Pull All, Fetch All actualizan los indicadores de status
- [ ] Panel de detalle de repo muestra rama, ahead/behind, archivos cambiados
- [ ] Tab Mirrors muestra grupos y status
- [ ] Settings: cambiar carpeta raíz, System check, Terminals Manager
- [ ] Vista compacta y vuelta a la vista completa
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

Si usas Claude Code, el skill `/test-plan` automatiza los checks pre-PR y guía los pasos interactivos.
