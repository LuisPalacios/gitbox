package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/LuisPalacios/gitbox/pkg/workspace"
	"github.com/spf13/cobra"
)

var workspaceCmd = &cobra.Command{
	Use:   "workspace",
	Short: "List and open discovered VS Code workspaces (read-only)",
	Long: `Workspaces are read-only in gitbox. gitbox discovers existing
*.code-workspace files under the configured folders and lists them so I can
open one in VS Code. It never creates, edits, generates, or deletes them.`,
}

// --- workspace list ---

var workspaceListCmd = &cobra.Command{
	Use:   "list",
	Short: "List discovered workspaces",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}

		if jsonOutput {
			data, _ := json.MarshalIndent(cfg.Workspaces, "", "    ")
			fmt.Fprintln(os.Stdout, string(data))
			return nil
		}

		if len(cfg.Workspaces) == 0 {
			fmt.Println("No workspaces discovered. Run 'gitbox workspace discover' to refresh.")
			return nil
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintf(w, "KEY\tMEMBERS\tFILE\n")
		fmt.Fprintf(w, "───\t───────\t────\n")
		for _, key := range cfg.OrderedWorkspaceKeys() {
			ws := cfg.Workspaces[key]
			fmt.Fprintf(w, "%s\t%d\t%s\n", key, len(ws.Members), ws.File)
		}
		w.Flush()
		return nil
	},
}

// --- workspace show ---

var workspaceShowCmd = &cobra.Command{
	Use:   "show <workspace-key>",
	Short: "Show workspace details",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		ws, ok := cfg.Workspaces[args[0]]
		if !ok {
			return fmt.Errorf("workspace %q not found", args[0])
		}
		if jsonOutput {
			data, _ := json.MarshalIndent(ws, "", "    ")
			fmt.Fprintln(os.Stdout, string(data))
			return nil
		}

		fmt.Printf("%s  %s\n", colorize(args[0], colorWhite), ws.EffectiveName(args[0]))
		fmt.Printf("  file:    %s\n", ws.File)
		fmt.Printf("  members: %d\n", len(ws.Members))
		for _, m := range ws.Members {
			fmt.Printf("    - %s/%s\n", m.Source, m.Repo)
		}
		return nil
	},
}

// --- workspace open ---

var workspaceOpenCmd = &cobra.Command{
	Use:   "open <workspace-key>",
	Short: "Open a discovered workspace in the configured editor",
	Long: `Opens the discovered *.code-workspace file with the first editor from
global.editors in gitbox.json.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		if _, ok := cfg.Workspaces[args[0]]; !ok {
			return fmt.Errorf("workspace %q not found", args[0])
		}
		oc, err := workspace.BuildOpenCommand(cfg, args[0])
		if err != nil {
			return err
		}
		fmt.Printf("Launching: %s\n", oc.Description)
		if err := oc.Cmd.Start(); err != nil {
			return fmt.Errorf("launch: %w", err)
		}
		// Detach — the launcher owns its own lifecycle.
		return nil
	},
}

// --- workspace discover ---

var workspaceDiscoverCmd = &cobra.Command{
	Use:   "discover",
	Short: "Rescan for *.code-workspace files and refresh the cache",
	Long: `Walks the standard folder and configured extra folders for
*.code-workspace files, resolves their member clones, and refreshes the
read-only workspace cache in gitbox.json.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		found, err := workspace.Discover(cfg)
		if err != nil {
			return err
		}

		changed, err := workspace.RefreshCache(cfg)
		if err != nil {
			return err
		}
		if changed {
			if err := saveConfig(cfg); err != nil {
				return fmt.Errorf("saving config: %w", err)
			}
		}

		if jsonOutput {
			data, _ := json.MarshalIndent(map[string]any{
				"found":   found,
				"changed": changed,
			}, "", "    ")
			fmt.Fprintln(os.Stdout, string(data))
			return nil
		}

		if len(found) == 0 {
			fmt.Println("No *.code-workspace files found.")
			return nil
		}
		fmt.Printf("Discovered %d workspace(s):\n", len(found))
		for _, f := range found {
			members := make([]string, 0, len(f.Members))
			for _, m := range f.Members {
				members = append(members, m.Source+"/"+m.Repo)
			}
			fmt.Printf("  %s  %s\n", f.Key, f.File)
			if len(members) > 0 {
				fmt.Printf("      members: %s\n", strings.Join(members, ", "))
			}
		}
		if changed {
			fmt.Println("\nCache updated.")
		} else {
			fmt.Println("\nCache already up to date.")
		}
		return nil
	},
}

func init() {
	workspaceCmd.AddCommand(
		workspaceListCmd,
		workspaceShowCmd,
		workspaceOpenCmd,
		workspaceDiscoverCmd,
	)
}
