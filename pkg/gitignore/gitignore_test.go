package gitignore

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/LuisPalacios/gitbox/pkg/git"
)

// isolateHome redirects HOME (and USERPROFILE on Windows) and points
// git's global config at an isolated file via GIT_CONFIG_GLOBAL, so the
// test never touches the real ~/.gitconfig or ~/.gitignore_global.
// Returns the resolved home dir.
func isolateHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", home)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, ".gitconfig"))
	return home
}

func TestRecommendedBlock_RoundTrips(t *testing.T) {
	block := RecommendedBlock()
	body, ok := extractManagedBody(block)
	if !ok {
		t.Fatal("extractManagedBody on RecommendedBlock returned not-found")
	}
	if strings.TrimRight(body, "\n") != strings.TrimRight(recommendedBody, "\n") {
		t.Errorf("round-trip body mismatch:\nwant:\n%s\ngot:\n%s", recommendedBody, body)
	}
}

func TestExtractManagedBody_NoSentinel(t *testing.T) {
	if _, ok := extractManagedBody("just some user content\n.DS_Store\n"); ok {
		t.Errorf("expected ok=false when sentinels absent")
	}
}

func TestStripManagedBlock_PreservesUserContent(t *testing.T) {
	user := "# my own ignores\nnode_modules/\n.env\n"
	combined := user + "\n" + RecommendedBlock()

	stripped := stripManagedBlock(combined)
	if strings.Contains(stripped, SentinelBegin) || strings.Contains(stripped, SentinelEnd) {
		t.Errorf("stripped content still contains sentinels:\n%s", stripped)
	}
	if !strings.Contains(stripped, "node_modules/") || !strings.Contains(stripped, ".env") {
		t.Errorf("user content lost:\n%s", stripped)
	}
}

func TestMergeBlock_EmptyExisting(t *testing.T) {
	got := mergeBlock("")
	if !strings.HasPrefix(got, SentinelBegin) {
		t.Errorf("expected merged content to start with sentinel, got:\n%s", got)
	}
	if !strings.HasSuffix(got, SentinelEnd+"\n") {
		t.Errorf("expected merged content to end with sentinel + newline")
	}
}

func TestMergeBlock_KeepsExistingAndAppends(t *testing.T) {
	existing := "node_modules/\n.env\n"
	got := mergeBlock(existing)
	if !strings.Contains(got, "node_modules/") {
		t.Errorf("user content was dropped")
	}
	if !strings.Contains(got, recommendedBody) {
		t.Errorf("recommended body missing from merge result")
	}
	// Existing must precede the managed block.
	idxNode := strings.Index(got, "node_modules/")
	idxBegin := strings.Index(got, SentinelBegin)
	if idxNode < 0 || idxBegin < 0 || idxNode > idxBegin {
		t.Errorf("expected user content before sentinel, got order: node=%d sentinel=%d", idxNode, idxBegin)
	}
}

func TestMergeBlock_ReplacesStaleBlock(t *testing.T) {
	stale := "node_modules/\n\n" + SentinelBegin + "\n# stale junk\nstale-line\n" + SentinelEnd + "\n"
	got := mergeBlock(stale)
	if strings.Contains(got, "stale-line") {
		t.Errorf("stale block content was not removed:\n%s", got)
	}
	if !strings.Contains(got, "node_modules/") {
		t.Errorf("user content lost when replacing stale block")
	}
	if !strings.Contains(got, ".DS_Store") {
		t.Errorf("recommended content missing after merge")
	}
}

func TestCheck_FileMissing_NeedsAction(t *testing.T) {
	isolateHome(t)
	s, err := Check()
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if s.FileExists {
		t.Errorf("expected FileExists=false")
	}
	if !s.NeedsAction {
		t.Errorf("expected NeedsAction=true when file missing")
	}
	if s.ExcludesfileSet {
		t.Errorf("expected ExcludesfileSet=false in fresh home")
	}
}

func TestInstall_FreshHome_CreatesFileAndSetsConfig(t *testing.T) {
	home := isolateHome(t)

	res, err := Install()
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if !res.Updated {
		t.Errorf("expected Updated=true on fresh install")
	}
	if !res.SetExcludesfile {
		t.Errorf("expected SetExcludesfile=true on fresh install")
	}
	if res.BackupPath != "" {
		t.Errorf("expected no backup on fresh install, got %q", res.BackupPath)
	}

	path := filepath.Join(home, ".gitignore_global")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading installed file: %v", err)
	}
	if !strings.Contains(string(data), SentinelBegin) {
		t.Errorf("installed file missing sentinel begin")
	}
	if !strings.Contains(string(data), ".DS_Store") {
		t.Errorf("installed file missing recommended content")
	}

	// core.excludesfile should now point at our file (slash-normalized).
	val, err := git.GlobalConfigGet(ExcludesfileKey)
	if err != nil {
		t.Fatalf("reading excludesfile: %v", err)
	}
	if val == "" {
		t.Errorf("expected core.excludesfile to be set")
	}
}

