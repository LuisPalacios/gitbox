// Package gitignore manages the user's global gitignore file
// (~/.gitignore_global) by installing a recommended block of OS-level
// junk patterns inside sentinel markers, so per-project .gitignore files
// don't have to repeat them.
package gitignore

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/LuisPalacios/gitbox/pkg/git"
)

// ExcludesfileKey is the git config key for the global excludes file.
const ExcludesfileKey = "core.excludesfile"

// maxBackups is the rolling-window cap on .bak-YYYYMMDD-HHMMSS files
// kept next to the global gitignore. Each Install() that rewrites the
// file creates one new backup and prunes the oldest beyond this cap, so
// the user always has a recovery point without the directory growing
// without bound.
const maxBackups = 3

// Sentinel markers wrap the managed block so we can rewrite only that
// region without disturbing user-added entries.
const (
	SentinelBegin = "# >>> gitbox:global-gitignore >>>"
	SentinelEnd   = "# <<< gitbox:global-gitignore <<<"
)

// recommendedBody is the canonical content of the managed block (between
// the sentinel markers, exclusive). Update this when the recommendation
// changes; existing installs will be detected as out-of-date and re-applied.
const recommendedBody = `# Global Gitignore — managed by gitbox
# Covers OS-level files for macOS, Windows, and Linux.
# Source of truth: https://github.com/github/gitignore (Global/)

# ─── macOS ───────────────────────────────
.DS_Store
.AppleDouble
.LSOverride
Icon
._*
.DocumentRevisions-V100
.fseventsd
.Spotlight-V100
.TemporaryItems
.Trashes
.VolumeIcon.icns
.com.apple.timemachine.donotpresent
.AppleDB
.AppleDesktop
Network Trash Folder
Temporary Items
.apdisk

# ─── Windows ─────────────────────────────
Thumbs.db
Thumbs.db:encryptable
ehthumbs.db
ehthumbs_vista.db
*.stackdump
[Dd]esktop.ini
$RECYCLE.BIN/
*.cab
*.msi
*.msix
*.msm
*.msp
*.lnk

# ─── Linux ───────────────────────────────
*~
.fuse_hidden*
.directory
.Trash-*
.nfs*`

// RecommendedBlock returns the full sentinel-wrapped managed block,
// terminated by a single newline.
func RecommendedBlock() string {
	return SentinelBegin + "\n" + recommendedBody + "\n" + SentinelEnd + "\n"
}

// RecommendedBody returns the canonical body of the managed block
// (without sentinel markers). Useful for previews in the UI.
func RecommendedBody() string {
	return recommendedBody
}

// DefaultPath returns the default location for the global gitignore file:
// ~/.gitignore_global on every platform (using the resolved home dir).
func DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home directory: %w", err)
	}
	return filepath.Join(home, ".gitignore_global"), nil
}

// Status reports the current state of the global gitignore.
type Status struct {
	Path            string   `json:"path"`            // resolved path of the file we'd act on
	DefaultPath     string   `json:"defaultPath"`     // ~/.gitignore_global
	Excludesfile    string   `json:"excludesfile"`    // raw value of git config --global core.excludesfile (empty if unset)
	ExcludesfileSet bool     `json:"excludesfileSet"` // core.excludesfile is set in global git config
	FileExists      bool     `json:"fileExists"`      // a file exists at Path
	BlockPresent    bool     `json:"blockPresent"`    // sentinel-wrapped managed block was found
	BlockUpToDate   bool     `json:"blockUpToDate"`   // managed block matches recommendedBody exactly
	HasDuplicates   bool     `json:"hasDuplicates"`   // a managed-block pattern also appears outside the block
	Duplicates      []string `json:"duplicates,omitempty"` // distinct duplicated patterns (trimmed)
	NeedsAction     bool     `json:"needsAction"`     // true when Install() would change something
}

// Check inspects git config and the global gitignore file and returns
// a Status describing what (if anything) Install() would do.
func Check() (Status, error) {
	def, err := DefaultPath()
	if err != nil {
		return Status{}, err
	}

	s := Status{DefaultPath: def, Path: def}

	// git config --global --get core.excludesfile
	if val, err := git.GlobalConfigGet(ExcludesfileKey); err == nil && val != "" {
		s.ExcludesfileSet = true
		s.Excludesfile = val
		s.Path = expandPath(val)
	}

	data, err := os.ReadFile(s.Path)
	if err != nil {
		if !os.IsNotExist(err) {
			return s, fmt.Errorf("reading %s: %w", s.Path, err)
		}
		// File doesn't exist — needs install.
		s.NeedsAction = true
		return s, nil
	}
	s.FileExists = true
	content := string(data)

	body, ok := extractManagedBody(content)
	if ok {
		s.BlockPresent = true
		s.BlockUpToDate = strings.TrimRight(body, "\n") == strings.TrimRight(recommendedBody, "\n")
	}

	// Even when no sentinels are present, scan the whole file for lines
	// that the managed block would own — those count as duplicates that
	// Install() will sanitize away.
	s.Duplicates = findDuplicatePatterns(content)
	s.HasDuplicates = len(s.Duplicates) > 0

	if !s.BlockPresent || !s.BlockUpToDate || !s.ExcludesfileSet || s.HasDuplicates {
		s.NeedsAction = true
	}
	return s, nil
}

