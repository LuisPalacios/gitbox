# Release signing

Every gitbox release carries `checksums.sha256.sig`, an SSH signature over the release's `checksums.sha256`. The updater inside `GitboxApp` refuses to install a release whose signature is missing or doesn't come from the release key compiled into the binary. This page explains how the signing works, how I run it for each release, and how a fork sets it up with its own key.

## Why releases are signed

The updater has always compared each downloaded artifact with its SHA256 in `checksums.sha256`. That proves the download isn't corrupted, but both files come from the same GitHub release, so anyone able to upload release assets could replace an artifact and its checksum together. The signature adds authenticity: only the holder of the private key can produce it, and that key never leaves the maintainer's machine.

## How it works

The flow has three actors: CI, the maintainer, and the updater on every user's machine.

1. I push a version tag. CI builds every artifact, writes `checksums.sha256` and creates the GitHub release as a **draft**. A draft is invisible to users and to the updater.
2. I run `./scripts/sign-release.sh <tag>` on my machine. The script downloads the draft's `checksums.sha256`, checks that it lists exactly the release's assets, and signs this message with `ssh-keygen -Y sign` through my SSH agent:

   ```text
   gitbox <tag>
   <checksums.sha256, byte for byte>
   ```

   The agent asks me to approve the signature. The script then verifies the new signature against `allowed_signers`, uploads it as `checksums.sha256.sig` and publishes the release.
3. When a user's `GitboxApp` finds the new release, it downloads `checksums.sha256` and `checksums.sha256.sig` first and checks the signature: it must use the `gitbox-release` namespace, come from the key compiled into the binary, and cover this exact tag and these exact checksums. Only then does it download the artifact and compare its SHA256. Any failure refuses the update.

The tag inside the message binds a signature to one release, so an old signature can't be replayed onto a new tag. The `gitbox-release` namespace keeps a signature made with the same key for anything else, such as a git commit, from passing as a release signature.

## What lives where

- The **private key** lives only in the maintainer's SSH agent. It is never in the repository, never in CI secrets and never on disk as a file.
- The **public key** lives in `pkg/update/release-signing-key.pub`. The updater embeds it at build time, and `sign-release.sh` passes the same file to `ssh-keygen -Y sign`, so the agent picks the matching private key.
- `allowed_signers` at the repository root publishes the same public key in the format `ssh-keygen -Y verify` reads, for manual verification. A unit test fails if it and `release-signing-key.pub` ever list different keys.
- The **signature** lives in each release as `checksums.sha256.sig`.

## Choosing an SSH agent

Any SSH agent works, as long as it:

- Holds an Ed25519 key (`ssh-ed25519`).
- Speaks the standard SSH agent protocol, so `ssh-add -L` lists the key and `ssh-keygen -Y sign` can use it.
- Is reachable from the shell where I run the script: through `SSH_AUTH_SOCK` on macOS and Linux, and through the Windows OpenSSH named pipe on Windows.

Password managers with a built-in SSH agent are a good fit, because the key syncs with the vault, never exists as a file, and every signature can require an approval. Plain `ssh-agent` with a key file also works.

On Windows, Git Bash's own `ssh-keygen` cannot reach an agent behind the Windows named pipe. `sign-release.sh` therefore uses the native `C:\Windows\System32\OpenSSH\ssh-keygen.exe` when it exists. Set `SSH_KEYGEN` to override the binary on any platform.

### Example: 1Password

1. In the 1Password desktop app, open **Settings → Developer** and turn on **Use the SSH agent**.
2. Create the key: **New Item → SSH Key → Add Private Key → Generate New Key**, type **Ed25519**. Give it any title, for example `SSH Gitbox`, and save it in any vault.
3. Make sure the agent offers it. By default the agent serves keys from the Personal or Private vault. If you keep an `agent.toml` (`%LOCALAPPDATA%\1Password\config\ssh\agent.toml` on Windows, `~/.config/1Password/ssh/agent.toml` on macOS and Linux), add the key by its exact title and restart the agent:

   ```toml
   [[ssh-keys]]
   item = "SSH Gitbox"
   ```