func TestInstall_Idempotent(t *testing.T) {
	isolateHome(t)

	if _, err := Install(); err != nil {
		t.Fatalf("first Install: %v", err)
	}

	res, err := Install()
	if err != nil {
		t.Fatalf("second Install: %v", err)
	}
	if res.Updated {
		t.Errorf("second Install should be a no-op, got Updated=true")
	}
	if !res.AlreadyUpToDate {
		t.Errorf("second Install should report AlreadyUpToDate=true")
	}
	if res.BackupPath != "" {
		t.Errorf("second Install must not create a backup, got %q", res.BackupPath)
	}
	if res.SetExcludesfile {
		t.Errorf("second Install must not re-set core.excludesfile")
	}

	// Status should now report no action needed.
	s, err := Check()
	if err != nil {
		t.Fatalf("Check after install: %v", err)
	}
	if s.NeedsAction {
		t.Errorf("expected NeedsAction=false after install, got status=%+v", s)
	}
	if !s.BlockPresent || !s.BlockUpToDate || !s.ExcludesfileSet {
		t.Errorf("expected fully-installed state, got %+v", s)
	}
}

func TestInstall_PreservesExistingUserContent(t *testing.T) {
	home := isolateHome(t)
	path := filepath.Join(home, ".gitignore_global")

	user := "# my project ignores\nnode_modules/\n.env\nsecrets.txt\n"
	if err := os.WriteFile(path, []byte(user), 0o644); err != nil {
		t.Fatalf("seeding existing file: %v", err)
	}

	res, err := Install()
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if !res.Updated {
		t.Errorf("expected Updated=true when adding block to existing file")
	}
	if res.BackupPath == "" {
		t.Errorf("expected a backup to be created when modifying existing file")
	}
	// Backup must be readable and equal the original.
	bdata, err := os.ReadFile(res.BackupPath)
	if err != nil {
		t.Fatalf("reading backup: %v", err)
	}
	if string(bdata) != user {
		t.Errorf("backup contents differ from original:\nwant:\n%s\ngot:\n%s", user, string(bdata))
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading merged file: %v", err)
	}
	got := string(data)
	for _, want := range []string{"node_modules/", ".env", "secrets.txt", SentinelBegin, ".DS_Store", SentinelEnd} {
		if !strings.Contains(got, want) {
			t.Errorf("merged file missing %q\nfull content:\n%s", want, got)
		}
	}
}

func TestInstall_ReplacesStaleManagedBlock(t *testing.T) {
	home := isolateHome(t)
	path := filepath.Join(home, ".gitignore_global")

	stale := "node_modules/\n\n" + SentinelBegin + "\nstale-only-line\n" + SentinelEnd + "\n"
	if err := os.WriteFile(path, []byte(stale), 0o644); err != nil {
		t.Fatalf("seeding stale file: %v", err)
	}

	res, err := Install()
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if !res.Updated {
		t.Errorf("expected Updated=true when replacing stale block")
	}

	data, _ := os.ReadFile(path)
	got := string(data)
	if strings.Contains(got, "stale-only-line") {
		t.Errorf("stale-only-line should have been removed")
	}
	if !strings.Contains(got, "node_modules/") {
		t.Errorf("user content lost during stale-block replacement")
	}
	if !strings.Contains(got, ".DS_Store") {
		t.Errorf("recommended block missing after replacement")
	}
}

func TestManagedPatternSet_HasOwnedAndExcludesComments(t *testing.T) {
	set := managedPatternSet()
	for _, want := range []string{".DS_Store", "Thumbs.db", "*~", "[Dd]esktop.ini", "Network Trash Folder"} {
		if _, ok := set[want]; !ok {
			t.Errorf("expected managed pattern set to contain %q", want)
		}
	}
	for _, doNotWant := range []string{"# ─── macOS ───────────────────────────────", "", "# Source of truth: https://github.com/github/gitignore (Global/)"} {
		if _, ok := set[doNotWant]; ok {
			t.Errorf("managed pattern set must exclude comment/blank %q", doNotWant)
		}
	}
}

func TestStripManagedPatterns_RemovesOwnedLines(t *testing.T) {
	in := "node_modules/\n.DS_Store\n.env\nThumbs.db\nsecret.key\n"
	got := stripManagedPatterns(in)
	if strings.Contains(got, ".DS_Store") {
		t.Errorf(".DS_Store should have been stripped:\n%s", got)
	}
	if strings.Contains(got, "Thumbs.db") {
		t.Errorf("Thumbs.db should have been stripped:\n%s", got)
	}
	for _, keep := range []string{"node_modules/", ".env", "secret.key"} {
		if !strings.Contains(got, keep) {
			t.Errorf("user pattern %q must survive strip:\n%s", keep, got)
		}
	}
}

