package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/workspace"
	"github.com/spf13/cobra"
)

var workspaceCmd = &cobra.Command{
	Use:   "workspace",
	Short: "Manage multi-repo workspaces (VS Code, tmuxinator)",
}

// --- workspace list ---

var workspaceListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all workspaces",
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
			fmt.Println("No workspaces configured.")
			return nil
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintf(w, "KEY\tTYPE\tMEMBERS\tFILE\n")
		fmt.Fprintf(w, "───\t────\t───────\t────\n")
		for _, key := range cfg.OrderedWorkspaceKeys() {
			ws := cfg.Workspaces[key]
			file := ws.File
			if file == "" {
				file = "(not generated)"
			}
			fmt.Fprintf(w, "%s\t%s\t%d\t%s\n", key, ws.Type, len(ws.Members), file)
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
		fmt.Printf("  type:    %s\n", ws.Type)
		if ws.Layout != "" {
			fmt.Printf("  layout:  %s\n", ws.Layout)
		}
		if ws.File != "" {
			fmt.Printf("  file:    %s\n", ws.File)
		} else {
			fmt.Printf("  file:    (not generated)\n")
		}
		if ws.Discovered {
			fmt.Printf("  discovered: true\n")
		}
		fmt.Printf("  members: %d\n", len(ws.Members))
		for _, m := range ws.Members {
			fmt.Printf("    - %s/%s\n", m.Source, m.Repo)
		}
		return nil
	},
}

// --- workspace add ---

var (
	workspaceAddType    string
	workspaceAddName    string
	workspaceAddFile    string
	workspaceAddLayout  string
	workspaceAddMembers []string
)

var workspaceAddCmd = &cobra.Command{
	Use:   "add <workspace-key>",
	Short: "Create a new workspace",
	Long: `Creates a workspace entry in gitbox.json. Does NOT write the generated
file to disk — use 'gitbox workspace generate' afterward (or 'open' to
generate-and-launch in one step).

Members are given as repeated --member source/repo-key flags:

  gitbox workspace add my-feature \
    --type codeWorkspace \
    --member github-me/me/frontend \
    --member gitea-work/team/backend`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		ws := config.Workspace{
			Type:   workspaceAddType,
			Name:   workspaceAddName,
			File:   workspaceAddFile,
			Layout: workspaceAddLayout,
		}
		for _, spec := range workspaceAddMembers {
			m, err := parseMemberSpec(spec)
			if err != nil {
				return err
			}
			ws.Members = append(ws.Members, m)
		}
		if err := cfg.AddWorkspace(args[0], ws); err != nil {
			return err
		}
		if err := saveConfig(cfg); err != nil {
			return err
		}
		fmt.Printf("Workspace %q created (%s, %d member(s))\n", args[0], ws.Type, len(ws.Members))
		return nil
	},
}

// --- workspace delete ---

var workspaceDeleteCmd = &cobra.Command{
	Use:   "delete <workspace-key>",
	Short: "Delete a workspace (does not remove the generated file)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		if err := cfg.DeleteWorkspace(args[0]); err != nil {
			return err
		}
		if err := saveConfig(cfg); err != nil {
			return err
		}
		fmt.Printf("Workspace %q deleted from config\n", args[0])
		return nil
	},
}

// --- workspace add-member / delete-member ---

var workspaceAddMemberCmd = &cobra.Command{
	Use:   "add-member <workspace-key> <source/repo-key>",
	Short: "Add a member clone to a workspace",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		m, err := parseMemberSpec(args[1])
		if err != nil {
			return err
		}
		if err := cfg.AddWorkspaceMember(args[0], m); err != nil {
			return err
		}
		if err := saveConfig(cfg); err != nil {
			return err
		}
		fmt.Printf("Added %s/%s to workspace %q\n", m.Source, m.Repo, args[0])
		return nil
	},
}

var workspaceDeleteMemberCmd = &cobra.Command{
	Use:   "delete-member <workspace-key> <source/repo-key>",
	Short: "Remove a member clone from a workspace",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		m, err := parseMemberSpec(args[1])
		if err != nil {
			return err
		}
		if err := cfg.DeleteWorkspaceMember(args[0], m.Source, m.Repo); err != nil {
			return err
		}
		if err := saveConfig(cfg); err != nil {
			return err
		}
		fmt.Printf("Removed %s/%s from workspace %q\n", m.Source, m.Repo, args[0])
		return nil
	},
}

// --- workspace generate ---

