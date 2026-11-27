# Upcoming release notes

## Onboarding names its guided setup and can be skipped

The first step of the first-run onboarding now offers **Guided setup**, with a line saying it connects an account and gets the first project, and **Skip and open gitbox**, which saves the clone root and opens the dashboard. A root that already holds clones shows the same single button, and the guide's project step then starts on those clones. See [Step 1: First launch](gui-guide.md#step-1-first-launch).

## A remote host without gitbox.json is set up from its window

Connecting to a host that has no `gitbox.json` no longer stops at a card that says to set it up by hand. The window opens the first-run onboarding for that host: the folder, the account and the first project are set up there, and the host saves its own config. See [Remote mode](remote-mode.md#what-a-remote-window-can-do).