func TestStripManagedPatterns_PreservesNegations(t *testing.T) {
	// "!.DS_Store" is a deliberate negation; its trimmed form does not
	// match ".DS_Store" so it must survive.
	in := "!.DS_Store\nnode_modules/\n"
	got := stripManagedPatterns(in)
	if !strings.Contains(got, "!.DS_Store") {
		t.Errorf("negation should have been preserved:\n%s", got)
	}
}

func TestStripManagedPatterns_CollapsesBlankRuns(t *testing.T) {
	// Each removed pattern leaves no blank in its place; surrounding
	// blanks are collapsed to a single empty line.
	in := "node_modules/\n\n.DS_Store\n\n.env\n"
	got := stripManagedPatterns(in)
	if strings.Contains(got, "\n\n\n") {
		t.Errorf("blank runs should be collapsed:\n%q", got)
	}
	for _, keep := range []string{"node_modules/", ".env"} {
		if !strings.Contains(got, keep) {
			t.Errorf("expected %q to survive:\n%s", keep, got)
		}
	}
}

func TestFindDuplicatePatterns_ReportsOutsideOnly(t *testing.T) {
	// .DS_Store inside the sentinel block does NOT count as a duplicate.
	// .DS_Store outside the block DOES.
	content := ".DS_Store\nnode_modules/\n\n" + RecommendedBlock()
	dups := findDuplicatePatterns(content)
	if len(dups) != 1 || dups[0] != ".DS_Store" {
		t.Errorf("expected exactly [.DS_Store], got %v", dups)
	}
}

func TestFindDuplicatePatterns_DeduplicatesReport(t *testing.T) {
	// Same pattern repeated outside should be reported once.
	content := ".DS_Store\n.DS_Store\nThumbs.db\n"
	dups := findDuplicatePatterns(content)
	if len(dups) != 2 {
		t.Errorf("expected 2 distinct duplicates, got %v", dups)
	}
}

func TestCheck_ReportsDuplicates_AndNeedsAction(t *testing.T) {
	home := isolateHome(t)
	if _, err := Install(); err != nil {
		t.Fatalf("seed install: %v", err)
	}
	// User moves .DS_Store out of the managed block.
	path := filepath.Join(home, ".gitignore_global")
	data, _ := os.ReadFile(path)
	hijacked := ".DS_Store\nnode_modules/\n\n" + string(data)
	if err := os.WriteFile(path, []byte(hijacked), 0o644); err != nil {
		t.Fatalf("hijacking file: %v", err)
	}

	s, err := Check()
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !s.HasDuplicates {
		t.Errorf("expected HasDuplicates=true, got %+v", s)
	}
	if !s.NeedsAction {
		t.Errorf("expected NeedsAction=true when duplicates exist")
	}
	if len(s.Duplicates) != 1 || s.Duplicates[0] != ".DS_Store" {
		t.Errorf("expected duplicates=[.DS_Store], got %v", s.Duplicates)
	}
}

func TestInstall_DedupesOutsideEntries_AndIsIdempotent(t *testing.T) {
	home := isolateHome(t)
	path := filepath.Join(home, ".gitignore_global")

	// Seed: managed block with .DS_Store removed AND copied out, plus
	// a real user pattern that must be preserved.
	staleBlock := strings.Replace(recommendedBody, ".DS_Store\n", "", 1)
	hijacked := ".DS_Store\nnode_modules/\n\n" +
		SentinelBegin + "\n" + staleBlock + "\n" + SentinelEnd + "\n"
	if err := os.WriteFile(path, []byte(hijacked), 0o644); err != nil {
		t.Fatalf("seeding file: %v", err)
	}

	res, err := Install()
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if !res.Updated {
		t.Errorf("expected Updated=true when sanitizing duplicates")
	}
	if res.BackupPath == "" {
		t.Errorf("expected backup before sanitizing")
	}

	merged, _ := os.ReadFile(path)
	got := string(merged)
	if strings.Count(got, ".DS_Store") != 1 {
		t.Errorf("expected exactly one .DS_Store occurrence after sanitize, got %d:\n%s",
			strings.Count(got, ".DS_Store"), got)
	}
	if !strings.Contains(got, "node_modules/") {
		t.Errorf("user pattern node_modules/ was lost during sanitize")
	}
	// The single remaining .DS_Store must live inside the sentinel
	// block, not in the user region above it.
	idxDS := strings.Index(got, ".DS_Store")
	idxBegin := strings.Index(got, SentinelBegin)
	if idxDS < idxBegin {
		t.Errorf("expected .DS_Store to live inside the managed block, got dsIndex=%d beginIndex=%d", idxDS, idxBegin)
	}

	// Idempotency: second install must be a no-op.
	res2, err := Install()
	if err != nil {
		t.Fatalf("second Install: %v", err)
	}
	if res2.Updated {
		t.Errorf("second Install should be a no-op, got Updated=true")
	}
	if !res2.AlreadyUpToDate {
		t.Errorf("second Install should report AlreadyUpToDate")
	}
	if res2.BackupPath != "" {
		t.Errorf("second Install must not back up, got %q", res2.BackupPath)
	}

	s, _ := Check()
	if s.NeedsAction {
		t.Errorf("expected NeedsAction=false after dedup install, got %+v", s)
	}
}