var workspaceGenerateDryRun bool

var workspaceGenerateCmd = &cobra.Command{
	Use:   "generate <workspace-key>",
	Short: "Generate (or regenerate) the workspace file on disk",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		result, err := workspace.Generate(cfg, args[0])
		if err != nil {
			return err
		}
		if workspaceGenerateDryRun {
			fmt.Printf("Would write %s (%d bytes)\n", result.File, len(result.Content))
			fmt.Println("--- content ---")
			fmt.Print(string(result.Content))
			return nil
		}
		if err := writeWorkspaceFile(result); err != nil {
			return err
		}
		// Persist the chosen file path back to config so subsequent 'open'
		// calls know where it lives.
		ws := cfg.Workspaces[args[0]]
		if ws.File != result.File {
			ws.File = result.File
			if err := cfg.UpdateWorkspace(args[0], ws); err != nil {
				return err
			}
			if err := saveConfig(cfg); err != nil {
				return err
			}
		}
		fmt.Printf("Generated %s (%d bytes)\n", result.File, len(result.Content))
		return nil
	},
}

// --- workspace open ---

var workspaceOpenCmd = &cobra.Command{
	Use:   "open <workspace-key>",
	Short: "Open a workspace with its configured launcher",
	Long: `Opens the workspace file with the first editor (for code workspaces) or
the first terminal running tmuxinator (for tmuxinator workspaces) from
global.editors / global.terminals in gitbox.json.

If the file hasn't been generated yet, this command generates it first.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}

		ws, ok := cfg.Workspaces[args[0]]
		if !ok {
			return fmt.Errorf("workspace %q not found", args[0])
		}

		// Generate (or regenerate) the file so it is always current before
		// we hand it off to the launcher.
		result, err := workspace.Generate(cfg, args[0])
		if err != nil {
			return err
		}
		if err := writeWorkspaceFile(result); err != nil {
			return err
		}
		if ws.File != result.File {
			ws.File = result.File
			if err := cfg.UpdateWorkspace(args[0], ws); err != nil {
				return err
			}
			if err := saveConfig(cfg); err != nil {
				return err
			}
		}

		// Rebuild the open command against the updated config.
		cfg2, err := loadConfig()
		if err != nil {
			return err
		}
		oc, err := workspace.BuildOpenCommand(cfg2, args[0])
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

func parseMemberSpec(spec string) (config.WorkspaceMember, error) {
	i := strings.IndexByte(spec, '/')
	if i <= 0 || i == len(spec)-1 {
		return config.WorkspaceMember{}, fmt.Errorf("invalid member spec %q: expected <source-key>/<repo-key>", spec)
	}
	return config.WorkspaceMember{
		Source: spec[:i],
		Repo:   spec[i+1:],
	}, nil
}

func writeWorkspaceFile(result workspace.GenerateResult) error {
	if err := os.MkdirAll(filepath.Dir(result.File), 0o755); err != nil {
		return fmt.Errorf("creating parent dir for %s: %w", result.File, err)
	}
	if err := os.WriteFile(result.File, result.Content, 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", result.File, err)
	}
	return nil
}

func init() {
	workspaceAddCmd.Flags().StringVar(&workspaceAddType, "type", "", "workspace type: codeWorkspace | tmuxinator")
	workspaceAddCmd.Flags().StringVar(&workspaceAddName, "name", "", "human-friendly display name (defaults to key)")
	workspaceAddCmd.Flags().StringVar(&workspaceAddFile, "file", "", "override the file path (else nearest common ancestor for codeWorkspace, ~/.tmuxinator/<key>.yml for tmuxinator)")
	workspaceAddCmd.Flags().StringVar(&workspaceAddLayout, "layout", "", "tmuxinator layout: windowsPerRepo | splitPanes")
	workspaceAddCmd.Flags().StringArrayVar(&workspaceAddMembers, "member", nil, "member clone as <source-key>/<repo-key>; repeatable")
	workspaceAddCmd.MarkFlagRequired("type")

	workspaceGenerateCmd.Flags().BoolVar(&workspaceGenerateDryRun, "dry-run", false, "print the generated content without writing it")

	workspaceCmd.AddCommand(
		workspaceListCmd,
		workspaceShowCmd,
		workspaceAddCmd,
		workspaceDeleteCmd,
		workspaceAddMemberCmd,
		workspaceDeleteMemberCmd,
		workspaceGenerateCmd,
		workspaceOpenCmd,
	)
}
