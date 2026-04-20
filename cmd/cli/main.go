// gitbox — lightweight command-line interface for managing
// Git repositories across multiple accounts and providers.
package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/LuisPalacios/gitbox/cmd/cli/tui"
	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/update"
	"github.com/spf13/cobra"
)

// Build-time variables (set via -ldflags).
// CI sets these; local builds get defaults with -dev- tag.
var (
	version = "dev"    // git tag (e.g., "v0.1.0") or "dev"
	commit  = "none"   // git short SHA (e.g., "a99cf17")
)

// Global flags.
var (
	configPath string
	jsonOutput bool
	verbose    bool
	testMode   bool
)

var rootCmd = &cobra.Command{
	Use:          "gitbox",
	Short:        "Unified tool for managing Git repositories across multiple accounts and providers",
	SilenceUsage: true,
}

func init() {
	rootCmd.Long = fmt.Sprintf("gitbox %s by Luis Palacios Derqui\nUnified tool for managing Git repositories across multiple accounts and providers.\nhttps://github.com/LuisPalacios/gitbox", fullVersion())

	rootCmd.PersistentFlags().StringVar(&configPath, "config", "", "path to config file (default: ~/.config/gitbox/gitbox.json)")
	rootCmd.PersistentFlags().BoolVar(&jsonOutput, "json", false, "output in JSON format")
	rootCmd.PersistentFlags().BoolVar(&verbose, "verbose", false, "verbose output")
	rootCmd.PersistentFlags().BoolVar(&testMode, "test-mode", false, "run with isolated test config from test-gitbox.json")

	// Preserve registration order (natural user workflow) instead of alphabetical.
	cobra.EnableCommandSorting = false

	// Command groups.
	rootCmd.AddGroup(
		&cobra.Group{ID: "main", Title: "Main Commands:"},
		&cobra.Group{ID: "additional", Title: "Additional Commands:"},
	)

	// Main commands — natural user order: setup → configure → operate.
	initCmd.GroupID = "main"
	globalCmd.GroupID = "main"
	accountCmd.GroupID = "main"
	sourceCmd.GroupID = "main"
	repoCmd.GroupID = "main"
	cloneCmd.GroupID = "main"
	statusCmd.GroupID = "main"
	pullCmd.GroupID = "main"
	fetchCmd.GroupID = "main"
	sweepCmd.GroupID = "main"
	browseCmd.GroupID = "main"
	mirrorCmd.GroupID = "main"
	workspaceCmd.GroupID = "main"
	rootCmd.AddCommand(initCmd, globalCmd, accountCmd, sourceCmd, repoCmd, cloneCmd, statusCmd, pullCmd, fetchCmd, sweepCmd, browseCmd, mirrorCmd, workspaceCmd)

	// Additional commands.
	reconfigureCmd.GroupID = "additional"
	identityCmd.GroupID = "additional"
	scanCmd.GroupID = "additional"
	adoptCmd.GroupID = "additional"
	updateCmd.GroupID = "additional"
	versionCmd.GroupID = "additional"
	rootCmd.AddCommand(reconfigureCmd, identityCmd, scanCmd, adoptCmd, updateCmd, versionCmd, tokenDeprecatedCmd)

	// Assign the auto-generated help command to the additional group.
	rootCmd.SetHelpCommandGroupID("additional")

	// Hide completion from the main Available Commands list;
	// it is shown in a custom section in the help template instead.
	rootCmd.CompletionOptions.HiddenDefaultCmd = true

	// Custom help template with grouped commands and "Shell completion" section.
	rootCmd.SetHelpTemplate(`{{with (or .Long .Short)}}{{. | trimTrailingWhitespaces}}

{{end}}Usage:{{if not .HasParent}}
  {{.CommandPath}}                          Start the interactive TUI
  {{.CommandPath}} [command] [flags]        Run in CLI mode{{else}}{{if .Runnable}}
  {{.UseLine}}{{end}}{{if .HasAvailableSubCommands}}
  {{.CommandPath}} [command]{{end}}{{end}}{{if gt (len .Aliases) 0}}

Aliases:
  {{.NameAndAliases}}{{end}}{{if .HasAvailableSubCommands}}{{$cmds := .Commands}}{{if eq (len .Groups) 0}}

Available Commands:{{range $cmds}}{{if (or .IsAvailableCommand (eq .Name "help"))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{else}}{{range $group := .Groups}}

{{.Title}}{{range $cmds}}{{if (and (eq .GroupID $group.ID) (or .IsAvailableCommand (eq .Name "help")))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{if not .AllChildCommandsHaveGroup}}

Additional Commands:{{range $cmds}}{{if (and (eq .GroupID "") (or .IsAvailableCommand (eq .Name "help")))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{end}}{{end}}
{{if not .HasParent}}
Shell completion:
  completion  Generate autocompletion for your shell (see docs/completion.md){{end}}{{if .HasAvailableLocalFlags}}

Flags:
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasAvailableInheritedFlags}}

Global Flags:
{{.InheritedFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasAvailableSubCommands}}

Use "{{.CommandPath}} [command] --help" for more information about a command.{{end}}
`)
}

