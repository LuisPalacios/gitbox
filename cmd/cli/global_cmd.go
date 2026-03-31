package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/spf13/cobra"
)

var globalCmd = &cobra.Command{
	Use:   "global",
	Short: "Manage global settings and configuration",
}

// --- global show ---

var globalShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Show global settings",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		data, _ := json.MarshalIndent(cfg.Global, "", "    ")
		fmt.Fprintln(os.Stdout, string(data))
		return nil
	},
}

// --- global update ---

var (
	globalFolder    string
	globalGCMHelper string
	globalGCMStore  string
	globalSSHFolder string
)

var globalUpdateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update global settings",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}

		if cmd.Flags().Changed("folder") {
			cfg.Global.Folder = globalFolder
		}

		// GCM settings.
		if cmd.Flags().Changed("gcm-helper") || cmd.Flags().Changed("gcm-credential-store") {
			if cfg.Global.CredentialGCM == nil {
				cfg.Global.CredentialGCM = &config.GCMGlobal{}
			}
			if cmd.Flags().Changed("gcm-helper") {
				cfg.Global.CredentialGCM.Helper = globalGCMHelper
			}
			if cmd.Flags().Changed("gcm-credential-store") {
				cfg.Global.CredentialGCM.CredentialStore = globalGCMStore
			}
		}

		// SSH settings.
		if cmd.Flags().Changed("ssh-folder") {
			if cfg.Global.CredentialSSH == nil {
				cfg.Global.CredentialSSH = &config.SSHGlobal{}
			}
			cfg.Global.CredentialSSH.SSHFolder = globalSSHFolder
		}

		return saveConfig(cfg)
	},
}

// --- global config (subgroup) ---

var globalConfigCmd = &cobra.Command{
	Use:   "config",
	Short: "Show or locate the configuration file",
}

var globalConfigShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Display the full configuration",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfgPath := resolveConfigPath()
		if cfgPath == "" {
			cfgPath = config.DefaultV2Path()
		}

		cfg, err := config.Load(cfgPath)
		if err != nil {
			return fmt.Errorf("loading config from %s: %w", cfgPath, err)
		}

		data, err := json.MarshalIndent(cfg, "", "    ")
		if err != nil {
			return fmt.Errorf("marshalling config: %w", err)
		}

		fmt.Fprintln(os.Stdout, string(data))
		return nil
	},
}

var globalConfigPathCmd = &cobra.Command{
	Use:   "path",
	Short: "Print the configuration file path and status",
	Run: func(cmd *cobra.Command, args []string) {
		cfgPath := resolveConfigPath()
		if cfgPath == "" {
			cfgPath = config.DefaultV2Path()
		}
		fmt.Println(cfgPath)
		if _, err := os.Stat(cfgPath); err != nil {
			fmt.Fprintln(os.Stderr, "(file does not exist — run 'gitbox init' to create it)")
		}
	},
}

func init() {
	globalCmd.AddCommand(globalShowCmd)
	globalCmd.AddCommand(globalUpdateCmd)
	globalCmd.AddCommand(globalConfigCmd)

	globalConfigCmd.AddCommand(globalConfigShowCmd)
	globalConfigCmd.AddCommand(globalConfigPathCmd)

	globalUpdateCmd.Flags().StringVar(&globalFolder, "folder", "", "root folder for all git clones")
	globalUpdateCmd.Flags().StringVar(&globalGCMHelper, "gcm-helper", "", "GCM credential helper (typically 'manager')")
	globalUpdateCmd.Flags().StringVar(&globalGCMStore, "gcm-credential-store", "", "GCM credential store (wincredman|keychain|secretservice)")
	globalUpdateCmd.Flags().StringVar(&globalSSHFolder, "ssh-folder", "", "SSH config directory (default: ~/.ssh)")
}