// InstallResult describes what Install() actually did.
type InstallResult struct {
	Path            string `json:"path"`            // file that was written or inspected
	BackupPath      string `json:"backupPath"`      // empty when no backup was taken
	SetExcludesfile bool   `json:"setExcludesfile"` // true when we just set core.excludesfile
	Updated         bool   `json:"updated"`         // file content actually changed
	AlreadyUpToDate bool   `json:"alreadyUpToDate"` // file content matched the recommended block already
}

// Install ensures the global gitignore contains the recommended managed
// block and that core.excludesfile points at it. It is safe to run
// repeatedly: when nothing needs to change it is a no-op (no backup,
// no rewrite).
func Install() (InstallResult, error) {
	status, err := Check()
	if err != nil {
		return InstallResult{}, err
	}

	res := InstallResult{Path: status.Path}

	// Step 1: rewrite the file if the managed block is missing, stale,
	// or any of its patterns have been duplicated outside the sentinels.
	if !status.BlockPresent || !status.BlockUpToDate || status.HasDuplicates {
		var existing string
		if status.FileExists {
			data, err := os.ReadFile(status.Path)
			if err != nil {
				return res, fmt.Errorf("reading %s: %w", status.Path, err)
			}
			existing = string(data)

			backup, err := backupFile(status.Path)
			if err != nil {
				return res, fmt.Errorf("backing up %s: %w", status.Path, err)
			}
			res.BackupPath = backup
		}

		merged := mergeBlock(existing)
		if err := atomicWrite(status.Path, []byte(merged)); err != nil {
			return res, fmt.Errorf("writing %s: %w", status.Path, err)
		}
		res.Updated = true
	} else {
		res.AlreadyUpToDate = true
	}

	// Step 2: ensure core.excludesfile points at the file.
	if !status.ExcludesfileSet {
		val := canonicalExcludesValue(status.Path)
		if err := git.GlobalConfigSet(ExcludesfileKey, val); err != nil {
			return res, fmt.Errorf("setting %s: %w", ExcludesfileKey, err)
		}
		res.SetExcludesfile = true
	}
	return res, nil
}

// extractManagedBody returns the body of the managed block (everything
// strictly between SentinelBegin and SentinelEnd), and a boolean
// indicating whether the block was found.
func extractManagedBody(content string) (string, bool) {
	begin := strings.Index(content, SentinelBegin)
	if begin < 0 {
		return "", false
	}
	rest := content[begin+len(SentinelBegin):]
	end := strings.Index(rest, SentinelEnd)
	if end < 0 {
		return "", false
	}
	body := rest[:end]
	body = strings.TrimPrefix(body, "\n")
	body = strings.TrimSuffix(body, "\n")
	return body, true
}

// mergeBlock returns the new file content: existing user content with
// the managed block stripped AND any line that the managed block owns
// removed from the rest of the file, followed by a fresh recommended
// block. The result always ends with a newline.
//
// Sanitization is essential because users will sometimes copy or move
// patterns out of the managed region; without this step Install would
// duplicate them on every run instead of resolving the conflict in the
// managed block's favour.
func mergeBlock(existing string) string {
	stripped := stripManagedBlock(existing)
	stripped = stripManagedPatterns(stripped)
	stripped = strings.TrimRight(stripped, "\n")

	var b strings.Builder
	if stripped != "" {
		b.WriteString(stripped)
		b.WriteString("\n\n")
	}
	b.WriteString(RecommendedBlock())
	return b.String()
}

// managedPatternSet returns the set of pattern lines (non-blank,
// non-comment, trimmed) that the recommended block owns. These are the
// lines that must never appear outside the sentinel markers — anywhere
// else they are duplicates and Install() removes them.
//
// Comments and blank lines from the recommended body are deliberately
// excluded so users can keep their own comments outside the block.
func managedPatternSet() map[string]struct{} {
	set := make(map[string]struct{})
	for _, line := range strings.Split(recommendedBody, "\n") {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		set[t] = struct{}{}
	}
	return set
}

