GITBOX — macOS INSTALLER
========================

TRUST WARNING
-------------

Gitbox is NOT signed or notarized by Apple. macOS Gatekeeper will block
the binaries if you drag-and-drop them manually. By running the bundled
installer or clearing quarantine attributes yourself, you are explicitly
trusting unsigned code.

Audit the source before running anything:
  https://github.com/LuisPalacios/gitbox

This software is provided "as is", without warranty of any kind. See the
LICENSE file in the repository for full terms.


RECOMMENDED: USE THE INSTALLER
-------------------------------

The DMG includes an "Install Gitbox" script that copies the binaries
and removes quarantine flags automatically. It will:

  1. Copy GitboxApp.app to /Applications/
  2. Copy the gitbox CLI to ~/bin/
  3. Remove quarantine attributes so macOS allows them to run
  4. Add ~/bin to your PATH if not already there

The script asks for confirmation before making changes. It does not
require administrator (sudo) privileges and does not access the network.

Because this software is not signed, macOS Gatekeeper will also block
the installer script itself when you double-click it. Two ways to run it:

  Option A — Right-click "Install Gitbox" and select "Open". macOS will
  show a warning dialog; click "Open" to proceed.

  Option B — Open Terminal and run:

    bash "/Volumes/gitbox/Install Gitbox.command"

  This bypasses Gatekeeper because bash reads the script as a text file.


MANUAL INSTALLATION
-------------------

If you prefer not to use the script:

  1. Drag GitboxApp.app to /Applications/ (or any folder you like)
  2. Open Terminal and run:

       xattr -cr /Applications/GitboxApp.app

  3. Copy the gitbox binary to a directory in your PATH:

       cp /Volumes/gitbox/gitbox ~/bin/
       chmod +x ~/bin/gitbox
       xattr -cr ~/bin/gitbox

The xattr command removes the quarantine flag that macOS sets on files
downloaded from the internet. Without it, Gatekeeper will prevent the
app from launching.


GETTING STARTED
---------------

After installation, run:

  gitbox help

Or launch GitboxApp from /Applications/.

Full documentation: https://github.com/LuisPalacios/gitbox#readme
