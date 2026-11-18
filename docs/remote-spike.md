# Remote mode spike: decision record

> **Noncanonical record.** This is a dated engineering record for issue #234, written in English only and frozen once it merges. It has no `docs/es/` counterpart. The canonical design lives in the remote-mode umbrella issue #233 and its sub-issues.

## Verdict

Every abandon criterion passes, so the remote-mode design goes ahead unchanged in its fundamentals: JSON-RPC 2.0 over the stdio of a system `ssh` session, a magic-byte handshake, a pure-Go server, and six embedded binaries.

The spike also found six things the issues got wrong or left out. They are listed under [Corrections to the issues](#corrections-to-the-issues), and the affected issues get updated to match.

## Setup

- **Client:** macOS 27 on arm64, OpenSSH 10.5p1, Go 1.26.11.
- **Linux remote:** a Debian 13 amd64 VM on the same workstation (OpenSSH 10.0p2, git 2.47.3). Its latency numbers prove the mechanism, not the network.
- **Windows remote:** a Windows 11 amd64 machine on the LAN with Win32-OpenSSH, Git for Windows 2.56 and Git Bash configured as the OpenSSH `DefaultShell`. It is the realistic network data point.
- **No macOS remote** was available. P8 covers that case.
- **Spike code:** a throwaway client and server, about 600 lines of Go, at commit `9b8324e` on the branch `spike/234-remote-p0`. That branch is published for reference and never merged. The server links the real `pkg/` libraries, so its size is representative of the future `gitbox-server`.

Each run uploads the server over the SSH transport, verifies its SHA-256, starts it, and performs the handshake: four magic bytes `00 FE 67 62`, then a framed hello. After that it makes 1000 `ping` calls and 50 `status` calls, each returning 300 synthetic repositories shaped exactly like the GUI's `StatusResult` DTO. The baseline is a plain `cat` echo inside an established SSH channel, so the RPC overhead is measured against the transport alone.

## Results against the abandon criteria

| Criterion (from #234) | Threshold | Measured | Result |
| --- | --- | --- | --- |
| `ping` p50 above channel RTT | ≤ 30 ms | Linux VM 0.348 ms vs 0.356 ms; Windows LAN 0.384 ms vs 0.387 ms (overhead within noise) | pass |
| `status` for 300 repos, size | ≤ 1 MB | 76.7 KB | pass |
| `status` for 300 repos, client decode | ≤ 500 ms | 0.73 ms p50 | pass |
| Handshake with 64 KiB of banner | survives | survives a real noisy `~/.bashrc` (65,536 bytes skipped) | pass |
| Cold start: upload, verify, start, first reply | ≤ 10 s | Linux VM 0.10 s; Windows LAN 1.59 s | pass |
| Six compressed binaries | ≤ +30 MB | +17.5 MB | pass |
| Build time for the six binaries | ≤ +60 s | 20 s on the workstation, warm cache | pass |

## Measurements

| Run | Target | SSH connect p50 | Channel RTT p50 | `ping` p50 / p95 | `status` p50 / p95 | Upload | Verify | Cold start | Exit after stdin closes |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| Linux VM, plain | linux/amd64 | 8.1 ms | 0.356 ms | 0.348 / 0.396 ms | 1.44 / 1.60 ms | 68 ms | 15 ms | 104 ms | 1.3 ms |
| Linux VM, noisy `.bashrc` | linux/amd64 | 10.0 ms | n/a | 0.352 / 0.471 ms | 1.37 / 1.59 ms | 70 ms | 17 ms | 111 ms | 1.5 ms |
| Linux VM, `fish` login shell | linux/amd64 | 10.3 ms | 0.349 ms | 0.352 / 0.464 ms | 1.37 / 1.42 ms | 70 ms | 15 ms | 105 ms | 1.4 ms |
| Linux VM, Alpine (musl) chroot | linux/amd64 | 7.3 ms | 0.357 ms | 0.355 / 0.402 ms | 1.35 / 1.47 ms | 68 ms | 15 ms | 122 ms | 1.8 ms |
| Windows LAN, Git Bash shell | windows/amd64 | 51.6 ms | 0.387 ms | 0.384 / 0.434 ms | 1.24 / 1.54 ms | 159 ms | 289 ms | 1,586 ms | 11 ms |

Notes:

- **Status payload.** One `status` response for 300 repositories is 76,710 bytes. That is about 256 bytes per repository with today's field names, so a full refresh every few seconds costs nothing worth optimising. Incremental status events are not needed for P4.
- **Connection reuse.** The Linux VM's 8 ms connect time comes from an existing `ControlMaster` in the client's SSH config. The Windows number, 52 ms, is a full handshake.
- **Windows cold start.** Upload and verify account for about 450 ms of the 1.6 s. The rest is the first start of a brand-new `.exe`, most likely the antivirus scanning it. Later starts skip that cost. P9 should measure it with Defender on and off.
- **Server exit.** The server exits as soon as stdin closes: 1–2 ms on Linux and 11 ms on Windows. That confirms the "die when stdin closes" model.
- **musl and Alpine.** The static `CGO_ENABLED=0` binary runs unchanged inside an Alpine 3.22 root filesystem with no glibc present.
- **Banner overflow.** 2 MiB of banner fails cleanly with `banner_overflow` after 1 MiB, as designed.

## Embedding cost

Each server binary was built with `CGO_ENABLED=0 -trimpath -ldflags "-s -w"` and compressed with `compress/gzip` at `BestCompression`.

| Target | Raw | Gzip | Build |
| --- | --- | --- | --- |
| linux/amd64 | 7.13 MB | 3.01 MB | 2.9 s |
| linux/arm64 | 6.62 MB | 2.73 MB | 2.9 s |
| darwin/amd64 | 7.25 MB | 3.04 MB | 2.8 s |
| darwin/arm64 | 6.71 MB | 2.82 MB | 2.9 s |
| windows/amd64 | 7.46 MB | 3.12 MB | 2.9 s |
| windows/arm64 | 6.76 MB | 2.80 MB | 2.9 s |
| **Total** | **41.9 MB** | **17.5 MB** | **20 s wall** |

Today's macOS arm64 GitboxApp executable is 11.7 MB. Embedding all six targets would make it about 29 MB, 2.5 times its current size, and still well under the threshold. Gzip at the highest level adds about 0.45 s per target.

## Values fixed for P3a and P3b

- **Size ceiling.** The main GUI executable on each platform may grow by at most **25 MB** over its size before embedding. That leaves about 40 % headroom over the measured 17.5 MB, so a regression shows up early. P3a records the baseline for each platform in `scripts/size-ceilings.txt`.
- **Build-time ceiling.** The server build may add at most **60 s** to a CI GUI job. CI runners are slower than the workstation, so P3a confirms the real figure in its first CI run.
- **Default target set for dev builds:** `linux/amd64` and `linux/arm64` (5.7 MB compressed). Release builds embed all six.
- **Banner limit:** 1 MiB before the marker or the magic bytes, enforced by both the probe and the handshake.
- **Upload command, by remote shell family:**
  - POSIX shell (Linux, macOS, or Windows with Git Bash as `DefaultShell`):

    ```text
    sh -c 'umask 077 && mkdir -p <dir> && cat > <dir>/.gitbox-server.<random>.tmp'
    ```

    The binary goes on stdin. Verification runs `sh -c 'printf "\0GBX\0"; sha256sum <tmp>'`, falling back to `shasum -a 256`, and the output is parsed after the marker.
  - Windows with `cmd` or PowerShell as `DefaultShell`: `scp` to a temporary name through the SFTP subsystem, which Win32-OpenSSH enables by default. Verification runs `Get-FileHash` through `powershell -NoProfile -NonInteractive -EncodedCommand <base64>` with `$ProgressPreference = 'SilentlyContinue'`.

## Corrections to the issues

1. **Every remote command whose output I parse needs a marker, not just the probe** (#239). A login shell that prints a banner also contaminates the output of `sha256sum`. With a real noisy `.bashrc`, 65,541 bytes arrived before the hash, so the hash read naively was wrong. Verification now prints `\0GBX\0` first and parses only what comes after it.
2. **A Windows host can answer the POSIX probe, and `cmd /c` must never be used from the client** (#239, #246).
   - With Git Bash as `DefaultShell`, `sh -c 'uname -sm'` answers `MINGW64_NT-10.0-26300 x86_64`.
   - Git Bash rewrites arguments that look like paths (MSYS path conversion), so `cmd /c echo …` reaches Windows as `cmd C:/ echo …` and starts an interactive `cmd` instead of running the command.
   - The probe has to treat `MINGW*`, `MSYS*` and `CYGWIN*` as Windows reached through a POSIX shell. Every Windows-native step goes through `powershell -EncodedCommand`, which has no slash arguments and needs no quoting under cmd, PowerShell or bash.
3. **Uploading through PowerShell's stdin does not work, so Windows hosts without a POSIX shell use `scp`** (#233, #246).
   - `[Console]::OpenStandardInput().CopyTo(…)` never saw end-of-file over SSH. It hung with and without `-InputFormat None`, and with progress output silenced.
   - `cat >` through Git Bash (1 MiB in 0.42 s) and `scp` (1 MiB in 0.1 s) both worked, with matching hashes.
   - This reverses the umbrella's rejection of `scp`, but only for Windows hosts whose `DefaultShell` is cmd or PowerShell. Every other host keeps the dependency-free `cat` upload.
4. **PowerShell writes progress records as CLIXML on stderr in SSH sessions** (#246). The first call of a session printed `#< CLIXML … Preparing modules for first use` on stderr. Every PowerShell step sets `$ProgressPreference = 'SilentlyContinue'`, and the connection log filters CLIXML.
5. **A dropped SSH session leaves Windows helper processes running** (#246). Three `powershell.exe` processes blocked on stdin outlived their sessions and had to be killed by hand. The Go server itself exits within 11 ms of stdin closing. The rule is that no helper started on a Windows remote may read stdin, except the server, and the server cleans up such leftovers on start.
6. **Whether GCM works on a remote depends on the host, not only on the OS** (#233, #242).
   - **Windows.** In an SSH session, `git credential fill` fails while validating the store, with the literal message `fatal: Unable to persist credentials with the 'wincredman' credential store.` (from `CredentialStore.ValidateWindowsCredentialManager`).
   - **Linux VM.** systemd-logind gave the SSH login a session D-Bus, and `gnome-keyring-daemon` was already running with an unlocked collection. In that state, GCM 3.0.2 stored, read back and erased a dummy credential through `secretservice` without any error.
   - **Headless Linux.** A real server without a desktop has no unlocked collection, and GCM fails there. That is documented by GCM upstream (git-ecosystem/git-credential-manager#2244) but not reproduced here.

   GCM stays out of the MVP because it is unreliable. The unsupported message names the store that failed, instead of claiming that GCM never works on a remote.

## Not measured

- **A macOS remote.** No host was available. P8 owns it.
- **Windows with `cmd` or PowerShell as `DefaultShell`.** The spike did not change the host's `HKLM\SOFTWARE\OpenSSH` key. It ran the PowerShell and `scp` steps explicitly instead. P9 tests both shells on a CI runner.
- **WAN latency.** The overhead above the channel RTT is within noise on both hosts. Over a slower link the RPC adds the same small constant on top of the link's own RTT, so I don't expect a different verdict.
- **The GCM error text on a headless Linux host.** It is quoted from upstream, not reproduced.

## Reproducing

The spike branch is published but never merged. With that branch checked out in its own worktree:

```bash
go run ./spike/remote/client -size-report -baseline <path to an existing GitboxApp executable>
go run ./spike/remote/client -host <alias> -n 1000 -status 50 -gcm
go run ./spike/remote/client -host <alias> -banner 65536
go run ./spike/remote/client -host <alias> -noexec <a writable noexec mount>
```

Each command prints a JSON summary. On a `noexec` mount the binary fails with exit code 126 and `Permission denied`, which is the signature P3b uses to report `noexec`.
