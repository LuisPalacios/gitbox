# macOS code signing and notarization

The CI workflow includes code signing and notarization steps for macOS DMGs. These steps are inactive until the required secrets are added to the GitHub repository.

## Prerequisites

An Apple Developer account ($99/year) is required. Sign up at https://developer.apple.com.

## Required GitHub secrets

Add these secrets to the repository settings (Settings > Secrets and variables > Actions):

| Secret | Description |
| --- | --- |
| `APPLE_CERTIFICATE` | Base64-encoded `.p12` Developer ID certificate. Export from Keychain Access, then run `base64 -i cert.p12 \| pbcopy`. |
| `APPLE_CERTIFICATE_PASSWORD` | Password used when exporting the `.p12` file. |
| `APPLE_IDENTITY` | Signing identity string, e.g. `Developer ID Application: Your Name (TEAMID)`. Find with `security find-identity -v -p codesigning`. |
| `APPLE_ID` | Apple ID email address used for notarization. |
| `APPLE_TEAM_ID` | 10-character Team ID from the Apple Developer portal (Membership section). |
| `APPLE_APP_PASSWORD` | App-specific password for notarization. Generate at https://appleid.apple.com under Sign-In and Security > App-Specific Passwords. |

## How it works

When `APPLE_CERTIFICATE` is present in the CI environment:

1. The certificate is imported into an ephemeral macOS keychain
2. `codesign` signs `GitboxApp.app` and the `gitbox` CLI binary with the Developer ID
3. `create-dmg` builds the DMG
4. `xcrun notarytool submit` uploads the DMG to Apple for notarization
5. `xcrun stapler staple` attaches the notarization ticket to the DMG

When secrets are absent, the signing steps are silently skipped and the DMG is produced unsigned.

## Testing locally

```bash
# Sign the app
codesign --force --deep --sign "Developer ID Application: Your Name (TEAMID)" \
  --options runtime GitboxApp.app

# Verify
codesign --verify --deep --strict GitboxApp.app
spctl --assess --type execute GitboxApp.app

# Notarize a DMG
xcrun notarytool submit gitbox-macos-arm64.dmg \
  --apple-id "you@example.com" \
  --team-id "ABCDE12345" \
  --password "app-specific-password" \
  --wait

# Staple the ticket
xcrun stapler staple gitbox-macos-arm64.dmg
```

## Unsigned DMGs

Until signing is configured, macOS users will see a Gatekeeper warning when opening the app. They can:

- Right-click "Install Gitbox" inside the DMG → Open — it copies binaries and removes quarantine flags automatically (or run `bash "/Volumes/gitbox/Install Gitbox.command"` from Terminal)
- Use `xattr -cr GitboxApp.app` and `xattr -cr gitbox` to remove the quarantine attribute manually
- Use the `bootstrap.sh` script which handles this automatically
- Use `gitbox update` from the CLI which replaces binaries without Gatekeeper checks
