package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/credential"
	"github.com/LuisPalacios/gitbox/pkg/git"
	"github.com/LuisPalacios/gitbox/pkg/provider"
	"github.com/spf13/cobra"
)

// --- account credential (parent) ---

var credentialCmd = &cobra.Command{
	Use:   "credential",
	Short: "Manage account credentials (setup, verify, del <account-key>)",
	Long: `Add, verify, or remove credentials for an account.

The command reads the account's default_credential_type (gcm, ssh, or token)
and performs the appropriate action for that type.`,
}

// --- credential setup ---

var credentialSetupForceToken bool

var credentialSetupCmd = &cobra.Command{
	Use:   "setup <account-key>",
	Short: "Set up or fix credential for an account",
	Long: `Configures the credential for the account based on its default_credential_type:

  token: Prompts for a PAT and stores it in the OS credential store.
  gcm:   Verifies GCM is configured (browser auth on first git operation).
  ssh:   Validates SSH config fields and checks ~/.ssh/config entry.

Use --token to store a PAT regardless of credential type (useful for SSH accounts
that need discovery/API access).`,
	Args: cobra.ExactArgs(1),
	RunE: runCredentialAdd,
}

// --- credential verify ---

var credentialVerifyCmd = &cobra.Command{
	Use:   "verify <account-key>",
	Short: "Verify credential works for an account",
	Long: `Validates that the credential for the account is properly configured
and can authenticate against the provider.`,
	Args: cobra.ExactArgs(1),
	RunE: runCredentialVerify,
}

// --- credential del ---

var credentialDelCmd = &cobra.Command{
	Use:   "del <account-key>",
	Short: "Remove credential for an account",
	Args:  cobra.ExactArgs(1),
	RunE:  runCredentialDel,
}

func init() {
	credentialSetupCmd.Flags().BoolVar(&credentialSetupForceToken, "token", false, "store a PAT (useful for SSH accounts that need discovery)")
	credentialCmd.AddCommand(credentialSetupCmd)
	credentialCmd.AddCommand(credentialVerifyCmd)
	credentialCmd.AddCommand(credentialDelCmd)
}

// --- add implementations ---

func runCredentialAdd(cmd *cobra.Command, args []string) error {
	accountKey := args[0]
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	acct, ok := cfg.GetAccountByKey(accountKey)
	if !ok {
		return fmt.Errorf("account %q not found", accountKey)
	}

	// --token flag: store a PAT regardless of credential type.
	if credentialSetupForceToken {
		return credentialAddToken(acct, accountKey)
	}

	credType := acct.DefaultCredentialType
	if credType == "" {
		return fmt.Errorf("account %q has no default_credential_type set\nUpdate it with: gitbox account update %s --default-credential-type <gcm|ssh|token>", accountKey, accountKey)
	}

	// Ensure global .gitconfig has credential sections for all GCM accounts.
	credential.EnsureGlobalGCMConfig(cfg.Global)

	switch credType {
	case "token":
		return credentialAddToken(acct, accountKey)
	case "gcm":
		return credentialAddGCM(acct, accountKey)
	case "ssh":
		return credentialAddSSH(cfg, acct, accountKey)
	default:
		return fmt.Errorf("unknown credential type %q for account %q", credType, accountKey)
	}
}

func credentialAddToken(acct config.Account, accountKey string) error {
	var token string
	stat, _ := os.Stdin.Stat()
	if (stat.Mode() & os.ModeCharDevice) == 0 {
		// Piped input.
		scanner := bufio.NewScanner(os.Stdin)
		if scanner.Scan() {
			token = strings.TrimSpace(scanner.Text())
		}
	} else {
		// Show quick guide before prompting.
		fmt.Fprintf(os.Stderr, "Create a PAT at your provider and paste it below.\n")
		fmt.Fprintf(os.Stderr, "Guide: https://github.com/LuisPalacios/gitbox/blob/main/docs/credentials.md\n\n")
		fmt.Fprintf(os.Stderr, "%s\n\n", provider.TokenCreationURL(acct.Provider, acct.URL))
		fmt.Fprintf(os.Stderr, "Enter PAT for %s (%s@%s): ", accountKey, acct.Username, acct.URL)
		scanner := bufio.NewScanner(os.Stdin)
		if scanner.Scan() {
			token = strings.TrimSpace(scanner.Text())
		}
	}

	if token == "" {
		fmt.Fprintln(os.Stderr, provider.TokenSetupGuide(acct.Provider, acct.URL, accountKey))
		return fmt.Errorf("no token provided")
	}

	if err := credential.StoreToken(accountKey, token); err != nil {
		return fmt.Errorf("storing token: %w", err)
	}

	fmt.Printf("Token stored for %s in credential file\n", accountKey)
	return nil
}

