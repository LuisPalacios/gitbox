GITBOX for macOS
================

HOW TO INSTALL
--------------

Open Terminal and run:

  bash "/Volumes/gitbox/Install Gitbox.command"

The script will:
  - Copy GitboxApp.app to /Applications/
  - Remove quarantine attributes so macOS allows it to run

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

  open /Applications/GitboxApp.app   Launch gitbox

The gitbox command-line tool and terminal UI ship only in 1.x
releases. To install the latest 1.x CLI:

  bash <(curl -fsSL https://raw.githubusercontent.com/LuisPalacios/gitbox/main/scripts/bootstrap.sh) --cli-only

Full documentation: https://github.com/LuisPalacios/gitbox#readme
