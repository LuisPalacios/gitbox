# Firma de releases

Cada release de gitbox incluye `checksums.sha256.sig`, una firma SSH sobre el `checksums.sha256` de la release. El actualizador de `GitboxApp` se niega a instalar una release cuya firma falte o no venga de la clave de releases compilada en el binario. Esta página explica cómo funciona la firma, cómo la ejecuto en cada release y cómo la configura un fork con su propia clave.

## Por qué se firman las releases

El actualizador siempre ha comparado cada artefacto descargado con su SHA256 en `checksums.sha256`. Eso demuestra que la descarga no está corrupta, pero los dos ficheros vienen de la misma release de GitHub, así que cualquiera capaz de subir assets a la release podría sustituir un artefacto y su checksum a la vez. La firma añade autenticidad: solo quien tiene la clave privada puede producirla, y esa clave nunca sale de la máquina del mantenedor.

## Cómo funciona

El flujo tiene tres actores: CI, el mantenedor y el actualizador en la máquina de cada usuario.

1. Publico un tag de versión. CI compila todos los artefactos, escribe `checksums.sha256` y crea la release de GitHub como **borrador**. Un borrador es invisible para los usuarios y para el actualizador.
2. Ejecuto `./scripts/sign-release.sh <tag>` en mi máquina. El script descarga el `checksums.sha256` del borrador, comprueba que lista exactamente los assets de la release y firma este mensaje con `ssh-keygen -Y sign` a través de mi agente SSH:

   ```text
   gitbox <tag>
   <checksums.sha256, byte a byte>
   ```

   El agente me pide aprobar la firma. Después el script verifica la nueva firma contra `allowed_signers`, la sube como `checksums.sha256.sig` y publica la release.
3. Cuando el `GitboxApp` de un usuario encuentra la nueva release, descarga primero `checksums.sha256` y `checksums.sha256.sig` y comprueba la firma: debe usar el namespace `gitbox-release`, venir de la clave compilada en el binario y cubrir exactamente este tag y estos checksums. Solo entonces descarga el artefacto y compara su SHA256. Cualquier fallo rechaza la actualización.

El tag dentro del mensaje ata cada firma a una release, así que una firma antigua no se puede reutilizar en un tag nuevo. El namespace `gitbox-release` impide que una firma hecha con la misma clave para otra cosa, como un commit de git, pase por firma de release.

## Qué se guarda dónde

- La **clave privada** vive solo en el agente SSH del mantenedor. Nunca está en el repositorio, ni en los secretos de CI, ni en disco como fichero.
- La **clave pública** vive en `pkg/update/release-signing-key.pub`. El actualizador la incrusta al compilar, y `sign-release.sh` pasa el mismo fichero a `ssh-keygen -Y sign`, así que el agente elige la clave privada correspondiente.
- `allowed_signers`, en la raíz del repositorio, publica la misma clave pública en el formato que lee `ssh-keygen -Y verify`, para la verificación manual. Un test unitario falla si este fichero y `release-signing-key.pub` llegan a listar claves distintas.
- La **firma** vive en cada release como `checksums.sha256.sig`.

## Elegir un agente SSH

Sirve cualquier agente SSH, siempre que:

- Guarde una clave Ed25519 (`ssh-ed25519`).
- Hable el protocolo estándar de agente SSH, para que `ssh-add -L` liste la clave y `ssh-keygen -Y sign` pueda usarla.
- Sea accesible desde la shell donde ejecuto el script: a través de `SSH_AUTH_SOCK` en macOS y Linux, y a través de la named pipe de OpenSSH de Windows en Windows.

Los gestores de contraseñas con agente SSH integrado encajan bien, porque la clave se sincroniza con la bóveda, nunca existe como fichero y cada firma puede requerir una aprobación. El `ssh-agent` normal con un fichero de clave también funciona.

En Windows, el `ssh-keygen` propio de Git Bash no puede alcanzar un agente detrás de la named pipe de Windows. Por eso `sign-release.sh` usa el `C:\Windows\System32\OpenSSH\ssh-keygen.exe` nativo cuando existe. Define `SSH_KEYGEN` para cambiar el binario en cualquier plataforma.

### Ejemplo: 1Password

1. En la app de escritorio de 1Password, abre **Settings → Developer** y activa **Use the SSH agent**.
2. Crea la clave: **New Item → SSH Key → Add Private Key → Generate New Key**, tipo **Ed25519**. Ponle cualquier título, por ejemplo `SSH Gitbox`, y guárdala en cualquier bóveda.
3. Asegúrate de que el agente la ofrece. Por defecto el agente sirve las claves de la bóveda Personal o Private. Si mantienes un `agent.toml` (`%LOCALAPPDATA%\1Password\config\ssh\agent.toml` en Windows, `~/.config/1Password/ssh/agent.toml` en macOS y Linux), añade la clave por su título exacto y reinicia el agente:

   ```toml
   [[ssh-keys]]
   item = "SSH Gitbox"
   ```