func credentialAddGCM(acct config.Account, accountKey string) error {
	// Check if GCM already has a stored credential.
	_, _, err := credential.ResolveGCMToken(acct.URL, acct.Username)
	if err == nil {
		fmt.Printf("GCM credential already stored for %s@%s.\n", acct.Username, hostnameFromURL(acct.URL))
		return nil
	}

	// No credential found — trigger interactive git credential fill.
	// With terminal access, GCM opens the browser for OAuth login.
	// We capture the output and then approve it so git stores it.
	host := hostnameFromURL(acct.URL)
	fmt.Fprintf(os.Stderr, "No GCM credential found for %s@%s.\n", acct.Username, host)
	fmt.Fprintf(os.Stderr, "Opening browser for authentication...\n\n")

	input := fmt.Sprintf("protocol=https\nhost=%s\nusername=%s\n\n", host, acct.Username)
	homeDir, _ := os.UserHomeDir()
	fillCmd := exec.Command(git.GitBin(), "credential", "fill")
	fillCmd.Dir = homeDir // Avoid repo-local .git/config credential overrides.
	fillCmd.Env = git.Environ() // Homebrew PATH for macOS — do not remove.
	fillCmd.Stdin = strings.NewReader(input)
	fillCmd.Stderr = os.Stderr // GCM needs stderr for browser prompts
	out, err := fillCmd.Output()
	if err != nil {
		return fmt.Errorf("GCM authentication failed for %s@%s", acct.Username, host)
	}

	// Approve the credential so git stores it persistently.
	approveCmd := exec.Command(git.GitBin(), "credential", "approve")
	approveCmd.Dir = homeDir // Avoid repo-local .git/config credential overrides.
	approveCmd.Env = git.Environ() // Homebrew PATH for macOS — do not remove.
	approveCmd.Stdin = strings.NewReader(string(out))
	_ = approveCmd.Run()

	// Verify it was stored.
	_, _, err = credential.ResolveGCMToken(acct.URL, acct.Username)
	if err != nil {
		return fmt.Errorf("GCM authentication did not complete — no credential stored for %s@%s", acct.Username, host)
	}

	fmt.Printf("+ GCM credential stored for %s@%s.\n", acct.Username, host)

	// Check API access — GCM OAuth tokens work for GitHub but not for
	// self-hosted Forgejo/Gitea which need a real PAT for API access.
	needPAT := false
	token, _, apiErr := credential.ResolveAPIToken(acct, accountKey)
	if apiErr != nil {
		needPAT = true
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := provider.TestAuth(ctx, acct.Provider, acct.URL, token, acct.Username); err != nil {
			needPAT = true
		} else {
			fmt.Printf("+ API access: OK\n")
		}
	}

	if needPAT {
		fmt.Printf("\n~ API access requires a PAT for %s.\n", host)
		fmt.Printf("  Would you like to store one now? [y/N]: ")
		scanner := bufio.NewScanner(os.Stdin)
		if scanner.Scan() && strings.EqualFold(strings.TrimSpace(scanner.Text()), "y") {
			fmt.Println()
			if err := credentialAddToken(acct, accountKey); err != nil {
				fmt.Printf("x PAT setup failed: %v\n", err)
			}
		}
	}

	return nil
}

