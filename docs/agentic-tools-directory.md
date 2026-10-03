# The Agentic Ecosystem Directory

The authoritative list of known AI harnesses, orchestrators, CLIs, and IDEs lives at [`pkg/harness/tools-directory.md`](../pkg/harness/tools-directory.md).

That file is embedded into the GUI binary via `//go:embed` and parsed at startup: rows whose `Category` is `Agentic CLI`, `AI Harness`, `Headless Harness`, `Agentic IDE`, or `Agentic IDE / CLI`, and whose `Executable / CLI Command` cell holds one or more backticked identifiers (e.g. `` `claude` ``, `` `aider` ``, or "`` `agent` `` or `` `cursor-agent` ``" for a renamed CLI), are probed on the host and added to the "Open in AI harness" menu entries in the GUI.

Two columns drive detection beyond the binary name:

- `Executable / CLI Command` may list several names. The first is the primary binary; the rest are alternates probed in order, so a tool that was renamed, or that ships under a different name on Windows, still resolves.
- `Well-known locations` lists per-tool install directories that are not on a typical PATH, each as its own backticked token. `~`, `$VAR` and `%VAR%` are expanded when the probe runs. Use `*N/A*` for tools that live on PATH or in `~/.local/bin`, which gitbox already probes on every OS alongside the Homebrew prefixes on macOS.

Detection runs in the background (shortly after the GUI launches, every ten minutes, and on window focus). A detected tool lands in `global.ai_harnesses` with `source: "detected"`; when its binary later disappears the entry is flagged `missing: true` and hidden, never deleted, so a reinstall restores it with the user's `args` intact.

To add or remove a detected harness, edit [`pkg/harness/tools-directory.md`](../pkg/harness/tools-directory.md) rather than adding a new file here — keeping a single source of truth avoids drift between user-facing docs and the embedded list the binary actually parses.

See [gui-guide.md → AI harness actions](gui-guide.md#ai-harness-actions) for how the menu uses this list at runtime.
