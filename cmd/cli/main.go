// gitboxcmd — lightweight command-line interface for managing
// Git repositories across multiple accounts and providers.
package main

import (
	"fmt"
	"os"

	"github.com/LuisPalacios/gitbox/pkg/config"
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
)

var rootCmd = &cobra.Command{
	Use:          "gitboxcmd",
	Short:        "Unified tool for managing Git repositories across multiple accounts and providers",
	SilenceUsage: true,
}

func init() {
	rootCmd.Long = fmt.Sprintf("gitbox %s by Luis Palacios Derqui\nUnified tool for managing Git repositories across multiple accounts and providers.\nhttps://github.com/LuisPalacios/gitbox", fullVersion())

	rootCmd.PersistentFlags().StringVar(&configPath, "config", "", "path to config file (default: ~/.config/gitbox/gitbox.json)")
	rootCmd.PersistentFlags().BoolVar(&jsonOutput, "json", false, "output in JSON format")
	rootCmd.PersistentFlags().BoolVar(&verbose, "verbose", false, "verbose output")

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
	rootCmd.AddCommand(initCmd, globalCmd, accountCmd, sourceCmd, repoCmd, cloneCmd, statusCmd, pullCmd)

	// Additional commands.
	identityCmd.GroupID = "additional"
	scanCmd.GroupID = "additional"
	migrateCmd.GroupID = "additional"
	versionCmd.GroupID = "additional"
	rootCmd.AddCommand(identityCmd, scanCmd, migrateCmd, versionCmd, tokenDeprecatedCmd)

	// Assign the auto-generated help command to the additional group.
	rootCmd.SetHelpCommandGroupID("additional")

	// Hide completion from the main Available Commands list;
	// it is shown in a custom section in the help template instead.
	rootCmd.CompletionOptions.HiddenDefaultCmd = true

	// Custom help template with grouped commands and "Shell completion" section.
	rootCmd.SetHelpTemplate(`{{with (or .Long .Short)}}{{. | trimTrailingWhitespaces}}

{{end}}Usage:{{if .Runnable}}
  {{.UseLine}}{{end}}{{if .HasAvailableSubCommands}}
  {{.CommandPath}} [command]{{end}}{{if gt (len .Aliases) 0}}

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
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
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
