<p align="center">
  <img src="assets/logo.svg" width="128" alt="gitbox">
</p>

<h1 align="center">Gitbox</h1>

<p align="center">
  <a href="https://github.com/LuisPalacios/gitbox/actions/workflows/ci.yml">
    <img src="https://github.com/LuisPalacios/gitbox/actions/workflows/ci.yml/badge.svg" alt="CI" />
  </a>
</p>

<p align="center">
  <strong>Accounts & clones — nothing else.</strong><br>
  Discover, clone, and organise Git repositories across multiple accounts, providers, and devices.<br>
  <em>gitbox never adds, commits, pushes, or modifies your working trees.</em>
</p>

---

This is for you if you manage multiple clones from different servers and accounts, mixing different credential types (GCM, SSH, tokens), and work from various devices, desktops or headless servers.

Gitbox establishes a bit of order, with just one file to manage **account setup, repo discovery, and cloning** — that's it. Your working trees are yours; gitbox won't touch them.

<br>

<p align="center">
  <img src="assets/screenshot.png" alt="Gitbox" width="600" />
</p>

<p align="center">
Supports GitHub, GitLab, Forgejo, etc. — on Windows, macOS, and Linux.
</p>

## Download

Grab the latest binaries from the [Releases](https://github.com/LuisPalacios/gitbox/releases) page.

The app is not signed, you need to do the following once per download:

- **macOS:** After extracting, move it to *Applications*. From Terminal `xattr -cr /Applications/Gitbox.app` and `xattr -cr /path/to/gitboxcmd && chmod +x /path/to/gitboxcmd`.

- **Windows SmartScreen:** After extracting, move executables to any folder. Launch `Gitbox.exe`, it will show "Windows protected your PC" dialog. Click **More info** → **Run anyway**.

## Documentation

See the full [documentation index](docs/README.md) for all guides, reference, and technical docs.

## License

[MIT](LICENSE)
