package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/spf13/cobra"
)

var accountCmd = &cobra.Command{
	Use:   "account",
	Short: "Manage accounts",
}

// --- account list ---

var accountListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all accounts",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}

		if jsonOutput {
			data, _ := json.MarshalIndent(cfg.Accounts, "", "    ")
			fmt.Fprintln(os.Stdout, string(data))
			return nil
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintf(w, "ACCOUNT\tPROVIDER\tURL\tUSERNAME\tDEFAULT CRED\n")
		fmt.Fprintf(w, "───────\t────────\t───\t────────\t────────────\n")
		for key, acct := range cfg.Accounts {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", key, acct.Provider, acct.URL, acct.Username, acct.DefaultCredentialType)
		}
		return w.Flush()
	},
}

// --- account show ---

var accountShowCmd = &cobra.Command{
	Use:   "show <account-key>",
	Short: "Show account details",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}

		acct, ok := cfg.GetAccountByKey(args[0])
		if !ok {
			return fmt.Errorf("account %q not found", args[0])
		}

		data, _ := json.MarshalIndent(acct, "", "    ")
		fmt.Fprintln(os.Stdout, string(data))
		return nil
	},
}

// --- account add ---

var (
	// Core fields.
	acctProvider  string
	acctURL       string
	acctUsername   string
	acctName      string
	acctEmail     string
	acctDefBranch string
	acctDefCred   string

	// SSH fields.
	acctSSHHost     string
	acctSSHHostname string
	acctSSHKeyType  string

	// GCM fields.
	acctGCMProvider    string
	acctGCMUseHTTPPath string

	// Token fields.
	acctTokenEnvVar string
)

var accountAddCmd = &cobra.Command{
	Use:   "add <account-key>",
	Short: "Add a new account",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadOrCreateConfig()
		if err != nil {
			return err
		}

		// Default branch: "main" if not specified.
		defBranch := acctDefBranch
		if defBranch == "" {
			defBranch = "main"
		}

		// Default credential type: "gcm" if not specified.
		defCred := acctDefCred
		if defCred == "" {
			defCred = "gcm"
		}

		acct := config.Account{
			Provider:              acctProvider,
			URL:                   acctURL,
			Username:              acctUsername,
			Name:                  acctName,
			Email:                 acctEmail,
			DefaultBranch:         defBranch,
			DefaultCredentialType: defCred,
		}

		// --- GCM config ---
		gcmExplicit := cmd.Flags().Changed("gcm-provider") || cmd.Flags().Changed("gcm-useHttpPath")
		if gcmExplicit || defCred == "gcm" {
			gcmProv := acctGCMProvider
			if gcmProv == "" {
				gcmProv = inferGCMProvider(acctProvider)
			}
			useHTTP := acctGCMUseHTTPPath == "true"
			acct.GCM = &config.GCMConfig{
				Provider:    gcmProv,
				UseHTTPPath: useHTTP,
			}
		}

		// --- SSH config ---
		sshExplicit := cmd.Flags().Changed("ssh-host") || cmd.Flags().Changed("ssh-hostname") || cmd.Flags().Changed("ssh-key-type")
		if sshExplicit || defCred == "ssh" {
			sshHost := acctSSHHost
			sshHostname := acctSSHHostname
			sshKeyType := acctSSHKeyType

			// Auto-derive hostname from URL if not provided.
			if sshHostname == "" {
				sshHostname = hostnameFromURL(acctURL)
			}
			// ssh-host and ssh-key-type are mandatory when SSH config is being created.
			if sshHost == "" {
				return fmt.Errorf("--ssh-host is required for SSH accounts (e.g., --ssh-host gh-%s)\nIt maps to the Host entry in ~/.ssh/config for multi-account SSH", acctUsername)
			}
			if sshKeyType == "" {
				return fmt.Errorf("--ssh-key-type is required for SSH accounts (e.g., --ssh-key-type ed25519)")
			}

			acct.SSH = &config.SSHConfig{
				Host:     sshHost,
				Hostname: sshHostname,
				KeyType:  sshKeyType,
			}
		}

		// --- Token config ---
		tokenExplicit := cmd.Flags().Changed("token-env-var")
		if tokenExplicit || defCred == "token" {
			acct.Token = &config.TokenConfig{}
			if cmd.Flags().Changed("token-env-var") {
				acct.Token.EnvVar = acctTokenEnvVar
			}
		}

		if err := cfg.AddAccount(args[0], acct); err != nil {
			return err
		}

		return saveConfig(cfg)
	},
}

