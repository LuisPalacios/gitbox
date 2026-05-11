package terminals

import "testing"

// TestCatalogIDsUnique guards the catalog invariant that ID is unique within
// each per-OS catalog. Sync's detection emits one TerminalApp/ShellEntry per
// catalog entry; collisions would silently shadow earlier rows.
func TestCatalogIDsUnique(t *testing.T) {
	t.Run("windows-terminals", func(t *testing.T) {
		assertUniqueTermIDs(t, windowsTerminals)
	})
	t.Run("darwin-terminals", func(t *testing.T) {
		assertUniqueTermIDs(t, darwinTerminals)
	})
	t.Run("linux-terminals", func(t *testing.T) {
		assertUniqueTermIDs(t, linuxTerminals)
	})
	t.Run("windows-shells", func(t *testing.T) {
		assertUniqueShellIDs(t, windowsShells)
	})
	t.Run("darwin-shells", func(t *testing.T) {
		assertUniqueShellIDs(t, darwinShells)
	})
	t.Run("linux-shells", func(t *testing.T) {
		assertUniqueShellIDs(t, linuxShells)
	})
}

func assertUniqueTermIDs(t *testing.T, list []CatalogTerminal) {
	t.Helper()
	seen := make(map[string]bool, len(list))
	for _, c := range list {
		if seen[c.ID] {
			t.Errorf("duplicate terminal ID %q", c.ID)
		}
		seen[c.ID] = true
		if c.ID == "" {
			t.Errorf("terminal entry %q has empty ID", c.Name)
		}
		if c.Name == "" {
			t.Errorf("terminal entry id=%q has empty Name", c.ID)
		}
		if c.Probe == nil {
			t.Errorf("terminal entry id=%q has nil Probe", c.ID)
		}
		// Empty ArgsTemplate is legitimate for terminals whose CLI has no
		// working-directory flag (e.g. xterm on Linux) — cmd.Dir handles
		// cwd, $SHELL handles the shell. Don't fail those rows.
	}
}

func assertUniqueShellIDs(t *testing.T, list []CatalogShell) {
	t.Helper()
	seen := make(map[string]bool, len(list))
	for _, c := range list {
		if seen[c.ID] {
			t.Errorf("duplicate shell ID %q", c.ID)
		}
		seen[c.ID] = true
		if c.ID == "" || c.Name == "" || c.Probe == nil {
			t.Errorf("shell entry id=%q name=%q has missing required fields", c.ID, c.Name)
		}
	}
}

// TestModernTerminalCoverage confirms the IsModern derivation
// (templateAcceptsShell) classifies catalog entries correctly per OS. All
// Windows Terminals must be modern (shell-token bearing); Linux Terminals
// are generally modern, with the exception of legacy emulators (xterm)
// whose CLI takes no working-directory flag and relies on cmd.Dir + $SHELL
// — those carry an empty argv template. macOS `open -a` entries are NOT
// modern in this sense — they delegate shell selection to the App.
func TestModernTerminalCoverage(t *testing.T) {
	for _, c := range windowsTerminals {
		if !modernTerminal(c) {
			t.Errorf("expected Windows catalog entry %q to be modern (have shell tokens)", c.ID)
		}
	}
	// Linux exception list: emulators whose CLI has no `--working-directory`
	// flag and no `-e <cmd>` slot we can populate with the implicit Unix
	// login-shell. They launch with cmd.Dir + $SHELL fallback instead.
	linuxNonModern := map[string]bool{"xterm": true}
	for _, c := range linuxTerminals {
		if linuxNonModern[c.ID] {
			if modernTerminal(c) {
				t.Errorf("Linux catalog entry %q is on the non-modern exception list but carries shell tokens", c.ID)
			}
			continue
		}
		if !modernTerminal(c) {
			t.Errorf("expected Linux catalog entry %q to be modern (have shell tokens)", c.ID)
		}
	}
	for _, c := range darwinTerminals {
		// macOS `open -a` rows have no shell tokens — they delegate to the App.
		if modernTerminal(c) {
			t.Errorf("expected macOS catalog entry %q to be NOT-modern (open -a)", c.ID)
		}
	}
}

// TestCatalogShape sanity-checks that the per-OS catalogs cover the issue #71
// spec — at least one Terminal per OS, sane shell coverage. Not exhaustive;
// just guards against accidental empty-catalog regressions.
func TestCatalogShape(t *testing.T) {
	if len(windowsTerminals) < 5 {
		t.Errorf("expected ≥5 Windows terminals, got %d", len(windowsTerminals))
	}
	if len(darwinTerminals) < 5 {
		t.Errorf("expected ≥5 macOS terminals, got %d", len(darwinTerminals))
	}
	if len(linuxTerminals) < 5 {
		t.Errorf("expected ≥5 Linux terminals, got %d", len(linuxTerminals))
	}
	if len(windowsShells) < 4 {
		t.Errorf("expected ≥4 Windows shells, got %d", len(windowsShells))
	}
	if len(darwinShells) < 3 {
		t.Errorf("expected ≥3 macOS shells, got %d", len(darwinShells))
	}
	if len(linuxShells) < 3 {
		t.Errorf("expected ≥3 Linux shells, got %d", len(linuxShells))
	}
}