func TestPruneBackups_KeepsOnlyMaxBackups(t *testing.T) {
	home := isolateHome(t)
	base := filepath.Join(home, ".gitignore_global")

	// Seed five fake backup files with sortable timestamp suffixes.
	stamps := []string{
		"20260101-100000",
		"20260102-100000",
		"20260103-100000",
		"20260104-100000",
		"20260105-100000",
	}
	for _, s := range stamps {
		if err := os.WriteFile(base+".bak-"+s, []byte("dummy"), 0o644); err != nil {
			t.Fatalf("seeding %s: %v", s, err)
		}
	}

	pruneBackups(base)

	// The newest maxBackups should survive; the rest should be gone.
	keep := stamps[len(stamps)-maxBackups:]
	drop := stamps[:len(stamps)-maxBackups]

	for _, s := range keep {
		p := base + ".bak-" + s
		if _, err := os.Stat(p); err != nil {
			t.Errorf("expected newest backup %s to survive, got %v", s, err)
		}
	}
	for _, s := range drop {
		p := base + ".bak-" + s
		if _, err := os.Stat(p); err == nil {
			t.Errorf("expected old backup %s to be removed", s)
		}
	}
}

func TestPruneBackups_NoOpUnderCap(t *testing.T) {
	home := isolateHome(t)
	base := filepath.Join(home, ".gitignore_global")

	// Two backups, both must survive (we are under maxBackups=3).
	for _, s := range []string{"20260101-100000", "20260102-100000"} {
		if err := os.WriteFile(base+".bak-"+s, []byte("dummy"), 0o644); err != nil {
			t.Fatalf("seeding: %v", err)
		}
	}

	pruneBackups(base)

	matches, _ := filepath.Glob(base + ".bak-*")
	if len(matches) != 2 {
		t.Errorf("expected 2 backups untouched, got %d: %v", len(matches), matches)
	}
}

func TestBackupFile_PrunesOldBackupsOnCreate(t *testing.T) {
	home := isolateHome(t)
	base := filepath.Join(home, ".gitignore_global")
	if err := os.WriteFile(base, []byte("user content"), 0o644); err != nil {
		t.Fatalf("seeding base file: %v", err)
	}

	// Pre-seed maxBackups existing backups so the new one created by
	// backupFile() pushes us over the cap and forces a prune.
	for _, s := range []string{
		"20260101-100000",
		"20260102-100000",
		"20260103-100000",
	} {
		if err := os.WriteFile(base+".bak-"+s, []byte("old"), 0o644); err != nil {
			t.Fatalf("seeding backup: %v", err)
		}
	}

	if _, err := backupFile(base); err != nil {
		t.Fatalf("backupFile: %v", err)
	}

	matches, _ := filepath.Glob(base + ".bak-????????-??????")
	if len(matches) > maxBackups {
		t.Errorf("expected at most %d backups after create+prune, got %d: %v",
			maxBackups, len(matches), matches)
	}

	// The oldest seeded file (20260101) must have been the one removed
	// because backupFile() created a newer-dated file via time.Now().
	if _, err := os.Stat(base + ".bak-20260101-100000"); err == nil {
		t.Errorf("oldest seed backup should have been pruned")
	}
}

func TestInstall_RespectsExistingExcludesfile(t *testing.T) {
	home := isolateHome(t)
	custom := filepath.Join(home, "custom-ignore")

	if err := git.GlobalConfigSet(ExcludesfileKey, filepath.ToSlash(custom)); err != nil {
		t.Fatalf("seeding excludesfile: %v", err)
	}

	res, err := Install()
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if res.SetExcludesfile {
		t.Errorf("Install must not re-set core.excludesfile when already set")
	}
	if res.Path != custom {
		t.Errorf("expected Install to act on %s, got %s", custom, res.Path)
	}
	// File should now exist at the custom path with the managed block.
	data, err := os.ReadFile(custom)
	if err != nil {
		t.Fatalf("reading custom excludes file: %v", err)
	}
	if !strings.Contains(string(data), SentinelBegin) {
		t.Errorf("custom file missing managed block")
	}
}