func credentialAddSSH(cfg *config.Config, acct config.Account, accountKey string) error {
	// Derive SSH host alias and key type.
	hostAlias := credential.SSHHostAlias(accountKey)
	keyType := "ed25519"
	if acct.SSH != nil && acct.SSH.KeyType != "" {
		keyType = acct.SSH.KeyType
	}

	hostname := hostnameFromURL(acct.URL)
	if acct.SSH != nil && acct.SSH.Hostname != "" {
		hostname = acct.SSH.Hostname
	}

	sshFolder := credential.SSHFolder(cfg)
	keyPath := credential.SSHKeyPath(sshFolder, accountKey)

	// Step 1: Ensure SSH directory exists.
	if err := os.MkdirAll(sshFolder, 0o700); err != nil {
		return fmt.Errorf("creating %s: %w", sshFolder, err)
	}

	// Step 2: Check/create SSH config entry.
	found, _ := credential.FindSSHConfigEntry(sshFolder, hostAlias)
	if found {
		fmt.Printf("+ SSH config: Host %s found\n", hostAlias)
	} else {
		opts := credential.SSHConfigEntryOpts{
			Host:     hostAlias,
			Hostname: hostname,
			KeyFile:  credential.SSHKeyPath(sshFolder, accountKey),
			Username: acct.Username,
			Name:     acct.Name,
			Email:    acct.Email,
			URL:      acct.URL,
		}
		if err := credential.WriteSSHConfigEntry(sshFolder, opts); err != nil {
			return fmt.Errorf("writing SSH config: %w", err)
		}
		fmt.Printf("+ SSH config: added Host %s\n", hostAlias)
	}

	// Step 3: Check/generate SSH key.
	if _, err := os.Stat(keyPath); err == nil {
		fmt.Printf("+ SSH key: %s\n", keyPath)
	} else {
		generatedPath, err := credential.GenerateSSHKey(sshFolder, accountKey, keyType)
		if err != nil {
			return err
		}
		fmt.Printf("+ SSH key: generated %s\n", generatedPath)
	}

	// Step 4: Update account SSH config in gitbox.json if needed.
	if acct.SSH == nil || acct.SSH.Host != hostAlias {
		if acct.SSH == nil {
			acct.SSH = &config.SSHConfig{}
		}
		acct.SSH.Host = hostAlias
		acct.SSH.Hostname = hostname
		acct.SSH.KeyType = keyType
		if err := cfg.UpdateAccount(accountKey, acct); err != nil {
			return err
		}
		if err := saveConfig(cfg); err != nil {
			return err
		}
	}

	// Step 5: Test SSH connection — if it fails, guide user through adding the key.
	if _, sshErr := credential.TestSSHConnection(sshFolder, hostAlias); sshErr != nil {
		pubKey, _ := credential.ReadPublicKey(keyPath)
		fmt.Printf("\nAdd this public key to your provider:\n\n")
		if pubKey != "" {
			fmt.Printf("  %s\n\n", pubKey)
		}
		fmt.Printf("Paste it at: %s\n\n", credential.SSHPublicKeyURL(acct.Provider, acct.URL))

		// Wait for user to add the key, then retry.
		fmt.Printf("Press Enter after adding the key...")
		scanner := bufio.NewScanner(os.Stdin)
		scanner.Scan()
		fmt.Println()

		if _, retryErr := credential.TestSSHConnection(sshFolder, hostAlias); retryErr != nil {
			fmt.Printf("x SSH connection: still failing — check that the key was added correctly\n")
			return fmt.Errorf("SSH setup incomplete")
		}
	}
	fmt.Printf("+ SSH connection: OK\n")

	// Step 6: Check API access — offer to store/replace PAT if missing or invalid.
	needPAT := false
	token, _, apiErr := credential.ResolveAPIToken(acct, accountKey)
	if apiErr != nil {
		needPAT = true
	} else {
		// Token exists — verify it actually works.
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := provider.TestAuth(ctx, acct.Provider, acct.URL, token, acct.Username); err != nil {
			fmt.Printf("~ API access: token is invalid or expired\n")
			// Delete the bad token.
			_ = credential.DeleteToken(accountKey)
			needPAT = true
		} else {
			fmt.Printf("+ API access: OK\n")
		}
	}

	if needPAT {
		fmt.Printf("\n~ A PAT is recommended for repo discovery.\n")
		fmt.Printf("  Would you like to store one now? [y/N]: ")
		scanner := bufio.NewScanner(os.Stdin)
		if scanner.Scan() && strings.EqualFold(strings.TrimSpace(scanner.Text()), "y") {
			fmt.Println()
			if err := credentialAddToken(acct, accountKey); err != nil {
				fmt.Printf("x PAT setup failed: %v\n", err)
			}
		}
	}

	return nil
}

// --- verify implementations ---

