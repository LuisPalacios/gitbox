# Gitbox Feature Radar

This document tracks future feature concepts, quality-of-life enhancements, and architectural ideas that are on the radar but not yet part of the active development roadmap.

### 1. Config Sync (The "Meta" Feature)

**The Concept:** Gitbox fixes repository cloning across machines, but users still have to manually copy `gitbox.json`. This feature allows users to back up their config to a private repository on one of their connected providers.
**Implementation:** A `gitbox config sync` command that creates a hidden private repo (e.g., `.gitbox-sync`), pushes the JSON config there, and allows a new machine to pull it down. The `credentials/` folder remains strictly machine-local for security.

### 2. Bulk Branch Cleanup (`gitbox sweep`)

**The Concept:** Developers notoriously accumulate dozens of dead local branches across multiple repositories. This feature safely deletes local branches that have already been merged upstream or deleted on the remote.
**Implementation:** A command and GUI panel that iterates through all repositories in a source, shelling out to `git fetch --prune` followed by `git branch --merged` to identify and nuke stale local branches in bulk.

### 3. Dynamic Workspaces

**The Concept:** When working on microservice architectures, developers often need to open an entire Source or Organization at once, rather than just single repositories.
**Implementation:** A `gitbox workspace generate` command that automatically creates a `<source>.code-workspace` file for VS Code (including all repos under that source) or a `tmuxinator` profile to spin up a Tmux session with a window for each repository.

### 4. Smart Archiving

**The Concept:** Detect when a repository has been archived upstream (e.g., made read-only on GitHub) and offer to free up local disk space.
**Implementation:** Show an "Archived Upstream" badge in the GUI during discovery. Provide a one-click action to compress the local clone into a `.tar.gz` file (preserving local history and stashes), delete the raw directory, and update the `gitbox.json` state to `archived`.

### 5. Unified "Action Required" Dashboard

**The Concept:** Leverage Gitbox's cross-provider API tokens to build a unified notification center for Pull Requests and code reviews.
**Implementation:** A lightweight dashboard tab in the GUI that runs asynchronous fetches against the search/issues APIs of configured providers (e.g., `is:pr is:open review-requested:@me`). This aggregates pending reviews into a simple list of links, turning Gitbox into a daily developer launchpad.