# Firma y notarización de código en macOS

Las releases de Gitbox **no** están firmadas ni notarizadas para macOS por ahora. `scripts/release-macos.sh`, que construye los assets de macOS en mi Mac, tiene una ruta de firma de código y notarización de Apple para los DMGs, pero permanece inactiva hasta que se definen las variables de entorno que se describen más abajo. Esta página describe esa ruta y lo que hacen hoy los usuarios con los builds sin firmar.

Esto es distinto de la firma SSH sobre `checksums.sha256` que cada release incluye para el actualizador; consulta [release-signing.md](release-signing.md).

El bootstrap verifica la firma SSH de la versión vinculada a su etiqueta y el SHA-256 del archivo antes de extraerlo. Después conserva la cuarentena de una app con una firma válida de un editor de Apple, incluidas las apps notarizadas, y rechaza las firmas no válidas. Para las compilaciones sin firma o con firma ad hoc, explica y elimina la cuarentena. Si falta la firma de la versión, detiene la instalación, incluidas las versiones antiguas 1.x.

## Qué cubre la ruta de firma

Para cada arquitectura, `release-macos.sh` ejecuta estos pasos:

1. **Codesign app bundle**, solo cuando `APPLE_IDENTITY` está definida: firma `GitboxApp.app` con `codesign --force --deep --sign "$APPLE_IDENTITY" --options runtime`, usando el certificado de mi llavero de inicio de sesión.
2. **Build DMG**: `create-dmg` empaqueta la app, `Install Gitbox.command` y `README.txt`. Este paso se ejecuta siempre.
3. **Notarize DMG**, solo cuando `NOTARY_PROFILE` está definida: `xcrun notarytool submit … --keychain-profile "$NOTARY_PROFILE" --wait` sube el DMG a Apple y después `xcrun stapler staple` adjunta el ticket de notarización.

Sin las variables, los pasos 1 y 3 se omiten y el DMG queda sin firmar. El certificado y la contraseña de notarización se quedan en mi llavero y nunca llegan a GitHub. La firma solo se aplica a la app dentro del DMG: los archivos `gitbox-macos-arm64.zip` y `gitbox-macos-amd64.zip`, que descargan el actualizador integrado y `bootstrap.sh`, nunca llevan firma de Apple.

## Activarla

La firma necesita una cuenta de Apple Developer ($99/año) de <https://developer.apple.com>. Importo el certificado Developer ID en mi llavero de inicio de sesión, guardo una vez las credenciales de notarización y exporto las dos variables antes de ejecutar `release-macos.sh`:

```bash
security find-identity -v -p codesigning   # lista la identidad
xcrun notarytool store-credentials gitbox-notary \
  --apple-id "you@example.com" --team-id "ABCDE12345" --password "app-specific-password"
export APPLE_IDENTITY="Developer ID Application: Your Name (TEAMID)"
export NOTARY_PROFILE=gitbox-notary
./scripts/release-macos.sh v2.2.0
```

Los valores salen de estas fuentes:

| Valor                        | Descripción                                                                                                                                  |
| ---------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------- |
| `APPLE_CERTIFICATE`          | Certificado Developer ID `.p12` codificado en Base64. Expórtalo desde Keychain Access y después ejecuta `base64 -i cert.p12 \| pbcopy`.      |
| `APPLE_CERTIFICATE_PASSWORD` | Contraseña usada al exportar el archivo `.p12`.                                                                                              |
| `APPLE_IDENTITY`             | Identidad de firma, por ejemplo `Developer ID Application: Your Name (TEAMID)`. Lístala con `security find-identity -v -p codesigning`.      |
| `APPLE_ID`                   | Dirección de email de Apple ID usada para la notarización.                                                                                   |
| `APPLE_TEAM_ID`              | Team ID de 10 caracteres del portal de Apple Developer (sección Membership).                                                                 |
| `APPLE_APP_PASSWORD`         | Contraseña específica de app para la notarización. Genérala en <https://appleid.apple.com> en Sign-In and Security → App-Specific Passwords. |

`APPLE_CERTIFICATE` y `APPLE_CERTIFICATE_PASSWORD` solo hacen falta para mover el certificado entre máquinas; `release-macos.sh` lee la identidad del llavero.

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
- Instalar con `scripts/bootstrap.sh`, que verifica la versión y elimina la cuarentena solo en compilaciones sin firma o con firma ad hoc.
- Actualizar desde la propia app (la píldora de actualización del pie). El actualizador descarga el zip por sí mismo, así que macOS nunca lo pone en cuarentena.