// stripManagedPatterns removes from content any line whose trimmed
// form matches a pattern owned by the managed block, and collapses
// runs of blank lines that result from those removals down to one.
//
// Negation patterns ("!.DS_Store") are preserved because their trimmed
// form does not match the unprefixed pattern in the set.
func stripManagedPatterns(content string) string {
	set := managedPatternSet()
	lines := strings.Split(content, "\n")
	out := make([]string, 0, len(lines))
	prevBlank := false
	for _, line := range lines {
		t := strings.TrimSpace(line)
		if _, owned := set[t]; owned {
			// Drop the line entirely; do not promote a blank in its
			// place — the prevBlank logic below collapses any whitespace
			// that surrounded the removed line.
			continue
		}
		blank := t == ""
		if blank && prevBlank {
			continue
		}
		prevBlank = blank
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

// findDuplicatePatterns returns the trimmed pattern lines from content
// that the managed block also owns AND that appear outside the sentinel
// markers. The result is deduplicated so each offending pattern is
// reported once even if the user has it on multiple lines.
func findDuplicatePatterns(content string) []string {
	set := managedPatternSet()
	outside := stripManagedBlock(content)
	var dups []string
	seen := make(map[string]bool)
	for _, line := range strings.Split(outside, "\n") {
		t := strings.TrimSpace(line)
		if t == "" {
			continue
		}
		if _, owned := set[t]; !owned || seen[t] {
			continue
		}
		seen[t] = true
		dups = append(dups, t)
	}
	return dups
}

// stripManagedBlock removes the first SentinelBegin..SentinelEnd region
// (inclusive of the markers and any trailing newline immediately after
// the end marker) from content. If no block is present, content is
// returned unchanged.
func stripManagedBlock(content string) string {
	begin := strings.Index(content, SentinelBegin)
	if begin < 0 {
		return content
	}
	rest := content[begin+len(SentinelBegin):]
	end := strings.Index(rest, SentinelEnd)
	if end < 0 {
		return content
	}
	tail := rest[end+len(SentinelEnd):]
	tail = strings.TrimPrefix(tail, "\n")

	head := content[:begin]
	head = strings.TrimRight(head, "\n")
	if head == "" {
		return tail
	}
	if tail == "" {
		return head
	}
	return head + "\n" + tail
}

// backupFile copies path to <path>.bak-YYYYMMDD-HHMMSS and returns the
// backup path. The caller is responsible for ensuring path exists.
// After writing the new backup the helper prunes the oldest .bak files
// for the same base path so that no more than maxBackups remain.
func backupFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	stamp := time.Now().Format("20060102-150405")
	backup := path + ".bak-" + stamp
	if err := os.WriteFile(backup, data, 0o644); err != nil {
		return "", err
	}
	pruneBackups(path)
	return backup, nil
}

// pruneBackups removes the oldest .bak-YYYYMMDD-HHMMSS files for the
// given base path, keeping only the most recent maxBackups. Pruning is
// best-effort: filesystem errors are swallowed so they never block an
// install. The ISO-style timestamp suffix sorts lexicographically in
// the same order as chronologically, so a plain string sort is enough
// to identify "oldest".
func pruneBackups(path string) {
	pattern := path + ".bak-????????-??????"
	matches, err := filepath.Glob(pattern)
	if err != nil || len(matches) <= maxBackups {
		return
	}
	sort.Strings(matches)
	for _, old := range matches[:len(matches)-maxBackups] {
		_ = os.Remove(old)
	}
}

// atomicWrite writes data to path via a temp file in the same directory
// followed by os.Rename, so a partial write can never leave the global
// gitignore in an inconsistent state.
func atomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".gitignore_global.tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := func() { os.Remove(tmpPath) }
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return err
	}
	// On Windows os.Rename fails if the destination exists; remove it first.
	if runtime.GOOS == "windows" {
		_ = os.Remove(path)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		cleanup()
		return err
	}
	return nil
}

// expandPath resolves a leading ~ in a git config value and converts
// any forward slashes to the OS-native separator so the result can be
// passed straight to os.ReadFile / os.Rename.
func expandPath(p string) string {
	if p == "" {
		return p
	}
	if strings.HasPrefix(p, "~") {
		if home, err := os.UserHomeDir(); err == nil {
			p = filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	return filepath.FromSlash(p)
}

// canonicalExcludesValue returns the value to write to core.excludesfile.
// Git accepts absolute paths on every platform; we use forward slashes
// for portability so the same global git config can be re-used across
// machines mounted under the same home dir.
func canonicalExcludesValue(path string) string {
	return filepath.ToSlash(path)
}