func main() {
	// Clean up .old binaries from a previous Windows update.
	update.CleanupOldBinary()

	// Set up test-mode isolation before anything else.
	// This applies to both TUI and CLI subcommands.
	testCfgPath, cleanup, err := setupTestModeIfEnabled()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	if cleanup != nil {
		defer cleanup()
	}
	if testCfgPath != "" {
		configPath = testCfgPath // so configFilePath() returns it for CLI commands
	}

	// Determine if we should launch TUI: no subcommand + terminal.
	// Allow --test-mode as the only flag (still TUI mode).
	if isTerminal() && isTUILaunch() {
		if err := tui.Run(configFilePath(), testMode); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		return
	}
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

// isTUILaunch returns true if the command line has no subcommand — only
// global flags (--test-mode, --config) or no args at all.
func isTUILaunch() bool {
	for _, arg := range os.Args[1:] {
		if !strings.HasPrefix(arg, "-") {
			return false // found a positional arg (subcommand)
		}
		// Skip flag values: if --config <path>, the next arg is a value.
		if arg == "--config" {
			continue // next iteration will see the path, but it starts without "-" — handled below
		}
	}
	// Re-check: --config takes a value that doesn't start with "-".
	// Parse flags properly via Cobra's flag set.
	if err := rootCmd.PersistentFlags().Parse(os.Args[1:]); err != nil {
		return false
	}
	return len(rootCmd.PersistentFlags().Args()) == 0
}

// setupTestModeIfEnabled checks the --test-mode flag from os.Args
// (before Cobra parses) and sets up isolated config if enabled.
func setupTestModeIfEnabled() (cfgPath string, cleanup func(), err error) {
	for _, arg := range os.Args[1:] {
		if arg == "--test-mode" {
			testMode = true
			break
		}
	}
	if !testMode {
		return "", nil, nil
	}
	return config.SetupTestMode()
}

// isTerminal returns true if stdin is connected to a terminal (not a pipe).
func isTerminal() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// resolveConfigPath returns the config path to use.
func resolveConfigPath() string {
	if configPath != "" {
		return configPath
	}
	return ""
}

// configFilePath returns the resolved config file path.
func configFilePath() string {
	if configPath != "" {
		return configPath
	}
	return config.DefaultV2Path()
}

// loadConfig loads the config file.
func loadConfig() (*config.Config, error) {
	return config.Load(configFilePath())
}

// loadOrCreateConfig loads the config or creates an empty one if it doesn't exist.
func loadOrCreateConfig() (*config.Config, error) {
	path := configFilePath()
	cfg, err := config.Load(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			// Config file exists but failed to parse — report and abort.
			return nil, fmt.Errorf("config file %s exists but failed to load: %w\nFix the file or delete it to start fresh", path, err)
		}
		// File doesn't exist — create empty config.
		cfg = &config.Config{
			Version:  2,
			Global:   config.GlobalConfig{Folder: "~/00.git"},
			Accounts: make(map[string]config.Account),
			Sources:  make(map[string]config.Source),
		}
	}
	return cfg, nil
}

// saveConfig saves the config to disk.
func saveConfig(cfg *config.Config) error {
	path := configFilePath()
	if err := config.Save(cfg, path); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}
	if verbose {
		fmt.Fprintf(os.Stderr, "Config saved to %s\n", path)
	}
	return nil
}

// printError prints an error message to stderr.
func printError(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "error: "+format+"\n", args...)
}

// printStatusLine prints a colored one-liner: "symbol state  label  details".
// Used by clone, pull, scan, and other streaming commands.
func printStatusLine(symbol, state, label, details, color string) {
	status := colorize(fmt.Sprintf("%s %-8s", symbol, state), color)
	if details != "" {
		fmt.Printf("%s  %-55s  %s\n", status, label, details)
	} else {
		fmt.Printf("%s  %s\n", status, label)
	}
}

// printStatusLineProgress prints a colored one-liner WITHOUT a newline,
// using \r so it can be overwritten by the next call. Used to show
// in-progress state (e.g., "cloning...") before the final result.
func printStatusLineProgress(symbol, state, label, color string) {
	status := colorize(fmt.Sprintf("%s %-8s", symbol, state), color)
	line := fmt.Sprintf("%s  %s", status, label)
	// Pad to 80 chars to ensure previous content is overwritten.
	fmt.Printf("\r%-80s", line)
}

// printStatusLineFinish overwrites a progress line with the final state + newline.
// Pads with spaces to fully clear any previous progress bar content.
func printStatusLineFinish(symbol, state, label, details, color string) {
	status := colorize(fmt.Sprintf("%s %-8s", symbol, state), color)
	var line string
	if details != "" {
		line = fmt.Sprintf("%s  %-55s  %s", status, label, details)
	} else {
		line = fmt.Sprintf("%s  %s", status, label)
	}
	// Clear to end of line with ANSI escape, then newline.
	fmt.Printf("\r%s\033[K\n", line)
}
