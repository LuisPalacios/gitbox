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

// TrustWarnings names effective settings that require review on a managed
// clone. Only fixed key names can appear in a warning. Git configuration
// values can contain credentials, commands or private machine paths.
func (s ConfigSnapshot) TrustWarnings(ctx context.Context, clone, origin string) []string {
	var warnings []string
	warn := func(key string) {
		e := GitError{Op: "config", Code: CodeUnsafeConfig, Detail: key}
		warnings = append(warnings, e.Error())
	}
	switch strings.ToLower(s.Get("core.fsmonitor")) {
	case "", "true", "false", "yes", "no", "on", "off", "0", "1":
	default:
		warn("core.fsmonitor")
	}
	if s.Get("core.hooksPath") != "" {
		// Git expands its own path syntax (including ~ and %(prefix)).
		value, err := outputContext(ctx, clone, "config", "--path", "--get", "core.hooksPath")
		if err != nil || !hooksInside(clone, strings.TrimSuffix(value, "\n")) {
			warn("core.hooksPath")
		}
	}
	if s.rewritesURL(origin) {
		// --get-url applies Git's precedence rules without contacting a remote.
		resolved, err := outputContext(ctx, clone, "ls-remote", "--get-url", "--", origin)
		want := fetchHost(origin)
		got := fetchHost(strings.TrimSpace(resolved))
		if err != nil || got == "" || !strings.EqualFold(got, want) {
			warn("url.*.insteadOf")
		}
	}
	return warnings
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

func (s ConfigSnapshot) rewritesURL(origin string) bool {
	for key, prefixes := range s.Values {
		if strings.HasPrefix(key, "url.") && strings.HasSuffix(key, ".insteadof") {
			for _, prefix := range prefixes {
				if origin != "" && strings.HasPrefix(origin, prefix) {
					return true
				}
			}
		}
	}
	return false
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