// --- account update ---

var accountUpdateCmd = &cobra.Command{
	Use:   "update <account-key>",
	Short: "Update an existing account",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}

		acct, ok := cfg.GetAccountByKey(args[0])
		if !ok {
			return fmt.Errorf("account %q not found", args[0])
		}

		// Core fields — apply only what was explicitly set.
		if cmd.Flags().Changed("provider") {
			acct.Provider = acctProvider
		}
		if cmd.Flags().Changed("url") {
			acct.URL = acctURL
		}
		if cmd.Flags().Changed("username") {
			acct.Username = acctUsername
		}
		if cmd.Flags().Changed("name") {
			acct.Name = acctName
		}
		if cmd.Flags().Changed("email") {
			acct.Email = acctEmail
		}
		if cmd.Flags().Changed("default-branch") {
			acct.DefaultBranch = acctDefBranch
		}
		if cmd.Flags().Changed("default-credential-type") {
			acct.DefaultCredentialType = acctDefCred
		}

		// SSH fields.
		if cmd.Flags().Changed("ssh-host") || cmd.Flags().Changed("ssh-hostname") || cmd.Flags().Changed("ssh-key-type") {
			if acct.SSH == nil {
				acct.SSH = &config.SSHConfig{}
			}
			if cmd.Flags().Changed("ssh-host") {
				acct.SSH.Host = acctSSHHost
			}
			if cmd.Flags().Changed("ssh-hostname") {
				acct.SSH.Hostname = acctSSHHostname
			}
			if cmd.Flags().Changed("ssh-key-type") {
				acct.SSH.KeyType = acctSSHKeyType
			}
		}

		// GCM fields.
		if cmd.Flags().Changed("gcm-provider") || cmd.Flags().Changed("gcm-useHttpPath") {
			if acct.GCM == nil {
				acct.GCM = &config.GCMConfig{}
			}
			if cmd.Flags().Changed("gcm-provider") {
				acct.GCM.Provider = acctGCMProvider
			}
			if cmd.Flags().Changed("gcm-useHttpPath") {
				acct.GCM.UseHTTPPath = acctGCMUseHTTPPath == "true"
			}
		}

		// Token fields.
		if cmd.Flags().Changed("token-env-var") {
			if acct.Token == nil {
				acct.Token = &config.TokenConfig{}
			}
			acct.Token.EnvVar = acctTokenEnvVar
		}

		if err := cfg.UpdateAccount(args[0], acct); err != nil {
			return err
		}

		return saveConfig(cfg)
	},
}

// --- account delete ---

var accountDeleteCmd = &cobra.Command{
	Use:   "delete <account-key>",
	Short: "Delete an account",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}

		if err := cfg.DeleteAccount(args[0]); err != nil {
			return err
		}

		return saveConfig(cfg)
	},
}

