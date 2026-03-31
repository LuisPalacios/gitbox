package credential

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/LuisPalacios/gitbox/pkg/git"
)

// SSHKeyPath returns the convention-based key file path for an account.
// Convention: <sshFolder>/gitbox-<accountKey>-sshkey
func SSHKeyPath(sshFolder, accountKey string) string {
	return filepath.Join(sshFolder, fmt.Sprintf("gitbox-%s-sshkey", accountKey))
}

// SSHHostAlias returns the convention-based SSH host alias for an account.
// Convention: gitbox-<accountKey>
func SSHHostAlias(accountKey string) string {
	return fmt.Sprintf("gitbox-%s", accountKey)
}

// FindSSHKey checks if an SSH key file exists for the given host.
// It first checks ~/.ssh/config for the IdentityFile configured for the host.
// Then tries the gitbox convention: <sshFolder>/gitbox-<accountKey>-sshkey
// Then falls back to legacy convention: <sshFolder>/<host>-sshkey
func FindSSHKey(sshFolder, host, keyType string) (string, error) {
	// First try: extract IdentityFile from ~/.ssh/config
	if identity := findIdentityFileInConfig(sshFolder, host); identity != "" {
		if strings.HasPrefix(identity, "~") {
			home, err := os.UserHomeDir()
			if err == nil {
				identity = filepath.Join(home, identity[1:])
			}
		}
		identity = filepath.Clean(identity)
		if _, err := os.Stat(identity); err == nil {
			return identity, nil
		}
		return "", fmt.Errorf("key configured in ~/.ssh/config but file not found: %s", identity)
	}

	// Second try: gitbox convention (host IS the alias like gitbox-github-AgorastisMesaio)
	gitboxKey := filepath.Join(sshFolder, host+"-sshkey")
	if _, err := os.Stat(gitboxKey); err == nil {
		return gitboxKey, nil
	}

	// Third try: legacy convention
	legacyKey := filepath.Join(sshFolder, fmt.Sprintf("id_%s_%s", keyType, host))
	if _, err := os.Stat(legacyKey); err == nil {
		return legacyKey, nil
	}

	return "", fmt.Errorf("not found")
}

// findIdentityFileInConfig parses ~/.ssh/config and returns the IdentityFile
// for the given Host alias. Returns empty string if not found.
func findIdentityFileInConfig(sshFolder, host string) string {
	configPath := filepath.Join(sshFolder, "config")
	f, err := os.Open(configPath)
	if err != nil {
		return ""
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	inHost := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		lower := strings.ToLower(line)
		if strings.HasPrefix(lower, "host ") && !strings.HasPrefix(lower, "hostname") {
			inHost = containsWord(line[5:], host)
			continue
		}
		if inHost && strings.HasPrefix(lower, "identityfile") {
			parts := strings.SplitN(line, " ", 2)
			if len(parts) == 2 {
				return strings.TrimSpace(parts[1])
			}
		}
	}
	return ""
}

