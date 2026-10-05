# Documentation

[Leer la documentación en español](es/README.md)

## First-time contributors

If you're new to the project, read these in order:

1. [Developer Guide](developer-guide.md) — prerequisites and building from source
2. [Testing](testing.md) — running tests, setting up the test fixture, pre-PR and release checklists
3. [Multiplatform](multiplatform.md) — cross-platform build, ship, and test (optional but recommended)

## User guides

| Doc                           | What's in it                                                             |
| ----------------------------- | ------------------------------------------------------------------------ |
| [GUI Guide](gui-guide.md)     | Desktop app: install, accounts, discovery, mirrors, workspaces, settings |
| [Credentials](credentials.md) | Token, GCM, and SSH setup in detail, troubleshooting                     |

## Developer guides

| Doc                                                       | What's in it                                                           |
| --------------------------------------------------------- | ---------------------------------------------------------------------- |
| [Developer Guide](developer-guide.md)                     | Building from source, git hooks, releases, contributing                |
| [Multiplatform](multiplatform.md)                         | Cross-platform build, ship, and test workflow                          |
| [Testing](testing.md)                                     | Test levels, fixture setup, pre-PR and release checklists              |
| [Worktree workflow](worktree-workflow.md)                 | Parallel issue work: one Claude session per worktree, gated push/merge |
| [Testing Reference](testing-reference.md)                 | Test inventory, harness internals                                      |
| [Architecture](architecture.md)                           | Technical design, component diagram, config file reference             |
| [macOS Signing](macos-signing.md)                         | Code signing and notarization setup for macOS releases                 |
| [Release signing](release-signing.md)                     | How releases are signed, SSH agent setup, using your own key in a fork |
| [Agentic ecosystem directory](agentic-tools-directory.md) | Where the list of auto-detected AI harnesses lives                     |

## Reference

| Doc                                                         | What's in it                                       |
| ----------------------------------------------------------- | -------------------------------------------------- |
| [Config file reference](architecture.md#4-config-format-v3) | Every `gitbox.json` key, folder structure, backups |
| [JSON annotated example](../json/gitbox.jsonc)              | Example of the `gitbox.json` file                  |
| [JSON Schema](../json/gitbox.schema.json)                   | The schema used in the `gitbox.json` file          |
