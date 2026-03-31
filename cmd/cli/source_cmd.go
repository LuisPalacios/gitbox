package main

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/spf13/cobra"
)

var sourceCmd = &cobra.Command{
	Use:   "source",
	Short: "Manage sources",
}

// --- source list ---

var sourceListAccount string

var sourceListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all sources",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}

		if jsonOutput {
			data, _ := json.MarshalIndent(cfg.Sources, "", "    ")
			fmt.Fprintln(os.Stdout, string(data))
			return nil
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintf(w, "SOURCE\tACCOUNT\tFOLDER\tREPOS\n")
		fmt.Fprintf(w, "──────\t───────\t──────\t─────\n")
		for key, src := range cfg.Sources {
			if sourceListAccount != "" && src.Account != sourceListAccount {
				continue
			}
			folder := src.EffectiveFolder(key)
			fmt.Fprintf(w, "%s\t%s\t%s\t%d\n", key, src.Account, folder, len(src.Repos))
		}
		return w.Flush()
	},
}

// --- source show ---

var sourceShowCmd = &cobra.Command{
	Use:   "show <source-key>",
	Short: "Show source details",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}

		src, ok := cfg.Sources[args[0]]
		if !ok {
			return fmt.Errorf("source %q not found", args[0])
		}

		data, _ := json.MarshalIndent(src, "", "    ")
		fmt.Fprintln(os.Stdout, string(data))
		return nil
	},
}

// --- source add ---

var srcAccount string
var srcFolder  string

var sourceAddCmd = &cobra.Command{
	Use:   "add <source-key>",
	Short: "Add a new source",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadOrCreateConfig()
		if err != nil {
			return err
		}

		src := config.Source{
			Account: srcAccount,
			Folder:  srcFolder,
		}

		if err := cfg.AddSource(args[0], src); err != nil {
			return err
		}

		return saveConfig(cfg)
	},
}

// --- source delete ---

var sourceDeleteCmd = &cobra.Command{
	Use:   "delete <source-key>",
	Short: "Delete a source and all its repos",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}

		if err := cfg.DeleteSource(args[0]); err != nil {
			return err
		}

		return saveConfig(cfg)
	},
}

func init() {
	sourceCmd.AddCommand(sourceListCmd)
	sourceCmd.AddCommand(sourceShowCmd)
	sourceCmd.AddCommand(sourceAddCmd)
	sourceCmd.AddCommand(sourceDeleteCmd)

	sourceListCmd.Flags().StringVar(&sourceListAccount, "account", "", "filter by account")
	sourceAddCmd.Flags().StringVar(&srcAccount, "account", "", "account key to reference")
	sourceAddCmd.Flags().StringVar(&srcFolder, "folder", "", "custom first-level folder (optional)")
	sourceAddCmd.MarkFlagRequired("account")
}