// FindSSHConfigEntry checks if ~/.ssh/config contains a Host entry matching the alias.
func FindSSHConfigEntry(sshFolder, host string) (bool, error) {
	configPath := filepath.Join(sshFolder, "config")
	f, err := os.Open(configPath)
	if err != nil {
		return false, fmt.Errorf("cannot read %s: %w", configPath, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		// Match "Host <alias>" (case-insensitive Host keyword).
		if strings.EqualFold(line, "Host "+host) ||
			strings.HasPrefix(strings.ToLower(line), "host ") && containsWord(line[5:], host) {
			return true, nil
		}
	}
	return false, nil
}

// RemoveSSHConfigEntry removes a Host block (including preceding comment lines)
// from ~/.ssh/config. Idempotent — returns nil if the entry doesn't exist.
func RemoveSSHConfigEntry(sshFolder, host string) error {
	configPath := filepath.Join(sshFolder, "config")
	data, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}

	lines := strings.Split(string(data), "\n")
	var result []string
	skip := false
	// Buffer comment lines that may precede the Host block.
	var commentBuf []string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Track comment lines as potential header for a Host block.
		if strings.HasPrefix(trimmed, "#") || trimmed == "" {
			if skip {
				continue // Still inside the block to remove.
			}
			commentBuf = append(commentBuf, line)
			continue
		}

		lower := strings.ToLower(trimmed)
		if strings.HasPrefix(lower, "host ") && !strings.HasPrefix(lower, "hostname") {
			if containsWord(trimmed[5:], host) {
				// Found the target — skip this block and discard buffered comments.
				skip = true
				commentBuf = nil
				continue
			}
			// Different Host block — flush buffered comments and stop skipping.
			skip = false
			result = append(result, commentBuf...)
			commentBuf = nil
			result = append(result, line)
			continue
		}

		if skip {
			continue // Inside the block to remove (HostName, User, IdentityFile, etc.).
		}

		// Regular config line outside any Host block.
		result = append(result, commentBuf...)
		commentBuf = nil
		result = append(result, line)
	}

	// Flush remaining comments if not skipping.
	if !skip {
		result = append(result, commentBuf...)
	}

	out := strings.Join(result, "\n")
	// Ensure file ends with a newline.
	if out != "" && !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	return os.WriteFile(configPath, []byte(out), 0o600)
}

// containsWord checks if a space-separated string contains the given word.
func containsWord(s, word string) bool {
	for _, w := range strings.Fields(s) {
		if w == word {
			return true
		}
	}
	return false
}

// TestSSHConnection tests SSH connectivity by running ssh -T.
// Uses -F to read the SSH config from <sshFolder>/config, ensuring full isolation
// from the user's ~/.ssh/config.
// Returns the greeting message (e.g., "Hi MyUser!") or an error.
func TestSSHConnection(sshFolder, host string) (string, error) {
	configFile := filepath.Join(sshFolder, "config")
	cmd := exec.Command("ssh", "-T",
		"-F", configFile,
		"-o", "ConnectTimeout=5",
		"-o", "StrictHostKeyChecking=accept-new",
		host)
	git.HideWindow(cmd)
	out, err := cmd.CombinedOutput()
	output := strings.TrimSpace(string(out))

	// Many providers return exit code 1 with a greeting (e.g., GitHub: "Hi user!").
	// We consider it a success if there's output and no connection error.
	if output != "" && !strings.Contains(output, "Connection refused") &&
		!strings.Contains(output, "Connection timed out") &&
		!strings.Contains(output, "Permission denied") &&
		!strings.Contains(output, "Host key verification failed") &&
		!strings.Contains(output, "Could not resolve hostname") &&
		!strings.Contains(output, "No such file or directory") &&
		!strings.Contains(output, "No such identity") {
		return output, nil
	}

	if err != nil {
		return "", fmt.Errorf("SSH connection failed: %s", output)
	}
	return output, nil
}

// SSHConfigGuide returns a suggested ~/.ssh/config entry for the account.
func SSHConfigGuide(host, hostname, keyFile string) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("  Host %s\n", host))
	sb.WriteString(fmt.Sprintf("      HostName %s\n", hostname))
	sb.WriteString("      User git\n")
	sb.WriteString(fmt.Sprintf("      IdentityFile %s\n", keyFile))
	sb.WriteString("      IdentitiesOnly yes\n")
	return sb.String()
}

// SSHConfigEntryOpts holds the parameters for writing an SSH config entry.
type SSHConfigEntryOpts struct {
	Host     string // Host alias (e.g., gitbox-github-AgorastisMesaio)
	Hostname string // Real server hostname (e.g., github.com)
	KeyFile  string // IdentityFile path (e.g., ~/.ssh/gitbox-github-AgorastisMesaio-sshkey)
	Username string // Git provider username
	Name     string // user.name for commits
	Email    string // user.email for commits
	URL      string // Provider base URL (e.g., https://github.com)
}