4. En macOS y Linux, apunta `SSH_AUTH_SOCK` al socket del agente (`~/Library/Group Containers/2BUA8C4S2C.com.1password/t/agent.sock` en macOS, `~/.1password/agent.sock` en Linux). En Windows, el agente se queda con la named pipe de OpenSSH; el servicio de Windows **OpenSSH Authentication Agent** debe estar desactivado.
5. Compruébalo y después ejecuta la comprobación de firma descrita en [Comprobar la configuración](#comprobar-la-configuración):

   ```bash
   ssh-add -L    # en Windows: /c/Windows/System32/OpenSSH/ssh-add.exe -L
   ```

### Ejemplo: Bitwarden o Vaultwarden

La app de escritorio de Bitwarden incluye un agente SSH y funciona igual contra un servidor Bitwarden o un Vaultwarden autoalojado (Vaultwarden necesita una versión que soporte elementos de clave SSH).

1. En la app de escritorio de Bitwarden, abre **Settings** y activa **Enable SSH agent**. Opcionalmente, configúralo para que pida autorización cada vez que se use una clave.
2. Crea la clave: **New → SSH key**. La app genera una clave Ed25519. Ponle cualquier nombre, por ejemplo `SSH Gitbox`, y guárdala.
3. En macOS y Linux, apunta `SSH_AUTH_SOCK` al socket del agente (`~/.bitwarden-ssh-agent.sock`; las versiones Flatpak y Snap usan su propia ruta, que muestra la app). En Windows, el agente se queda con la named pipe de OpenSSH; el servicio de Windows **OpenSSH Authentication Agent** debe estar desactivado.
4. Mantén la app de escritorio desbloqueada mientras firmas y compruébalo:

   ```bash
   ssh-add -L    # en Windows: /c/Windows/System32/OpenSSH/ssh-add.exe -L
   ```

### Comprobar la configuración

`sign-release.sh --check` firma un mensaje desechable con la clave de releases a través del agente y lo verifica contra `allowed_signers`. No toca nada en GitHub:

```bash
./scripts/sign-release.sh --check
```

Una comprobación correcta imprime la huella de la clave y `the agent signs with the release key and allowed_signers verifies it`. Si falla, `ssh-add -L` debe listar la misma clave que `pkg/update/release-signing-key.pub`.

## Procedimiento de release

1. Crea el tag y publícalo. CI lo compila todo y crea una release en borrador:

   ```bash
   git tag v2.2.0
   git push origin v2.2.0
   ```

2. Espera a que termine el workflow `CI`. Su último paso imprime el comando que toca ejecutar.
3. Firma y publica:

   ```bash
   ./scripts/sign-release.sh v2.2.0
   ```

   El script rechaza una release que ya está publicada, que no tiene `checksums.sha256`, que ya tiene firma o cuyos checksums no listan exactamente los assets de la release. Decide si la release pasa a ser la "latest" de GitHub con la misma regla que usaba antes CI: solo la línea de versión mayor más alta puede ser latest, así que una release de mantenimiento `v1.x` nunca secuestra el actualizador de las apps v2.

Para ensayar sin publicar, `./scripts/sign-release.sh v2.2.0 --dry-run` firma y verifica, y se detiene antes de subir nada. Si algo falla, la release sigue siendo un borrador oculto hasta que arreglo el problema y vuelvo a ejecutar el script.

## Verificar una release a mano

Cualquiera puede comprobar una release con OpenSSH estándar y el fichero `allowed_signers` de este repositorio:

```bash
gh release download v2.2.0 --repo LuisPalacios/gitbox \
  --pattern checksums.sha256 --pattern checksums.sha256.sig
{ printf 'gitbox v2.2.0\n'; cat checksums.sha256; } \
  | ssh-keygen -Y verify -f allowed_signers -I gitbox-release -n gitbox-release -s checksums.sha256.sig
sha256sum --check --ignore-missing checksums.sha256
```

El primer comando imprime `Good "gitbox-release" signature`, y el segundo confirma los artefactos descargados junto a él.

## Usar tu propia clave en un fork

Un fork que publica sus propias releases necesita su propia clave, porque no puede firmar con la mía:

1. Crea una clave Ed25519 en tu agente SSH, como en los ejemplos anteriores.
2. Copia la línea de clave pública que imprime `ssh-add -L` en `pkg/update/release-signing-key.pub`, seguida del comentario `gitbox-release`:

   ```text
   ssh-ed25519 AAAA... gitbox-release
   ```

3. Sustituye la clave en la única entrada de `allowed_signers`, manteniendo el principal y el namespace:

   ```text
   gitbox-release namespaces="gitbox-release" ssh-ed25519 AAAA...
   ```

4. Ejecuta `go test ./pkg/update/`. `TestReleaseSigningKey_MatchesAllowedSigners` falla si los dos ficheros no coinciden.
5. Ejecuta `./scripts/sign-release.sh --check`.
6. Apunta el actualizador a tu repositorio: el `Repo` por defecto en `pkg/update` nombra este repositorio.

A partir de ahí, los binarios compilados desde el fork solo aceptan releases firmadas con la clave del fork.

## Clave perdida o filtrada

Hay una sola clave de releases y ninguna de respaldo. Si se pierde la clave privada, la app sigue funcionando, pero todas las copias instaladas rechazan todas las releases nuevas, porque nada más puede producir una firma válida. La solución es una clave nueva, una release que incluya la nueva clave pública y una instalación manual única de esa release por parte de cada usuario, tras la cual las actualizaciones automáticas vuelven a funcionar.

Si la clave se filtra, quien la tenga puede firmar actualizaciones que las apps instaladas aceptan. Genero una clave nueva, publico una release con la nueva clave pública y aviso de que los usuarios deben instalarla a mano. Las releases firmadas con la clave antigua dejan de ser de confianza para las apps que llevan la nueva.

## De qué no protege la firma

La firma demuestra que aprobé exactamente el `checksums.sha256` de una release. No demuestra que los artefactos estén libres de código malicioso: si CI estuviera comprometido y compilara un binario manipulado, firmaría su checksum como cualquier otro. La firma cierra el hueco entre "CI lo compiló" y "el actualizador lo instala", donde un token robado para subir assets o un asset sustituido pasarían desapercibidos.

## Transición

Las releases hasta la v2.1.3 no están firmadas, y las apps que salieron con ellas no verifican firmas. Se actualizan a la primera release firmada con normalidad. A partir de esa release, todas las releases deben estar firmadas, o las apps actualizadas las rechazan.
