# Pruebas

Esta guía cubre cómo ejecutar y escribir pruebas para gitbox. Para el inventario completo de pruebas (cada nombre de prueba y qué cubre), consulta [testing-reference.md](testing-reference.md). Recuento actual: 208 pruebas en todos los paquetes.

## Pre-push hook

El repo incluye una red de seguridad: un pre-push hook que ejecuta análisis estático y todas las pruebas unitarias antes de cada `git push`.

Git no recoge hooks personalizados automáticamente, así que después de clonar el repo ejecuto esto una vez:

```bash
git config core.hooksPath .githooks
```

A partir de ahora cada `git push` ejecuta los checks. Para saltarlo temporalmente (no recomendado): `git push --no-verify`.

## Niveles de prueba

Hay tres niveles de pruebas, cada uno con algo más de preparación que el anterior:

- **Pruebas unitarias** — se ejecutan al instante, no necesitan setup. Prueban lógica en aislamiento sin tocar la red ni ningún proveedor.
- **Pruebas de integración** — conectan con proveedores reales (GitHub, GitLab, Gitea, etc.) usando credenciales reales. Necesitan un pequeño archivo de configuración que preparo una vez.
- **Pruebas de escenario** — ejecutan el ciclo completo de gitbox end-to-end: crear cuentas, clonar repos, comprobar status, configurar mirrors y desmontarlo todo. Mismo archivo de configuración que las pruebas de integración.

## Antes de empezar: el fixture de pruebas

Las pruebas de integración y escenario necesitan hablar con proveedores Git reales. El proyecto usa un archivo llamado `test-gitbox.json` en la raíz del repo — una configuración normal de gitbox con un campo extra por cuenta: una clave `_test` que guarda el token de ese proveedor. El test runner lo lee, inyecta los tokens como variables de entorno y ejecuta todo en directorios temporales descartables para no tocar nunca la máquina real.

**Las pruebas unitarias funcionan sin este archivo.** Si intentas ejecutar pruebas de integración sin él, fallan con un mensaje claro que te dice que lo crees (o que uses `go test -short` para ejecutar solo unit tests).

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

Antes de ejecutar pruebas de integración, **ejecuta el script de setup al menos una vez** para verificar tokens y generar claves SSH:

```bash
./scripts/setup-credentials.sh
```

Esto verifica tokens API, genera pares de claves SSH por host y prueba conexiones SSH. Si un token está mal o expirado, lo verás aquí — mucho más rápido que depurar una prueba fallida. El script es idempotente y seguro de ejecutar varias veces. Cuando todo muestre `ok` verde, estás listo para pruebas de integración.

Para ejecutar credential setup también en máquinas remotas: `./scripts/setup-credentials.sh all`. Consulta [multiplatform.md](multiplatform.md) para el workflow cross-platform completo.

### Cuentas GCM

Las credenciales GCM viven en el keyring del OS y salen de un login interactivo en navegador — no hay token que poner en un archivo. El test runner comprueba en runtime si `git credential fill` funciona y salta pruebas si no. No añadas una clave `_test` a cuentas GCM — solo asegúrate de que GCM está configurado en la máquina.

### Pruebas de mirrors (opcional)

Para probar operaciones de mirror, añade una sección `mirrors` con un par de cuentas real:

```json
"mirrors": {
  "gh-to-forgejo": {
    "account_src": "github-personal",
    "account_dst": "forgejo-testuser",
    "repos": {}
  }
}
```

Ambas cuentas necesitan tokens en sus claves `_test`. El token de destino necesita acceso de escritura porque las pruebas de mirror crean repos ahí.

### Seguridad

El test runner **siempre sobrescribe** `global.folder` con un directorio temporal descartable — todos los clones y archivos de config van ahí y se borran después de cada prueba.

Para `credential_ssh.ssh_folder`, las pruebas de integración leen la ruta desde `test-gitbox.json` para poder encontrar claves SSH reales. Esta ruta **no debe ser `~/.ssh`** — apúntala a una ubicación aislada como `~/.gitbox-test/ssh`.

El test runner lo exige: si el fixture apunta `ssh_folder` a `~/.ssh` o `global.folder` a `~/.config/gitbox`, las pruebas fallan inmediatamente.

## Ejecutar las pruebas

Recomiendo ejecutar pruebas incrementalmente, ganando confianza a medida que avanzas.

### Paso 1: pruebas unitarias (sin credenciales)

Confirma que el código compila y que la lógica básica funciona. Sin red, sin proveedores, sin `test-gitbox.json`.

```bash
go test -short ./...
```

Las pruebas de probing WSL en `pkg/git` se saltan por defecto en Windows. Para ejercitarlas, define `GITBOX_TEST_WSL=1` y vuelve a ejecutar `go test ./pkg/git/`. El probe ejecuta `wsl.exe --status` y salta limpiamente si WSL no está instalado; en sistemas no Windows las pruebas afirman que los helpers devuelven false / error.

### Paso 2: verificación de credenciales

Comprueba que los tokens de proveedor en `test-gitbox.json` funcionan realmente — conecta a la API de cada proveedor y confirma que la autenticación tiene éxito.

```bash
go test -v -run TestIntegration_CLI_CredentialVerify ./cmd/cli/
```

