package git

import (
	"context"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Each inspection process uses a private, empty hooks directory. Only reads
// on a clone discovered on disk use these command-scoped settings.
var inspectionHooks struct {
	sync.Mutex
	dir string
}

// InspectionProfile returns the command-line settings for an untracked clone.
func InspectionProfile() ([]string, error) {
	inspectionHooks.Lock()
	defer inspectionHooks.Unlock()
	if inspectionHooks.dir == "" {
		base, err := filepath.Abs(os.TempDir())
		if err != nil {
			return nil, errors.New("could not locate Git inspection directory")
		}
		dir, err := os.MkdirTemp(base, "gitbox-inspection-hooks-")
		if err != nil {
			return nil, errors.New("could not prepare Git inspection")
		}
		inspectionHooks.dir = dir
	}
	return []string{"-c", "core.fsmonitor=false", "-c", "core.hooksPath=" + inspectionHooks.dir,
		"-c", "core.sshCommand=", "-c", "protocol.file.allow=never"}, nil
}

// CloseInspectionProfile removes the process's empty directory on shutdown.
func CloseInspectionProfile() {
	inspectionHooks.Lock()
	defer inspectionHooks.Unlock()
	if inspectionHooks.dir != "" {
		_ = os.Remove(inspectionHooks.dir)
		inspectionHooks.dir = ""
	}
}

func inspectOutput(path string, args ...string) (string, error) {
	profile, err := InspectionProfile()
	if err != nil {
		return "", err
	}
	return output(path, append(profile, args...)...)
}

// The settings TrustFindings reviews, as a finding names them.
const (
	TrustFSMonitor = "core.fsmonitor"
	TrustHooksPath = "core.hooksPath"
	TrustInsteadOf = "url.*.insteadOf"
)

// The scopes a clone controls. Git reports "local" and "worktree";
// ConfigSnapshot relabels a file included from the local config "include".
// The user's global file and the system file are written by the user or
// an administrator on purpose, so their settings are trusted.
const (
	ScopeLocal    = "local"
	ScopeWorktree = "worktree"
	ScopeInclude  = "include"
)

func cloneControlled(scope string) bool {
	return scope == ScopeLocal || scope == ScopeWorktree || scope == ScopeInclude
}

// TrustFinding is a setting stored where the clone controls it that could
// run a program or redirect the managed origin. Key and Scope come from the
// fixed lists above: a Git value can hold a credential, a command or a
// private path, so it never appears in a finding.
type TrustFinding struct {
	Key   string
	Scope string
}

func (f TrustFinding) Error() string { return "unsafe " + f.Key + " in " + f.Scope + " config" }

// Err is the error that stops a fetch or a pull of the clone. errors.As
// finds the finding beneath it.
func (f TrustFinding) Err() *GitError {
	return &GitError{Op: "config", Code: CodeUnsafeConfig, Hint: "error.git." + string(CodeUnsafeConfig),
		Detail: f.Key, Err: f, ExitCode: -1}
}

// Warning is the sentence a repair report shows for f.
func (f TrustFinding) Warning() string {
	where := map[string]string{
		ScopeLocal:    "the clone's own config",
		ScopeWorktree: "the clone's worktree config",
		ScopeInclude:  "a file the clone's config includes",
	}[f.Scope]
	what := "points outside the clone"
	switch f.Key {
	case TrustFSMonitor:
		what = "runs a program"
	case TrustInsteadOf:
		what = "sends the origin to another server"
	}
	return f.Key + " in " + where + " " + what + ": fetch and pull stay paused until it is reviewed"
}

// TrustFindings reviews the settings a clone controls that require review
// before Gitbox fetches or pulls it. Each setting is judged by the entry Git
// would use; a value from the global or system config is trusted.
func (s ConfigSnapshot) TrustFindings(ctx context.Context, clone, origin string) []TrustFinding {
	var findings []TrustFinding
	if e, ok := s.winning(TrustFSMonitor); ok && cloneControlled(e.Scope) {
		switch strings.ToLower(e.Value) {
		case "", "true", "false", "yes", "no", "on", "off", "0", "1":
		default:
			findings = append(findings, TrustFinding{TrustFSMonitor, e.Scope})
		}
	}
	if e, ok := s.winning(TrustHooksPath); ok && cloneControlled(e.Scope) && e.Value != "" {
		// Git expands its own path syntax (including ~ and %(prefix)).
		value, err := outputContext(ctx, clone, "config", "--path", "--get", TrustHooksPath)
		if err != nil || !hooksInside(clone, strings.TrimSuffix(value, "\n")) {
			findings = append(findings, TrustFinding{TrustHooksPath, e.Scope})
		}
	}
	if e, ok := s.rewrite(origin); ok && cloneControlled(e.Scope) {
		// --get-url applies Git's precedence rules without contacting a remote.
		resolved, err := outputContext(ctx, clone, "ls-remote", "--get-url", "--", origin)
		want := fetchHost(origin)
		got := fetchHost(strings.TrimSpace(resolved))
		if err != nil || got == "" || !strings.EqualFold(got, want) {
			findings = append(findings, TrustFinding{TrustInsteadOf, e.Scope})
		}
	}
	return findings
}

// winning is the entry that sets a single-value key: Git uses the last one.
func (s ConfigSnapshot) winning(key string) (ConfigEntry, bool) {
	key = configKey(key)
	for i := len(s.Entries) - 1; i >= 0; i-- {
		if s.Entries[i].Key == key {
			return s.Entries[i], true
		}
	}
	return ConfigEntry{}, false
}

// A managed origin uses a network URL or Git's user@host:path SSH form.
// Other transports have no comparable server. Keep explicit ports so two
// services on the same machine are distinguishable.
func fetchHost(raw string) string {
	if strings.Contains(raw, "://") {
		u, err := url.Parse(raw)
		if err == nil {
			switch u.Scheme {
			case "https", "http", "ssh", "git":
				return u.Host
			}
		}
		return ""
	}
	if strings.Contains(raw, "::") {
		return ""
	}
	host, _, _, err := ParseRemoteURL(raw)
	if err != nil {
		return ""
	}
	return host
}

// rewrite is the insteadOf entry Git applies to origin: the longest matching
// prefix wins. On a tie it prefers a setting the clone controls, so a
// redirect the clone wrote is never hidden behind a trusted one.
func (s ConfigSnapshot) rewrite(origin string) (ConfigEntry, bool) {
	var best ConfigEntry
	found := false
	if origin == "" {
		return best, false
	}
	for _, e := range s.Entries {
		if !strings.HasPrefix(e.Key, "url.") || !strings.HasSuffix(e.Key, ".insteadof") ||
			e.Value == "" || !strings.HasPrefix(origin, e.Value) {
			continue
		}
		if !found || len(e.Value) > len(best.Value) ||
			len(e.Value) == len(best.Value) && cloneControlled(e.Scope) {
			best, found = e, true
		}
	}
	return best, found
}

func hooksInside(clone, hooks string) bool {
	root, err := filepath.EvalSymlinks(clone)
	if err != nil || hooks == "" {
		return false
	}
	hooks = filepath.FromSlash(hooks)
	if !filepath.IsAbs(hooks) {
		// Preserve .. until symlinks are resolved.
		hooks = clone + string(filepath.Separator) + hooks
	}
	// A missing hooks directory is common. Check the nearest existing
	// ancestor and require Git's remaining relative path to stay beneath it.
	for {
		resolved, err := filepath.EvalSymlinks(hooks)
		if err == nil {
			rel, err := filepath.Rel(root, resolved)
			return err == nil && filepath.IsLocal(rel)
		}
		if !errors.Is(err, os.ErrNotExist) {
			return false
		}
		dir, name := filepath.Split(hooks)
		// If the entry exists but cannot resolve, it is a dangling symlink.
		if _, err := os.Lstat(hooks); !errors.Is(err, os.ErrNotExist) || name == ".." {
			return false
		}
		hooks = strings.TrimRight(dir, string(filepath.Separator))
		if hooks == "" || hooks == filepath.VolumeName(hooks) {
			return false
		}
	}
}
