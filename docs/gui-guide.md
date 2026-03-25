# GUI Guide

> **Status: Planned** — The gitbox GUI is not yet implemented. This page describes the intended design.

The desktop app (`gitbox`) will provide a visual interface built with **Wails v2 + Svelte**, sharing the same Go library (`pkg/`) as the CLI.

## Planned Features

- Account setup with guided credential wizard
- Repo discovery with checkboxes instead of numbered selection
- Status dashboard with color-coded sync state
- Clone with visual progress bars
- One-click pull for repos that are behind

## Same Config, Same Logic

The GUI reads the same config file as `gitboxcmd` (`~/.config/gitbox/gitbox.json`), so you can use both interchangeably. All business logic lives in `pkg/` — the GUI is a frontend, not a reimplementation.

See the [Architecture](architecture.md#9-gui-design-blueprint) for the technical blueprint.

## In the Meantime

Use the [CLI Quick Start](cli-guide.md) to get started with `gitboxcmd`.
