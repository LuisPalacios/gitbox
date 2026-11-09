# macOS code signing and notarization

Gitbox releases are currently **not** signed or notarized for macOS. The `CI` workflow (`.github/workflows/ci.yml`) has an Apple code-signing and notarization path for the DMGs, but it stays inactive until the Apple secrets below exist in the repository. This page describes that path and what users do with unsigned builds today.

This is separate from the SSH signature over `checksums.sha256` that every release carries for the updater; see [release-signing.md](release-signing.md).

## What the CI path covers

The `dmg-macos-arm64` and `dmg-macos-amd64` jobs run these steps only when `env.APPLE_CERTIFICATE != ''`:

1. **Import signing certificate**: decodes the `.p12` into an ephemeral `build.keychain`.
2. **Codesign app bundle**: signs `GitboxApp.app` with `codesign --force --deep --sign "$APPLE_IDENTITY" --options runtime`.
3. **Build DMG**: `create-dmg` packs the app, `Install Gitbox.command` and `README.txt`. This step always runs.
4. **Notarize DMG**: `xcrun notarytool submit … --wait` uploads the DMG to Apple, then `xcrun stapler staple` attaches the notarization ticket.

Without the secrets, steps 1, 2 and 4 are skipped and the DMG is unsigned. The signing applies only to the app inside the DMG: the `gitbox-macos-arm64.zip` and `gitbox-macos-amd64.zip` archives, which the in-app updater and `bootstrap.sh` download, are never Apple-signed.

## Enabling it

Signing needs an Apple Developer account ($99/year) from <https://developer.apple.com>. Add these secrets under **Settings → Secrets and variables → Actions**:

| Secret                       | Description                                                                                                                             |
| ---------------------------- | --------------------------------------------------------------------------------------------------------------------------------------- |
| `APPLE_CERTIFICATE`          | Base64-encoded `.p12` Developer ID certificate. Export it from Keychain Access, then run `base64 -i cert.p12 \| pbcopy`.                |
| `APPLE_CERTIFICATE_PASSWORD` | Password used when exporting the `.p12` file.                                                                                           |
| `APPLE_IDENTITY`             | Signing identity, for example `Developer ID Application: Your Name (TEAMID)`. List it with `security find-identity -v -p codesigning`.  |
| `APPLE_ID`                   | Apple ID email address used for notarization.                                                                                           |
| `APPLE_TEAM_ID`              | 10-character Team ID from the Apple Developer portal (Membership section).                                                              |
| `APPLE_APP_PASSWORD`         | App-specific password for notarization. Generate it at <https://appleid.apple.com> under Sign-In and Security → App-Specific Passwords. |

`APPLE_CERTIFICATE` is the gate: when it is set, all signing and notarization steps run and need the other secrets too.

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

## Installing unsigned builds

Because releases are unsigned, macOS shows a Gatekeeper warning when a user opens an app from the downloaded DMG. Any of these gets past it:

- Run `bash "/Volumes/gitbox/Install Gitbox.command"` from Terminal. The script bundled in the DMG copies `GitboxApp.app` to `/Applications/` and removes the quarantine attribute.
- Remove the quarantine attribute by hand: `xattr -cr /Applications/GitboxApp.app`.
- Install with `scripts/bootstrap.sh`, which clears the quarantine attribute itself.
- Update from inside the app (the update pill in the footer). The updater downloads the zip itself, so macOS never quarantines it.
