# Firma y notarización de código en macOS

Las releases de Gitbox **no** están firmadas ni notarizadas para macOS por ahora. El workflow `CI` (`.github/workflows/ci.yml`) tiene una ruta de firma de código y notarización de Apple para los DMGs, pero permanece inactiva hasta que existan en el repositorio los secretos de Apple que se describen más abajo. Esta página describe esa ruta y lo que hacen hoy los usuarios con los builds sin firmar.

Esto es distinto de la firma SSH sobre `checksums.sha256` que cada release incluye para el actualizador; consulta [release-signing.md](release-signing.md).

## Qué cubre la ruta de CI

Los jobs `dmg-macos-arm64` y `dmg-macos-amd64` ejecutan estos pasos solo cuando el secreto `APPLE_CERTIFICATE` está definido (el indicador `APPLE_SIGNING` del job):

1. **Import signing certificate**: decodifica el `.p12` en un `build.keychain` efímero.
2. **Codesign app bundle**: firma `GitboxApp.app` con `codesign --force --deep --sign "$APPLE_IDENTITY" --options runtime`.
3. **Build DMG**: `create-dmg` empaqueta la app, `Install Gitbox.command` y `README.txt`. Este paso se ejecuta siempre.
4. **Notarize DMG**: `xcrun notarytool submit … --wait` sube el DMG a Apple y después `xcrun stapler staple` adjunta el ticket de notarización.

Sin los secretos, los pasos 1, 2 y 4 se omiten y el DMG queda sin firmar. La firma solo se aplica a la app dentro del DMG: los archivos `gitbox-macos-arm64.zip` y `gitbox-macos-amd64.zip`, que descargan el actualizador integrado y `bootstrap.sh`, nunca llevan firma de Apple.

## Activarla

La firma necesita una cuenta de Apple Developer ($99/año) de <https://developer.apple.com>. Añade estos secretos en **Settings → Secrets and variables → Actions**:

| Secreto                      | Descripción                                                                                                                                  |
| ---------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------- |
| `APPLE_CERTIFICATE`          | Certificado Developer ID `.p12` codificado en Base64. Expórtalo desde Keychain Access y después ejecuta `base64 -i cert.p12 \| pbcopy`.      |
| `APPLE_CERTIFICATE_PASSWORD` | Contraseña usada al exportar el archivo `.p12`.                                                                                              |
| `APPLE_IDENTITY`             | Identidad de firma, por ejemplo `Developer ID Application: Your Name (TEAMID)`. Lístala con `security find-identity -v -p codesigning`.      |
| `APPLE_ID`                   | Dirección de email de Apple ID usada para la notarización.                                                                                   |
| `APPLE_TEAM_ID`              | Team ID de 10 caracteres del portal de Apple Developer (sección Membership).                                                                 |
| `APPLE_APP_PASSWORD`         | Contraseña específica de app para la notarización. Genérala en <https://appleid.apple.com> en Sign-In and Security → App-Specific Passwords. |

`APPLE_CERTIFICATE` es la puerta: cuando está definido, se ejecutan todos los pasos de firma y notarización, que también necesitan los demás secretos.

## Probar en local

```bash
# Firmar la app
codesign --force --deep --sign "Developer ID Application: Your Name (TEAMID)" \
  --options runtime GitboxApp.app

# Verificar
codesign --verify --deep --strict GitboxApp.app
spctl --assess --type execute GitboxApp.app

# Notarizar un DMG
xcrun notarytool submit gitbox-macos-arm64.dmg \
  --apple-id "you@example.com" \
  --team-id "ABCDE12345" \
  --password "app-specific-password" \
  --wait

# Adjuntar el ticket
xcrun stapler staple gitbox-macos-arm64.dmg
```

## Instalar builds sin firmar

Como las releases no están firmadas, macOS muestra un aviso de Gatekeeper cuando un usuario abre una app desde el DMG descargado. Cualquiera de estas opciones lo evita:

- Ejecutar `bash "/Volumes/gitbox/Install Gitbox.command"` desde Terminal. El script incluido en el DMG copia `GitboxApp.app` a `/Applications/` y elimina el atributo de cuarentena.
- Eliminar el atributo de cuarentena a mano: `xattr -cr /Applications/GitboxApp.app`.
- Instalar con `scripts/bootstrap.sh`, que elimina el atributo de cuarentena por sí mismo.
- Actualizar desde la propia app (la píldora de actualización del pie). El actualizador descarga el zip por sí mismo, así que macOS nunca lo pone en cuarentena.