func init() {
	accountCmd.AddCommand(accountListCmd)
	accountCmd.AddCommand(accountShowCmd)
	accountCmd.AddCommand(accountAddCmd)
	accountCmd.AddCommand(accountUpdateCmd)
	accountCmd.AddCommand(accountDeleteCmd)
	accountCmd.AddCommand(accountDiscoverCmd)
	accountCmd.AddCommand(credentialCmd)

	// --- Flags for add ---
	accountAddCmd.Flags().StringVar(&acctProvider, "provider", "", "provider type (github, gitlab, gitea, forgejo, bitbucket, generic)")
	accountAddCmd.Flags().StringVar(&acctURL, "url", "", "server URL (e.g., https://github.com)")
	accountAddCmd.Flags().StringVar(&acctUsername, "username", "", "account username")
	accountAddCmd.Flags().StringVar(&acctName, "name", "", "git user.name")
	accountAddCmd.Flags().StringVar(&acctEmail, "email", "", "git user.email")
	accountAddCmd.Flags().StringVar(&acctDefBranch, "default-branch", "", "default branch (default: main)")
	accountAddCmd.Flags().StringVar(&acctDefCred, "default-credential-type", "", "default credential type (auto-detected from global)")
	// SSH
	accountAddCmd.Flags().StringVar(&acctSSHHost, "ssh-host", "", "SSH config Host alias (e.g., gh-MyGitHubUser)")
	accountAddCmd.Flags().StringVar(&acctSSHHostname, "ssh-hostname", "", "SSH server hostname (default: derived from --url)")
	accountAddCmd.Flags().StringVar(&acctSSHKeyType, "ssh-key-type", "", "SSH key type (e.g., ed25519, rsa)")
	// GCM
	accountAddCmd.Flags().StringVar(&acctGCMProvider, "gcm-provider", "", "GCM provider hint (default: derived from --provider)")
	accountAddCmd.Flags().StringVar(&acctGCMUseHTTPPath, "gcm-useHttpPath", "false", "GCM: scope credentials by HTTP path (true|false)")
	// Token
	accountAddCmd.Flags().StringVar(&acctTokenEnvVar, "token-env-var", "", "custom env var name for token (default: GITBOX_TOKEN_<KEY>)")
	// Required
	accountAddCmd.MarkFlagRequired("provider")
	accountAddCmd.MarkFlagRequired("url")
	accountAddCmd.MarkFlagRequired("username")
	accountAddCmd.MarkFlagRequired("name")
	accountAddCmd.MarkFlagRequired("email")

	// --- Flags for update ---
	accountUpdateCmd.Flags().StringVar(&acctProvider, "provider", "", "provider type")
	accountUpdateCmd.Flags().StringVar(&acctURL, "url", "", "server URL")
	accountUpdateCmd.Flags().StringVar(&acctUsername, "username", "", "username")
	accountUpdateCmd.Flags().StringVar(&acctName, "name", "", "git user.name")
	accountUpdateCmd.Flags().StringVar(&acctEmail, "email", "", "git user.email")
	accountUpdateCmd.Flags().StringVar(&acctDefBranch, "default-branch", "", "default branch")
	accountUpdateCmd.Flags().StringVar(&acctDefCred, "default-credential-type", "", "default credential type")
	// SSH
	accountUpdateCmd.Flags().StringVar(&acctSSHHost, "ssh-host", "", "SSH Host alias")
	accountUpdateCmd.Flags().StringVar(&acctSSHHostname, "ssh-hostname", "", "SSH server hostname")
	accountUpdateCmd.Flags().StringVar(&acctSSHKeyType, "ssh-key-type", "", "SSH key type (ed25519|rsa)")
	// GCM
	accountUpdateCmd.Flags().StringVar(&acctGCMProvider, "gcm-provider", "", "GCM provider hint")
	accountUpdateCmd.Flags().StringVar(&acctGCMUseHTTPPath, "gcm-useHttpPath", "", "GCM: scope credentials by HTTP path (true|false)")
	// Token
	accountUpdateCmd.Flags().StringVar(&acctTokenEnvVar, "token-env-var", "", "custom env var name for token")
}

// --- helpers ---

// inferGCMProvider maps a provider type to the GCM provider hint.
func inferGCMProvider(provider string) string {
	switch provider {
	case "github":
		return "github"
	case "gitlab":
		return "gitlab"
	case "bitbucket":
		return "bitbucket"
	default:
		return "generic"
	}
}

// hostnameFromURL extracts the hostname from a URL.
func hostnameFromURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return strings.TrimPrefix(strings.TrimPrefix(rawURL, "https://"), "http://")
	}
	return u.Hostname()
}
