package credential

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/git"
)

// AgentReport compares an SSH account's key with the keys of the ssh agent
// the machine sees (#244). On a remote host that agent is the client's,
// forwarded with ssh -A, when the connection forwards it. Only SHA-256
// fingerprints are kept, never key material.
type AgentReport struct {
	// KeyFile is the account's private key file, as its Host entry names
	// it, or the gitbox convention when the entry names none.
	KeyFile string
	// KeyFileFingerprint is the fingerprint of KeyFile's public key, ""
	// when the .pub file is missing or unreadable.
	KeyFileFingerprint string
	// AgentFingerprints are the fingerprints of the agent's keys.
	AgentFingerprints []string
	// AgentForwarded: SSH_AUTH_SOCK is set, so ssh on this machine, and git
	// through it, can use the agent's keys.
	AgentForwarded bool
	// Match: the agent holds the account's key.
	Match bool
	// IdentitiesOnly: the Host entry says IdentitiesOnly yes, so ssh offers
	// only the account's key through the alias, even when the agent holds
	// others. The entry gitbox writes always says it.
	IdentitiesOnly bool
}

// agentListTimeout bounds ssh-add, which can hang on a stale socket.
const agentListTimeout = 5 * time.Second

// SSHAgentReport reports on the SSH key of accountKey in cfg and the agent
// in this process's environment.
func SSHAgentReport(ctx context.Context, cfg *config.Config, accountKey string) AgentReport {
	sshFolder := SSHFolder(cfg)
	host := SSHHostAlias(accountKey)
	if acct, ok := cfg.Accounts[accountKey]; ok && acct.SSH != nil && acct.SSH.Host != "" {
		host = acct.SSH.Host
	}
	r := AgentReport{
		KeyFile:        SSHKeyPath(sshFolder, accountKey),
		IdentitiesOnly: strings.EqualFold(hostEntryOption(sshFolder, host, "identitiesonly"), "yes"),
	}
	if identity := hostEntryOption(sshFolder, host, "identityfile"); identity != "" {
		r.KeyFile = filepath.Clean(config.ExpandTilde(strings.Trim(identity, `"`)))
	}
	r.KeyFileFingerprint = publicKeyFingerprint(r.KeyFile + ".pub")
	if os.Getenv("SSH_AUTH_SOCK") != "" {
		r.AgentForwarded = true
		r.AgentFingerprints = agentFingerprints(ctx)
	}
	r.Match = r.KeyFileFingerprint != "" && slices.Contains(r.AgentFingerprints, r.KeyFileFingerprint)
	return r
}

// publicKeyFingerprint returns the SHA-256 fingerprint of the public key in
// path, or "" when it cannot be read.
func publicKeyFingerprint(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	key, _, _, _, err := ssh.ParseAuthorizedKey(data)
	if err != nil {
		return ""
	}
	return ssh.FingerprintSHA256(key)
}

// agentFingerprints lists the fingerprints of the agent's keys with ssh-add.
// An agent with no keys (exit 1) or none to reach (exit 2) gives none.
func agentFingerprints(ctx context.Context) []string {
	ctx, cancel := context.WithTimeout(ctx, agentListTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ssh-add", "-l", "-E", "sha256")
	cmd.WaitDelay = time.Second
	git.HideWindow(cmd)
	out, _ := cmd.Output()
	return parseAgentList(string(out))
}

// parseAgentList keeps the fingerprint field of each line of ssh-add -l
// ("256 SHA256:… comment (ED25519)"). The comments, which can hold paths
// and host names, are dropped.
func parseAgentList(out string) []string {
	var fps []string
	for line := range strings.SplitSeq(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && strings.HasPrefix(fields[1], "SHA256:") && !slices.Contains(fps, fields[1]) {
			fps = append(fps, fields[1])
		}
	}
	return fps
}