func runCredentialVerify(cmd *cobra.Command, args []string) error {
	accountKey := args[0]
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	acct, ok := cfg.GetAccountByKey(accountKey)
	if !ok {
		return fmt.Errorf("account %q not found", accountKey)
	}

	credType := acct.DefaultCredentialType
	if credType == "" {
		return fmt.Errorf("account %q has no default_credential_type set", accountKey)
	}

	switch credType {
	case "token":
		return credentialVerifyToken(acct, accountKey)
	case "gcm":
		return credentialVerifyGCM(acct, accountKey)
	case "ssh":
		return credentialVerifySSH(cfg, acct, accountKey)
	default:
		return fmt.Errorf("unknown credential type %q", credType)
	}
}

func credentialVerifyToken(acct config.Account, accountKey string) error {
	hasErrors := false

	// Resolve token.
	token, source, err := credential.ResolveToken(acct, accountKey)
	if err != nil {
		fmt.Printf("x Token: not found\n")
		fmt.Printf("Store with: gitbox account credential setup %s\n", accountKey)
		return fmt.Errorf("verification failed")
	}

	fmt.Printf("+ Token: OK (source: %s)\n", source)

	// Test API access.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := provider.TestAuth(ctx, acct.Provider, acct.URL, token, acct.Username); err != nil {
		fmt.Printf("x API access: %v\n", err)
		errMsg := err.Error()
		if strings.Contains(errMsg, "insufficient permissions") {
			fmt.Printf("\n  The token is valid but lacks the required scopes.\n")
			fmt.Printf("  %s\n", provider.TokenCreationURL(acct.Provider, acct.URL))
			fmt.Printf("  Then replace it: gitbox account credential setup %s\n", accountKey)
		} else if strings.Contains(errMsg, "authentication failed") {
			fmt.Printf("\n  The stored credential is not a valid PAT (it may be a password).\n")
			fmt.Printf("  Replace it with a PAT: gitbox account credential setup %s\n", accountKey)
		}
		hasErrors = true
	} else {
		fmt.Printf("+ API access: OK\n")
	}

	if hasErrors {
		return fmt.Errorf("verification completed with errors")
	}
	return nil
}

func credentialVerifyGCM(acct config.Account, accountKey string) error {
	// Resolve token via GCM (git credential fill).
	gcmToken, _, gcmErr := credential.ResolveGCMToken(acct.URL, acct.Username)
	if gcmErr != nil {
		fmt.Printf("x GCM credential: not found\n")
		return fmt.Errorf("verification failed")
	}
	fmt.Printf("+ GCM credential: OK\n")

	// Test API access — try GCM token first, then keyring PAT.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := provider.TestAuth(ctx, acct.Provider, acct.URL, gcmToken, acct.Username); err == nil {
		fmt.Printf("+ API access: OK (GCM)\n")
		return nil
	}

	// GCM token doesn't work for API (common with Forgejo/Gitea) — try keyring PAT.
	patToken, _, patErr := credential.ResolveToken(acct, accountKey)
	if patErr == nil {
		ctx2, cancel2 := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel2()
		if err := provider.TestAuth(ctx2, acct.Provider, acct.URL, patToken, acct.Username); err == nil {
			fmt.Printf("+ API access: OK (PAT)\n")
			return nil
		}
		fmt.Printf("x API access: PAT is invalid or expired\n")
	} else {
		fmt.Printf("~ API access: not available (run 'credential setup' to store a PAT for discovery)\n")
	}
	return nil
}

func credentialVerifySSH(cfg *config.Config, acct config.Account, accountKey string) error {
	hostAlias := credential.SSHHostAlias(accountKey)
	addHint := fmt.Sprintf("  Run: gitbox account credential setup %s", accountKey)

	sshFolder := credential.SSHFolder(cfg)

	keyType := "ed25519"
	if acct.SSH != nil && acct.SSH.KeyType != "" {
		keyType = acct.SSH.KeyType
	}

	// Step 1: Check SSH config entry.
	found, _ := credential.FindSSHConfigEntry(sshFolder, hostAlias)
	if !found {
		fmt.Printf("x SSH config: Host %q not found\n%s\n", hostAlias, addHint)
		return fmt.Errorf("verification failed")
	}
	fmt.Printf("+ SSH config: Host %s\n", hostAlias)

	// Step 2: Check SSH key.
	keyPath, err := credential.FindSSHKey(sshFolder, hostAlias, keyType)
	if err != nil {
		fmt.Printf("x SSH key: not found\n%s\n", addHint)
		return fmt.Errorf("verification failed")
	}
	fmt.Printf("+ SSH key: %s\n", keyPath)

	// Step 3: Test SSH connection.
	if _, sshErr := credential.TestSSHConnection(sshFolder, hostAlias); sshErr != nil {
		fmt.Printf("x SSH connection: key not registered at %s\n", hostnameFromURL(acct.URL))
		fmt.Printf("  Add public key at: %s\n", credential.SSHPublicKeyURL(acct.Provider, acct.URL))
		return fmt.Errorf("verification failed")
	}
	fmt.Printf("+ SSH connection: OK\n")


	// Step 4: Check API access (optional — informational only).
	token, source, err := credential.ResolveAPIToken(acct, accountKey)
	if err != nil {
		fmt.Printf("~ API access: not available (run 'credential setup' to store a PAT for discovery)\n")
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := provider.TestAuth(ctx, acct.Provider, acct.URL, token, acct.Username); err != nil {
			fmt.Printf("~ API access: %v\n", err)
		} else {
			fmt.Printf("+ API access: OK (%s)\n", source)
		}
	}

	return nil
}

