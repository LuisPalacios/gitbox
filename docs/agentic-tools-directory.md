# The Agentic Ecosystem Directory

The authoritative list of known AI harnesses, orchestrators, CLIs, and IDEs lives at [`pkg/harness/tools-directory.md`](../pkg/harness/tools-directory.md).

That file is embedded into the GUI binary via `//go:embed` and parsed at startup: rows whose `Category` is `Agentic CLI`, `AI Harness`, `Headless Harness`, `Agentic IDE`, or `Agentic IDE / CLI`, and whose `Executable / CLI Command` cell is a single backticked identifier (e.g. `` `claude` ``, `` `aider` ``, `` `cursor` ``), are auto-detected on `PATH` and added to the "Open in AI harness" menu entries in the GUI.

To add or remove a detected harness, edit [`pkg/harness/tools-directory.md`](../pkg/harness/tools-directory.md) rather than adding a new file here — keeping a single source of truth avoids drift between user-facing docs and the embedded list the binary actually parses.

See [gui-guide.md → AI harness actions](gui-guide.md#ai-harness-actions) for how the menu uses this list at runtime.
