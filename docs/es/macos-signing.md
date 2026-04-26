# Firma y notarización de código en macOS

El workflow de CI incluye pasos de firma de código y notarización para DMGs de macOS. Estos pasos permanecen inactivos hasta que se añaden los secretos necesarios al repositorio de GitHub.

## Requisitos previos

Hace falta una cuenta de Apple Developer ($99/año). Regístrate en <https://developer.apple.com>.

## Secretos de GitHub necesarios

Añade estos secretos a la configuración del repositorio (Settings > Secrets and variables > Actions):

| Secreto                      | Descripción                                                                                                                                           |
| ---------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------- |
| `APPLE_CERTIFICATE`          | Certificado Developer ID `.p12` codificado en Base64. Expórtalo desde Keychain Access y ejecuta `base64 -i cert.p12 \| pbcopy`.                       |
| `APPLE_CERTIFICATE_PASSWORD` | Contraseña usada al exportar el archivo `.p12`.                                                                                                       |
| `APPLE_IDENTITY`             | Cadena de identidad de firma, por ejemplo `Developer ID Application: Your Name (TEAMID)`. Encuéntrala con `security find-identity -v -p codesigning`. |
| `APPLE_ID`                   | Dirección de email de Apple ID usada para la notarización.                                                                                            |
| `APPLE_TEAM_ID`              | Team ID de 10 caracteres del portal de Apple Developer (sección Membership).                                                                          |
| `APPLE_APP_PASSWORD`         | Contraseña específica de app para notarización. Genérala en <https://appleid.apple.com> bajo Sign-In and Security > App-Specific Passwords.           |

## Cómo funciona

Cuando `APPLE_CERTIFICATE` está presente en el entorno de CI:

1. El certificado se importa en un keychain efímero de macOS
2. `codesign` firma `GitboxApp.app` y el binario CLI `gitbox` con el Developer ID
3. `create-dmg` construye el DMG
4. `xcrun notarytool submit` sube el DMG a Apple para notarización
5. `xcrun stapler staple` adjunta el ticket de notarización al DMG

Cuando faltan los secretos, los pasos de firma se omiten silenciosamente y el DMG se produce sin firmar.

## Probar localmente

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

## DMGs sin firmar

Hasta que la firma esté configurada, los usuarios de macOS verán una advertencia de Gatekeeper al abrir la app. Pueden:

- Ejecutar `bash "/Volumes/gitbox/Install Gitbox.command"` desde Terminal — el script de instalación incluido copia los binarios y elimina automáticamente los flags de cuarentena
- Usar `xattr -cr GitboxApp.app` y `xattr -cr gitbox` para eliminar manualmente el atributo de cuarentena
- Usar el script `bootstrap.sh`, que gestiona esto automáticamente
- Usar `gitbox update` desde la CLI, que reemplaza binarios sin comprobaciones de Gatekeeper