### Paso 3: discovery

Usa cuentas y tokens de `test-gitbox.json` para llamar a la API de cada proveedor y listar repositorios.

```bash
go test -v -run TestIntegration_CLI_Discover ./cmd/cli/
```

### Paso 4: clone, status, pull, fetch

Elige la primera cuenta con sources y repos, clona uno en una carpeta temporal, comprueba estado de sync, hace pull y fetch. El clone se borra automáticamente.

```bash
go test -v -run "TestIntegration_CLI_(Clone|Status|Pull|Fetch)" ./cmd/cli/
```

### Paso 5: pruebas de integración TUI

Ejecuta la TUI programáticamente (sin UI visible). Carga cuentas desde `test-gitbox.json`, simula el event loop de Bubble Tea, envía pulsaciones y comprueba la salida renderizada.

```bash
go test -v -run TestIntegration_TUI ./cmd/cli/tui/
```

### Paso 6: escenario de ciclo completo

La grande. Ejecuta todo el workflow CLI de gitbox: crea cuentas, añade sources y repos, clona, comprueba status, hace pull, configura mirrors, borra un clone, reclona y luego desmonta todo en orden inverso.

```bash
go test -v -run TestScenario ./cmd/cli/
```

### Paso 7: todas las pruebas de integración juntas

```bash
go test -v -run Integration ./cmd/cli/... ./cmd/cli/tui/
```

### Paso 8: todo

Ejecuta en verbose, ignora caché:

```bash
go test -v -p 1 -count=1 ./...
```

## Checklist pre-PR

Ejecuta esto antes de cada push o PR. Todo automatizado — o deja que el pre-push hook se encargue de vet + unit tests.

```text
- [ ] go vet ./...
- [ ] go test -short ./...
- [ ] ./scripts/deploy.sh                  (cross-compile; despliega a remotos si están configurados)
- [ ] ./scripts/smoke.sh                  (smoke tests en todas las plataformas configuradas)
```

Si el cambio toca un área específica, verifica al menos en la máquina dev:

```text
- [ ] Cambios de config → gitbox global show --json parsea correctamente
- [ ] Cambios de comando CLI → ejecutar con --help y una invocación real
- [ ] Cambios TUI → lanzar gitbox (sin args), navegar a la pantalla cambiada
- [ ] Cambios de credenciales → verificar badges de estado de credenciales en dashboard
- [ ] Cambios GUI → lanzar GitboxApp, verificar que la pantalla cambiada renderiza
```

## Checklist completa de release

Ejecuta antes de crear un tag de release. Combina pasos automatizados + interactivos en todas las plataformas.

### Automatizado

```text
- [ ] go vet ./...
- [ ] go test -short ./...              (pruebas unitarias)
- [ ] go test ./...                     (integración + escenario, requiere test-gitbox.json)
- [ ] ./scripts/deploy.sh                  (cross-compile + deploy a remotos)
```

### Smoke CLI (todas las plataformas)

Ejecuta `./scripts/smoke.sh all` o manualmente en cada plataforma:

```text
- [ ] gitbox version
- [ ] gitbox help
- [ ] gitbox global show --json
- [ ] gitbox account list --json
- [ ] gitbox status --json
```

### Verificación TUI (interactiva, todas las plataformas)

Lanza `gitbox` (sin args) en cada plataforma:

```text
- [ ] Dashboard carga con tarjetas de cuenta y badges de credenciales
- [ ] Cambio de tab (Accounts ↔ Mirrors) funciona
- [ ] Detalle de cuenta vía Enter en tarjeta
- [ ] Pantalla de credenciales para cada tipo (token/gcm/ssh)
- [ ] Discovery: cuenta → discover → multi-select → save
- [ ] Detalle de repo: status, ruta de clone
- [ ] Settings: cambiar carpeta, verificar persistencia
- [ ] Hints de teclado renderizan, Esc navega atrás, Ctrl+C sale
```

### Flujos de credenciales CLI (interactivos, por plataforma)

```text
- [ ] Windows: token, gcm (navegador), ssh (key gen)
- [ ] macOS: token, gcm (navegador vía `open`), ssh
- [ ] Linux: token, ssh, gcm-over-SSH (mensaje "desktop session")
```

### Clone y sync (al menos 1 plataforma)

```text
- [ ] gitbox clone → clona repos faltantes
- [ ] gitbox status → muestra clean/dirty/ahead/behind
- [ ] gitbox pull → hace pull de repos que están behind
- [ ] gitbox fetch → hace fetch sin merge
```

### Verificación GUI (al menos Windows)

```text
- [ ] App abre sin console flash
- [ ] Dashboard muestra cuentas y repos
- [ ] Credential setup funciona
- [ ] Flujos discovery y clone/pull/fetch funcionan
- [ ] Tab mirror muestra grupos y status
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

1. Añadir la prueba automatizada relevante al [inventario de pruebas](testing-reference.md)
2. Añadir un paso de verificación manual a la sección adecuada anterior (TUI, CLI, GUI)
3. Si la feature es sensible a plataforma, añadir una nota a la tabla específica por plataforma

Si usas Claude Code, el skill `/test-plan` automatiza los checks pre-PR y guía los pasos interactivos.
