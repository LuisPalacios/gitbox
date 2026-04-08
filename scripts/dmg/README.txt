GITBOX for macOS
================

HOW TO INSTALL
--------------

Open Terminal and run:

  bash "/Volumes/gitbox/Install Gitbox.command"

The script will:
  - Copy GitboxApp.app to /Applications/
  - Copy the gitbox CLI to ~/bin/
  - Remove quarantine attributes so macOS allows them to run
  - Add ~/bin to your PATH if needed

It asks for confirmation first. No sudo required, no network access.

Why Terminal? This app is not signed by Apple, so macOS Gatekeeper
blocks everything in this DMG — including the installer script itself.
Running it through bash bypasses that restriction.


MANUAL INSTALLATION
-------------------

If you prefer not to use the script:

  1. Drag GitboxApp.app to /Applications/
  2. Open Terminal and run:

       xattr -cr /Applications/GitboxApp.app

  3. Copy the CLI binary to a directory in your PATH:

       mkdir -p ~/bin
       cp /Volumes/gitbox/gitbox ~/bin/
       chmod +x ~/bin/gitbox
       xattr -cr ~/bin/gitbox

The xattr command removes the quarantine flag that macOS sets on
files downloaded from the internet.


TRUST WARNING
-------------

Gitbox is NOT signed or notarized by Apple. By running the installer
or clearing quarantine attributes yourself, you are explicitly trusting
unsigned code. Audit the source before running anything:

  https://github.com/LuisPalacios/gitbox

This software is provided "as is", without warranty of any kind.
See the LICENSE file in the repository for full terms.


AFTER INSTALLATION
------------------

  gitbox help               Launch the CLI
  gitbox                    Launch the TUI (interactive terminal UI)
  open /Applications/GitboxApp.app   Launch the GUI

Full documentation: https://github.com/LuisPalacios/gitbox#readme