// WriteSSHConfigEntry appends a Host block to ~/.ssh/config.
// Creates the config file if it doesn't exist.
func WriteSSHConfigEntry(sshFolder string, opts SSHConfigEntryOpts) error {
	configPath := filepath.Join(sshFolder, "config")

	// Ensure ~/.ssh directory exists.
	if err := os.MkdirAll(sshFolder, 0o700); err != nil {
		return fmt.Errorf("creating %s: %w", sshFolder, err)
	}

	var sb strings.Builder
	sb.WriteString("\n#\n")
	sb.WriteString(fmt.Sprintf("# Generated by gitbox — https://github.com/LuisPalacios/gitbox\n"))
	sb.WriteString(fmt.Sprintf("#\n"))
	sb.WriteString(fmt.Sprintf("# Clone:  git clone %s:%s/your-repository.git\n", opts.Host, opts.Username))
	sb.WriteString(fmt.Sprintf("# Name:   git config user.name %q\n", opts.Name))
	sb.WriteString(fmt.Sprintf("# Email:  git config user.email %q\n", opts.Email))
	sb.WriteString(fmt.Sprintf("# Remote: git remote set-url origin %s:%s/your-repository.git\n", opts.Host, opts.Username))
	sb.WriteString(fmt.Sprintf("#\n"))
	sb.WriteString(fmt.Sprintf("Host %s\n", opts.Host))
	sb.WriteString(fmt.Sprintf("    HostName %s\n", opts.Hostname))
	sb.WriteString("    User git\n")
	sb.WriteString(fmt.Sprintf("    IdentityFile %s\n", opts.KeyFile))
	sb.WriteString("    IdentitiesOnly yes\n")

	f, err := os.OpenFile(configPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("opening %s: %w", configPath, err)
	}
	defer f.Close()

	if _, err := f.WriteString(sb.String()); err != nil {
		return fmt.Errorf("writing to %s: %w", configPath, err)
	}
	return nil
}

// GenerateSSHKey generates an SSH key pair using ssh-keygen.
// Key path: <sshFolder>/gitbox-<accountKey>-sshkey
// The key comment is set to "gitbox-<local-hostname>" for easy identification.
// Returns the private key path.
func GenerateSSHKey(sshFolder, accountKey, keyType string) (string, error) {
	// Check ssh-keygen is available.
	if _, err := exec.LookPath("ssh-keygen"); err != nil {
		return "", fmt.Errorf("ssh-keygen not found on PATH — install OpenSSH to generate keys")
	}

	keyPath := SSHKeyPath(sshFolder, accountKey)

	// Don't overwrite existing keys.
	if _, err := os.Stat(keyPath); err == nil {
		return keyPath, nil
	}

	// Ensure directory exists.
	if err := os.MkdirAll(sshFolder, 0o700); err != nil {
		return "", fmt.Errorf("creating %s: %w", sshFolder, err)
	}

	localHost, _ := os.Hostname()
	if localHost == "" {
		localHost = "unknown"
	}
	comment := fmt.Sprintf("gitbox-%s", localHost)
	cmd := exec.Command("ssh-keygen", "-q", "-t", keyType, "-f", keyPath, "-C", comment, "-N", "")
	git.HideWindow(cmd)
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("ssh-keygen failed: %w", err)
	}
	return keyPath, nil
}

// ReadPublicKey reads the .pub file and returns its content.
func ReadPublicKey(keyPath string) (string, error) {
	pubPath := keyPath + ".pub"
	data, err := os.ReadFile(pubPath)
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", pubPath, err)
	}
	return strings.TrimSpace(string(data)), nil
}

// SSHPublicKeyURL returns the provider-specific URL to add SSH public keys.
func SSHPublicKeyURL(providerName, baseURL string) string {
	base := strings.TrimRight(baseURL, "/")
	switch providerName {
	case "github":
		return base + "/settings/keys"
	case "gitlab":
		return base + "/-/user_settings/ssh_keys"
	case "gitea", "forgejo":
		return base + "/user/settings/keys"
	case "bitbucket":
		return base + "/account/settings/ssh-keys/"
	default:
		return base + " (add your SSH public key in your account settings)"
	}
}