4. On macOS and Linux, point `SSH_AUTH_SOCK` at the agent's socket (`~/Library/Group Containers/2BUA8C4S2C.com.1password/t/agent.sock` on macOS, `~/.1password/agent.sock` on Linux). On Windows, the agent takes over the OpenSSH named pipe; the Windows **OpenSSH Authentication Agent** service must be disabled.
5. Check it, then run the signing check described in [Check the setup](#check-the-setup):

   ```bash
   ssh-add -L    # on Windows: /c/Windows/System32/OpenSSH/ssh-add.exe -L
   ```

### Example: Bitwarden or Vaultwarden

The Bitwarden desktop app includes an SSH agent and works the same way against a Bitwarden or a self-hosted Vaultwarden server (Vaultwarden needs a version that supports SSH key items).

1. In the Bitwarden desktop app, open **Settings** and turn on **Enable SSH agent**. Optionally set it to ask for authorization every time a key is used.
2. Create the key: **New → SSH key**. The app generates an Ed25519 key. Give it any name, for example `SSH Gitbox`, and save it.
3. On macOS and Linux, point `SSH_AUTH_SOCK` at the agent's socket (`~/.bitwarden-ssh-agent.sock`; the Flatpak and Snap builds use their own path, shown in the app). On Windows, the agent takes over the OpenSSH named pipe; the Windows **OpenSSH Authentication Agent** service must be disabled.
4. Keep the desktop app unlocked while signing, then check it:

   ```bash
   ssh-add -L    # on Windows: /c/Windows/System32/OpenSSH/ssh-add.exe -L
   ```

### Check the setup

`sign-release.sh --check` signs a throwaway message with the release key through the agent and verifies it against `allowed_signers`. It touches nothing on GitHub:

```bash
./scripts/sign-release.sh --check
```

A passing check prints the key's fingerprint and `the agent signs with the release key and allowed_signers verifies it`. If it fails, `ssh-add -L` must list the same key as `pkg/update/release-signing-key.pub`.

## Release procedure

1. Tag and push. CI builds everything and creates a draft release:

   ```bash
   git tag v2.2.0
   git push origin v2.2.0
   ```

2. Wait for the `CI` workflow to finish. Its last step prints the command to run next.
3. Sign and publish:

   ```bash
   ./scripts/sign-release.sh v2.2.0
   ```

   The script refuses a release that is already published, has no `checksums.sha256`, already has a signature, or whose checksums don't list exactly the release's assets. It decides whether the release becomes GitHub's "latest" with the same rule CI used before: only the highest major version line may be latest, so a `v1.x` maintenance release never hijacks the updater of v2 apps.

To rehearse without publishing, `./scripts/sign-release.sh v2.2.0 --dry-run` signs and verifies, then stops before uploading. If anything fails, the release stays a hidden draft until I fix the problem and run the script again.

To test the in-app update right after publishing, I delete `~/.config/gitbox/.update-check` before opening an installed copy of the previous release. The app checks for updates at most once a day and records the last check in that file, so without deleting it the new release may not show up until the next day. The amber version pill in the footer then offers the new release, and the update only installs if the signature and checksum verify.

## Verify a release by hand

Anyone can check a release with stock OpenSSH and the `allowed_signers` file from this repository:

```bash
gh release download v2.2.0 --repo LuisPalacios/gitbox \
  --pattern checksums.sha256 --pattern checksums.sha256.sig
{ printf 'gitbox v2.2.0\n'; cat checksums.sha256; } \
  | ssh-keygen -Y verify -f allowed_signers -I gitbox-release -n gitbox-release -s checksums.sha256.sig
sha256sum --check --ignore-missing checksums.sha256
```

The first command prints `Good "gitbox-release" signature`, and the second confirms the artifacts downloaded next to it.

## Using your own key in a fork

A fork that publishes its own releases needs its own key, because it can't sign with mine:

1. Create an Ed25519 key in your SSH agent, as in the examples above.
2. Copy the public key line that `ssh-add -L` prints into `pkg/update/release-signing-key.pub`, followed by the comment `gitbox-release`:

   ```text
   ssh-ed25519 AAAA... gitbox-release
   ```

3. Replace the key in the single entry of `allowed_signers`, keeping the principal and the namespace:

   ```text
   gitbox-release namespaces="gitbox-release" ssh-ed25519 AAAA...
   ```

4. Run `go test ./pkg/update/`. `TestReleaseSigningKey_MatchesAllowedSigners` fails if the two files disagree.
5. Run `./scripts/sign-release.sh --check`.
6. Point the updater at your repository: the default `Repo` in `pkg/update` names this repository.

From then on, binaries built from the fork only accept releases signed with the fork's key.

## Lost or leaked key

There is one release key and no backup key. If the private key is lost, the app keeps working, but every installed copy refuses every new release, because nothing else can produce a valid signature. The fix is a new key, a release that ships the new public key, and a one-time manual install of that release by each user, after which automatic updates resume.

If the key leaks, whoever holds it can sign updates that installed apps accept. I generate a new key, ship a release with the new public key, and announce that users must install it manually. Releases signed with the old key stop being trusted by apps that carry the new one.

## What signing doesn't protect against

The signature proves that I approved the exact `checksums.sha256` of a release. It doesn't prove the artifacts are free of malicious code: if CI itself were compromised and built a tampered binary, I would sign its checksum like any other. Signing closes the gap between "CI built it" and "the updater installs it", where a stolen release-upload token or a replaced asset would otherwise go unnoticed.

## Transition

Releases up to v2.1.3 are unsigned, and the apps that shipped with them don't verify signatures. They update to the first signing release normally. From that release on, every release must be signed, or updated apps refuse it.
