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
	"github.com/LuisPalacios/gitbox/pkg/i18n"
	"github.com/LuisPalacios/gitbox/pkg/update"
	"github.com/spf13/cobra"
)

// Build-time variables (set via -ldflags).
// CI sets these; local builds get defaults with -dev- tag.
var (
	version = "dev"  // git tag (e.g., "v0.1.0") or "dev"
	commit  = "none" // git short SHA (e.g., "a99cf17")
)

// Global flags.
var (
	configPath string
	jsonOutput bool
	langFlag   string
	verbose    bool
	testMode   bool
	tr         = i18n.New(i18n.English)
)

var rootCmd = &cobra.Command{
	Use:          "gitbox",
	Short:        tr.T("app.description"),
	SilenceUsage: true,
}

func init() {
	applyCLITranslations(tr)

	rootCmd.PersistentFlags().StringVar(&configPath, "config", "", tr.T("flag.config"))
	rootCmd.PersistentFlags().BoolVar(&jsonOutput, "json", false, tr.T("flag.json"))
	rootCmd.PersistentFlags().StringVar(&langFlag, "lang", "", tr.T("flag.lang"))
	rootCmd.PersistentFlags().BoolVar(&verbose, "verbose", false, tr.T("flag.verbose"))
	rootCmd.PersistentFlags().BoolVar(&testMode, "test-mode", false, tr.T("flag.test_mode"))

	// Preserve registration order (natural user workflow) instead of alphabetical.
	cobra.EnableCommandSorting = false

	// Command groups.
	rootCmd.AddGroup(
		&cobra.Group{ID: "main", Title: tr.T("help.main_commands")},
		&cobra.Group{ID: "additional", Title: tr.T("help.additional_commands")},
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
	gitignoreCmd.GroupID = "additional"
	scanCmd.GroupID = "additional"
	adoptCmd.GroupID = "additional"
	updateCmd.GroupID = "additional"
	versionCmd.GroupID = "additional"
	doctorCmd.GroupID = "additional"
	rootCmd.AddCommand(reconfigureCmd, identityCmd, gitignoreCmd, scanCmd, adoptCmd, updateCmd, versionCmd, doctorCmd, tokenDeprecatedCmd)

	// Assign the auto-generated help command to the additional group.
	rootCmd.SetHelpCommandGroupID("additional")

	// Hide completion from the main Available Commands list;
	// it is shown in a custom section in the help template instead.
	rootCmd.CompletionOptions.HiddenDefaultCmd = true

	setHelpTemplate(tr)
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

	tr = i18n.New(i18n.Resolve(peekFlagValue("--lang"), loadConfigForLanguage()))
	applyCLITranslations(tr)

	// Determine if we should launch TUI: no subcommand + terminal.
	// Allow --test-mode as the only flag (still TUI mode).
	if isTerminal() && isTUILaunch() {
		if err := tui.Run(configFilePath(), testMode, tr); err != nil {
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
		fmt.Fprintf(os.Stderr, tr.T("msg.config_saved"), path)
	}
	return nil
}

// printError prints an error message to stderr.
func printError(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "error: "+format+"\n", args...)
}

func loadConfigForLanguage() *config.Config {
	path := peekFlagValue("--config")
	if path == "" {
		path = configFilePath()
	}
	cfg, err := config.Load(path)
	if err != nil {
		return nil
	}
	return cfg
}

func peekFlagValue(name string) string {
	args := os.Args[1:]
	prefix := name + "="
	for i, arg := range args {
		if arg == name && i+1 < len(args) {
			return args[i+1]
		}
		if strings.HasPrefix(arg, prefix) {
			return strings.TrimPrefix(arg, prefix)
		}
	}
	return ""
}

func applyCLITranslations(t i18n.Translator) {
	rootCmd.Short = t.T("app.description")
	rootCmd.Long = t.F("app.long", fullVersion())
	if groups := rootCmd.Groups(); len(groups) >= 2 {
		groups[0].Title = t.T("help.main_commands")
		groups[1].Title = t.T("help.additional_commands")
	}
	setHelpTemplate(t)
	translateGlobalCommand(t)
	translateCommandShorts(t)
	if f := rootCmd.PersistentFlags().Lookup("config"); f != nil {
		f.Usage = t.T("flag.config")
	}
	if f := rootCmd.PersistentFlags().Lookup("json"); f != nil {
		f.Usage = t.T("flag.json")
	}
	if f := rootCmd.PersistentFlags().Lookup("lang"); f != nil {
		f.Usage = t.T("flag.lang")
	}
	if f := rootCmd.PersistentFlags().Lookup("verbose"); f != nil {
		f.Usage = t.T("flag.verbose")
	}
	if f := rootCmd.PersistentFlags().Lookup("test-mode"); f != nil {
		f.Usage = t.T("flag.test_mode")
	}
}

func translateCommandShorts(t i18n.Translator) {
	keys := map[string]string{
		"init":        "cmd.init.short",
		"account":     "cmd.account.short",
		"source":      "cmd.source.short",
		"repo":        "cmd.repo.short",
		"clone":       "cmd.clone.short",
		"status":      "cmd.status.short",
		"pull":        "cmd.pull.short",
		"fetch":       "cmd.fetch.short",
		"sweep":       "cmd.sweep.short",
		"browse":      "cmd.browse.short",
		"mirror":      "cmd.mirror.short",
		"workspace":   "cmd.workspace.short",
		"reconfigure": "cmd.reconfigure.short",
		"identity":    "cmd.identity.short",
		"gitignore":   "cmd.gitignore.short",
		"scan":        "cmd.scan.short",
		"adopt":       "cmd.adopt.short",
		"update":      "cmd.update.short",
		"version":     "cmd.version.short",
		"doctor":      "cmd.doctor.short",
	}
	for _, cmd := range rootCmd.Commands() {
		if key, ok := keys[cmd.Name()]; ok {
			cmd.Short = t.T(key)
		}
	}
}

func setHelpTemplate(t i18n.Translator) {
	rootCmd.SetHelpTemplate(fmt.Sprintf(`{{with (or .Long .Short)}}{{. | trimTrailingWhitespaces}}

{{end}}%s{{if not .HasParent}}
  {{.CommandPath}}                          %s
  {{.CommandPath}} [command] [flags]        %s{{else}}{{if .Runnable}}
  {{.UseLine}}{{end}}{{if .HasAvailableSubCommands}}
  {{.CommandPath}} [command]{{end}}{{end}}{{if gt (len .Aliases) 0}}

%s
  {{.NameAndAliases}}{{end}}{{if .HasAvailableSubCommands}}{{$cmds := .Commands}}{{if eq (len .Groups) 0}}

%s{{range $cmds}}{{if (or .IsAvailableCommand (eq .Name "help"))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{else}}{{range $group := .Groups}}

{{.Title}}{{range $cmds}}{{if (and (eq .GroupID $group.ID) (or .IsAvailableCommand (eq .Name "help")))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{if not .AllChildCommandsHaveGroup}}

%s{{range $cmds}}{{if (and (eq .GroupID "") (or .IsAvailableCommand (eq .Name "help")))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{end}}{{end}}
{{if not .HasParent}}
%s
  completion  %s{{end}}{{if .HasAvailableLocalFlags}}

%s
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasAvailableInheritedFlags}}

%s
{{.InheritedFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasAvailableSubCommands}}

%s{{end}}
`,
		t.T("help.usage"),
		t.T("help.start_tui"),
		t.T("help.cli_mode"),
		t.T("help.aliases"),
		t.T("help.available_commands"),
		t.T("help.additional_commands"),
		t.T("help.shell_completion"),
		t.T("help.completion_desc"),
		t.T("help.flags"),
		t.T("help.global_flags"),
		t.F("help.more", "{{.CommandPath}}"),
	))
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
