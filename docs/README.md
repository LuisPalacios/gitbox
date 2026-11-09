# Documentation

[Leer la documentación en español](es/README.md)

## For users

| Doc                           | What's in it                                                                  |
| ----------------------------- | ----------------------------------------------------------------------------- |
| [GUI guide](gui-guide.md)     | Install, first launch, accounts, sync, actions, mirrors, containers, settings |
| [Credentials](credentials.md) | GCM, token and SSH setup, token scopes, troubleshooting                       |

## For contributors

Start with the developer guide, then testing. Multiplatform is optional but recommended.

| Doc                                                   | What's in it                                                               |
| ----------------------------------------------------- | -------------------------------------------------------------------------- |
| [Developer guide](developer-guide.md)                 | Prerequisites, building, health check, extending the code, release process |
| [Testing](testing.md)                                 | Test levels, the test fixture, pre-PR and release checklists               |
| [Testing reference](testing-reference.md)             | Test inventory by package and harness internals                            |
| [Multiplatform](multiplatform.md)                     | Building, shipping and smoke-testing on remote hosts over SSH              |
| [Worktree workflow](worktree-workflow.md)             | Parallel issue work with one agent session per worktree                    |
| [Architecture](architecture.md)                       | Design, package responsibilities, config file reference                    |
| [Release signing](release-signing.md)                 | How releases are signed and verified, agent setup, forks                   |
| [macOS signing](macos-signing.md)                     | Optional Apple code signing and notarization in CI                         |
| [Agentic tools directory](agentic-tools-directory.md) | How AI harness detection reads its catalog                                 |

## Reference

| Doc                                                         | What's in it                                    |
| ----------------------------------------------------------- | ----------------------------------------------- |
| [Config file reference](architecture.md#4-config-format-v5) | Every `gitbox.json` key, folder layout, backups |
| [Annotated example](../json/gitbox.jsonc)                   | A complete `gitbox.json` with comments          |
| [JSON Schema](../json/gitbox.schema.json)                   | Schema for editor validation and autocompletion |
