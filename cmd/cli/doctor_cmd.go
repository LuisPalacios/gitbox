package main

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/doctor"
	"github.com/spf13/cobra"
)

var doctorJSON bool

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check that every external tool gitbox relies on is installed",
	Long: `Probe the host for the command-line tools gitbox uses (git, Git Credential
Manager, OpenSSH, tmux, ...) and report whether each is installed, where it
lives on disk, and its version.

When a tool is missing, doctor prints an install command for the current OS.
The "required for your config" column tells you whether gitbox actually needs
the tool given the accounts and workspaces in your gitbox.json — tools that
aren't required are informational.

Use --json to get machine-readable output for scripts and bug reports.`,
	RunE: runDoctor,
}

func init() {
	doctorCmd.Flags().BoolVar(&doctorJSON, "json", false, "emit JSON instead of a human table")
}

type doctorJSONEntry struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	Purpose     string `json:"purpose"`
	Found       bool   `json:"found"`
	Path        string `json:"path,omitempty"`
	Version     string `json:"version,omitempty"`
	Required    bool   `json:"required"`
	ReasonReq   string `json:"requiredFor,omitempty"`
	InstallHint string `json:"installHint,omitempty"`
}

func runDoctor(cmd *cobra.Command, args []string) error {
	// Load config if present; doctor still works without one, just with no
	// "required for your config" annotations.
	cfg, _ := loadConfig()

	results := doctor.Check(doctor.StandardTools())
	required := requiredToolsFor(cfg)

	if doctorJSON {
		return emitDoctorJSON(results, required)
	}
	emitDoctorText(results, required)
	return nil
}

// requiredToolsFor inspects the config and returns, for each tool name,
// the reason it is required (or "" if it isn't).
func requiredToolsFor(cfg *config.Config) map[string]string {
	required := make(map[string]string)
	required["git"] = "always — core dependency"

	if cfg == nil {
		return required
	}

	// Walk accounts to see which credential types are in play.
	var anyGCM, anySSH, anyToken bool
	for _, acct := range cfg.Accounts {
		switch acct.DefaultCredentialType {
		case "gcm":
			anyGCM = true
		case "ssh":
			anySSH = true
		case "token":
			anyToken = true
		}
	}
	if anyGCM || anyToken {
		required["git-credential-manager"] = "you have accounts using the gcm/token credential type"
	}
	if anySSH {
		required["ssh"] = "you have accounts using the ssh credential type"
		required["ssh-keygen"] = "you have accounts using the ssh credential type"
		required["ssh-add"] = "you have accounts using the ssh credential type"
	}

	// Walk workspaces to see if tmuxinator is in use.
	var anyTmuxinator bool
	for _, ws := range cfg.Workspaces {
		if ws.Type == "tmuxinator" {
			anyTmuxinator = true
			break
		}
	}
	if anyTmuxinator {
		required["tmux"] = "you have tmuxinator workspaces"
		required["tmuxinator"] = "you have tmuxinator workspaces"
		if runtime.GOOS == "windows" {
			required["wsl"] = "tmuxinator runs inside WSL on Windows"
		}
	}
	return required
}

func emitDoctorText(results []doctor.Result, required map[string]string) {
	fmt.Printf("%s\n", colorize("System tools", colorWhite))
	fmt.Println()
	anyMissingRequired := false
	for _, r := range results {
		reason, isRequired := required[r.Tool.Name]
		if isRequired && !r.Found {
			anyMissingRequired = true
		}
		printDoctorRow(r, isRequired, reason)
	}
	fmt.Println()
	if anyMissingRequired {
		fmt.Printf("%s One or more required tools are missing. See the install hints above.\n",
			colorize("!", colorRed))
		os.Exit(1)
	}
	fmt.Printf("%s Everything gitbox needs is installed.\n", colorize("OK", colorGreen))
}

func printDoctorRow(r doctor.Result, isRequired bool, reason string) {
	var symbol, color, state string
	switch {
	case r.Found:
		symbol, color, state = "✓", colorGreen, "ok"
	case isRequired:
		symbol, color, state = "x", colorRed, "missing"
	default:
		symbol, color, state = "·", colorOrange, "optional"
	}

	// Line 1: status + tool name + version/path.
	var details string
	if r.Found {
		switch {
		case r.Version != "" && r.Path != "":
			details = fmt.Sprintf("%s  %s", r.Version, r.Path)
		case r.Path != "":
			details = r.Path
		}
	} else {
		details = r.Tool.Purpose
	}
	printStatusLine(symbol, state, r.Tool.DisplayName, details, color)

	// Line 2 (only when missing): reason this tool matters + install hint.
	if !r.Found {
		if isRequired {
			fmt.Printf("          needed:  %s\n", reason)
		}
		if hint := r.InstallHint(); hint != "" {
			fmt.Printf("          install: %s\n", hint)
		}
	}
}

func emitDoctorJSON(results []doctor.Result, required map[string]string) error {
	out := make([]doctorJSONEntry, 0, len(results))
	for _, r := range results {
		reason, isRequired := required[r.Tool.Name]
		out = append(out, doctorJSONEntry{
			Name:        r.Tool.Name,
			DisplayName: r.Tool.DisplayName,
			Purpose:     r.Tool.Purpose,
			Found:       r.Found,
			Path:        r.Path,
			Version:     r.Version,
			Required:    isRequired,
			ReasonReq:   reason,
			InstallHint: r.InstallHint(),
		})
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}