// --- del implementations ---

func runCredentialDel(cmd *cobra.Command, args []string) error {
	accountKey := args[0]
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	acct, ok := cfg.GetAccountByKey(accountKey)
	if !ok {
		return fmt.Errorf("account %q not found", accountKey)
	}

	credType := acct.DefaultCredentialType
	if credType == "" {
		return fmt.Errorf("account %q has no default_credential_type set", accountKey)
	}

	switch credType {
	case "token":
		return credentialDelToken(acct, accountKey)
	case "gcm":
		return credentialDelGCM(acct, accountKey)
	case "ssh":
		return credentialDelSSH(cfg, acct, accountKey)
	default:
		return fmt.Errorf("unknown credential type %q", credType)
	}
}

func credentialDelToken(acct config.Account, accountKey string) error {
	if err := credential.DeleteToken(accountKey); err != nil {
		return fmt.Errorf("removing token: %w", err)
	}
	fmt.Printf("Token removed for %s from credential file\n", accountKey)

	if err := credential.RemoveCredentialFile(accountKey); err == nil {
		fmt.Printf("+ Credential store file removed\n")
	}
	return nil
}

func credentialDelGCM(acct config.Account, accountKey string) error {
	host := hostnameFromURL(acct.URL)
	input := fmt.Sprintf("protocol=https\nhost=%s\nusername=%s\n", host, acct.Username)

	homeDir, _ := os.UserHomeDir()
	cmd := exec.Command(git.GitBin(), "credential", "reject")
	cmd.Dir = homeDir // Avoid repo-local .git/config credential overrides.
	cmd.Env = git.Environ() // Homebrew PATH for macOS — do not remove.
	cmd.Stdin = strings.NewReader(input)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git credential reject failed: %w", err)
	}

	fmt.Printf("GCM credential removed for %s@%s\n", acct.Username, host)
	return nil
}

func credentialDelSSH(cfg *config.Config, acct config.Account, accountKey string) error {
	hostAlias := credential.SSHHostAlias(accountKey)

	sshFolder := credential.SSHFolder(cfg)

	// 1. Delete SSH key files (idempotent).
	keyPath := credential.SSHKeyPath(sshFolder, accountKey)
	if err := os.Remove(keyPath); err == nil {
		fmt.Printf("+ Removed SSH key: %s\n", keyPath)
	}
	if err := os.Remove(keyPath + ".pub"); err == nil {
		fmt.Printf("+ Removed SSH public key: %s.pub\n", keyPath)
	}

	// 2. Remove ~/.ssh/config entry (idempotent).
	if err := credential.RemoveSSHConfigEntry(sshFolder, hostAlias); err == nil {
		fmt.Printf("+ Removed Host %s from ~/.ssh/config\n", hostAlias)
	}

	// 3. Delete PAT from keyring if stored (idempotent).
	if err := credential.DeleteToken(accountKey); err == nil {
		fmt.Printf("+ Removed PAT from credential file\n")
	}

	// 4. Tell user to remove the public key from the provider.
	fmt.Printf("\n~ Remember to remove the SSH public key from your provider:\n")
	fmt.Printf("  %s\n", credential.SSHPublicKeyURL(acct.Provider, acct.URL))

	return nil
}

// --- helpers ---

func maskToken(token string) string {
	if len(token) < 10 {
		return "****"
	}
	return token[:4] + "..." + token[len(token)-4:]
}

