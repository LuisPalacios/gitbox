package git

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// A partial clone can fetch a promised blob even during a local Git read.
// Deny all transports as well: older Git understands GIT_ALLOW_PROTOCOL
// even when it does not yet implement GIT_NO_LAZY_FETCH.
func sweepRead(dir string, args ...string) (string, error) {
	env := append(nonInteractiveEnv(), "GIT_NO_LAZY_FETCH=1", "GIT_ALLOW_PROTOCOL=")
	return outputRequest(context.Background(), Request{Dir: dir, Args: args, EnvExact: env})
}

func sweepDefault(dir string) (string, error) {
	return defaultBranchWith(func(args ...string) (string, error) { return sweepRead(dir, args...) })
}

// sweepRef pins the commit and tracks whether Git has the branch checked
// out in any worktree. Never follow a symbolic branch ref during a sweep.
type sweepRef struct {
	name, tip, upstream string
	gone, protected     bool
}

func sweepRefs(dir string, patterns []string) ([]sweepRef, error) {
	const format = "--format=%(refname:lstrip=2)%00%(objectname)%00%(upstream)%00%(upstream:track)%00%(symref)%00%(worktreepath)%00"
	out, err := sweepRead(dir, append([]string{"for-each-ref", format, "--"}, patterns...)...)
	if err != nil {
		return nil, err
	}
	var refs []sweepRef
	for out != "" {
		var fields [6]string
		for i := range fields {
			var ok bool
			fields[i], out, ok = strings.Cut(out, "\x00")
			if !ok {
				return nil, newGitErrorContext(context.Background(), "for-each-ref", errors.New("incomplete branch record"), nil)
			}
		}
		out = strings.TrimPrefix(out, "\n")
		refs = append(refs, sweepRef{name: fields[0], tip: fields[1], upstream: fields[2], gone: fields[3] == "[gone]", protected: fields[4] != "" || fields[5] != ""})
	}
	return refs, nil
}

func commitID(dir, ref string) (string, error) {
	out, err := sweepRead(dir, "rev-parse", "--verify", ref+"^{commit}")
	return strings.TrimSpace(out), err
}

// operationBranches finds the branches reserved by a rebase or a bisect.
// During either operation HEAD is detached and %(worktreepath) is empty.
func operationBranches(dir string) (map[string]bool, error) {
	raw, err := sweepRead(dir, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return nil, err
	}
	common := strings.TrimSuffix(raw, "\n")
	dirs := []string{common}
	linked, err := os.ReadDir(filepath.Join(common, "worktrees"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, newGitErrorContext(context.Background(), "worktree", err, nil)
	}
	for _, entry := range linked {
		if entry.IsDir() {
			dirs = append(dirs, filepath.Join(common, "worktrees", entry.Name()))
		}
	}
	branches := make(map[string]bool)
	for _, admin := range dirs {
		for _, marker := range []string{"BISECT_START", "rebase-merge/head-name", "rebase-apply/head-name", "rebase-merge/update-refs"} {
			path := filepath.Join(admin, marker)
			info, err := os.Lstat(path)
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				return nil, newGitErrorContext(context.Background(), "worktree", err, nil)
			}
			// The administrative markers should contain only branch names and
			// object IDs. Refuse special files, links, and oversized markers.
			if !info.Mode().IsRegular() || info.Size() > DefaultMaxOutput {
				return nil, newGitErrorContext(context.Background(), "worktree", errors.New("invalid operation marker"), nil)
			}
			file, err := os.Open(path)
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				return nil, newGitErrorContext(context.Background(), "worktree", err, nil)
			}
			lines := bufio.NewScanner(file)
			for lines.Scan() {
				line := lines.Text()
				if name, ok := strings.CutPrefix(line, "refs/heads/"); ok {
					branches[name] = true
				} else if marker == "BISECT_START" {
					branches[line] = true
				}
			}
			_ = file.Close()
			if err := lines.Err(); err != nil {
				return nil, newGitErrorContext(context.Background(), "worktree", err, nil)
			}
		}
	}
	return branches, nil
}

// sweepCategory classifies pinned commits, so a concurrent update cannot
// attach an observation about yesterday's tip to today's preview.
func sweepCategory(dir, base string, ref sweepRef) string {
	if ref.gone {
		return "gone"
	}
	if _, err := sweepRead(dir, "merge-base", "--is-ancestor", ref.tip, base); err == nil {
		return "merged"
	} else if ge, ok := errors.AsType[*GitError](err); !ok || ge.ExitCode != 1 {
		return ""
	}
	if isSquashMerged(dir, base, ref.tip) {
		return "squashed"
	}
	return ""
}

func isSquashMerged(dir, base, tip string) bool {
	// Identical trees also cover a squash of several commits into one.
	trees, err := sweepRead(dir, "rev-parse", base+"^{tree}", tip+"^{tree}")
	if err != nil {
		return false
	}
	pair := strings.Fields(trees)
	if len(pair) != 2 {
		return false
	}
	if pair[0] == pair[1] {
		return true
	}
	cherry, err := sweepRead(dir, "cherry", base, tip)
	if err != nil || strings.TrimSpace(cherry) == "" {
		return false
	}
	for line := range strings.SplitSeq(strings.TrimSpace(cherry), "\n") {
		if !strings.HasPrefix(line, "- ") {
			return false
		}
	}
	// Patch IDs survive a later revert, and cherry skips merge commits.
	// Require the current base to still contain the same final content
	// on every path changed by the branch, including merge resolutions.
	changed, err := sweepRead(dir, "diff", "--no-ext-diff", "--no-textconv", "--no-renames", "--ignore-submodules=none", "--name-only", "-z", base+"..."+tip, "--")
	if err != nil || changed == "" {
		return false
	}
	different, err := sweepRead(dir, "diff", "--no-ext-diff", "--no-textconv", "--no-renames", "--ignore-submodules=none", "--name-only", "-z", base, tip, "--")
	if err != nil {
		return false
	}
	paths := make(map[string]bool)
	for p := range strings.SplitSeq(strings.TrimSuffix(changed, "\x00"), "\x00") {
		paths[p] = true
	}
	for p := range strings.SplitSeq(strings.TrimSuffix(different, "\x00"), "\x00") {
		if paths[p] {
			return false
		}
	}
	return true
}

// SweepBranches reads local refs without modifying the repository.
func SweepBranches(dir string) (SweepResult, error) {
	def, err := sweepDefault(dir)
	if err != nil {
		return SweepResult{}, err
	}
	cur, err := sweepRead(dir, "rev-parse", "--abbrev-ref", "HEAD")
	cur = strings.TrimSpace(cur)
	if err != nil {
		return SweepResult{}, err
	}
	base, err := commitID(dir, "refs/heads/"+def)
	if err != nil {
		return SweepResult{}, err
	}
	refs, err := sweepRefs(dir, []string{"refs/heads/"})
	if err != nil {
		return SweepResult{}, err
	}
	protected, err := operationBranches(dir)
	if err != nil {
		return SweepResult{}, err
	}
	result := SweepResult{DefaultBranch: def, CurrentBranch: cur, Tips: make(map[string]string)}
	for _, ref := range refs {
		if ref.name == def || ref.name == cur || ref.protected || protected[ref.name] {
			continue
		}
		switch sweepCategory(dir, base, ref) {
		case "gone":
			result.Gone = append(result.Gone, ref.name)
		case "merged":
			result.Merged = append(result.Merged, ref.name)
		case "squashed":
			result.Squashed = append(result.Squashed, ref.name)
		default:
			continue
		}
		result.Tips[ref.name] = ref.tip
	}
	return result, nil
}

// DeletePreviewedBranches rechecks only the displayed names. A stale
// observation is a skip. update-ref atomically compares the preview's OID
// while removing the ref, including when a writer moves it after our check.
func DeletePreviewedBranches(dir string, preview SweepResult) (deleted, skipped []string, errs []error) {
	def, err := sweepDefault(dir)
	if err != nil {
		return nil, nil, []error{err}
	}
	base, err := commitID(dir, "refs/heads/"+def)
	if err != nil {
		return nil, nil, []error{err}
	}
	for _, group := range []struct {
		category string
		names    []string
	}{{"gone", preview.Gone}, {"merged", preview.Merged}, {"squashed", preview.Squashed}} {
		for _, name := range group.names {
			removed, err := deleteCandidate(dir, def, base, name, group.category, preview)
			if removed {
				deleted = append(deleted, name)
			} else if err == nil {
				skipped = append(skipped, name)
			}
			if err != nil {
				errs = append(errs, err)
			}
		}
	}
	return
}

func deleteCandidate(dir, def, base, name, category string, preview SweepResult) (bool, error) {
	tip := preview.Tips[name]
	if def != preview.DefaultBranch || name == def || tip == "" {
		return false, nil
	}
	refname := "refs/heads/" + name
	refs, err := sweepRefs(dir, []string{refname})
	if err != nil {
		return false, err
	}
	if len(refs) != 1 || refs[0].name != name || refs[0].tip != tip || refs[0].protected || sweepCategory(dir, base, refs[0]) != category {
		return false, nil
	}
	// Preserve branch -d's additional check against its upstream
	// (or HEAD if untracked) for an ordinary merged branch.
	if category == "merged" {
		up := refs[0].upstream
		if up == "" {
			up = "HEAD"
		}
		if _, err := sweepRead(dir, "merge-base", "--is-ancestor", tip, up); err != nil {
			return false, nil
		}
	}
	// Category checks may run several Git commands. Refresh the default,
	// operation state and worktree occupancy just before deleting the ref.
	currentDefault, err := sweepDefault(dir)
	if err != nil {
		return false, err
	}
	protected, err := operationBranches(dir)
	if err != nil {
		return false, err
	}
	live, err := sweepRefs(dir, []string{refname})
	if err != nil {
		return false, err
	}
	if currentDefault != def || protected[name] || len(live) != 1 || live[0] != refs[0] {
		return false, nil
	}
	if err := deletePinnedBranch(dir, def, base, refname, refs[0]); err != nil {
		currentBase, baseErr := commitID(dir, "refs/heads/"+def)
		now, refErr := sweepRefs(dir, []string{refname})
		if baseErr == nil && currentBase != base || refErr == nil && (len(now) != 1 || now[0] != refs[0]) {
			return false, nil
		}
		return false, err
	}
	return true, removeBranchConfig(dir, name)
}

// Git takes both ref locks in one transaction. A moving default branch
// cannot invalidate the proof between the category check and deletion.
func deletePinnedBranch(dir, def, base, refname string, ref sweepRef) error {
	input := fmt.Sprintf("verify refs/heads/%s %s\n", def, base)
	if ref.gone {
		input += fmt.Sprintf("verify %s %s\n", ref.upstream, strings.Repeat("0", len(ref.tip)))
	}
	input += fmt.Sprintf("option no-deref\ndelete %s %s\n", refname, ref.tip)
	ctx := context.Background()
	res, err := Default.Run(ctx, Request{Dir: dir, Args: []string{"update-ref", "--stdin"}, Stdin: strings.NewReader(input)})
	if err != nil {
		return failure(ctx, "update-ref", err, res, res.combined())
	}
	return nil
}

func removeBranchConfig(dir, name string) error {
	_, err := output(dir, "config", "--local", "--name-only", "--get-regexp", "^branch[.]"+regexp.QuoteMeta(name)+"[.]")
	var ge *GitError
	if errors.As(err, &ge) && ge.ExitCode == 1 {
		return nil
	}
	if err != nil {
		return err
	}
	// A concurrent creation can already have installed settings of its own.
	if err := run(dir, "show-ref", "--verify", "--quiet", "refs/heads/"+name); err == nil {
		return nil
	} else if !errors.As(err, &ge) || ge.ExitCode != 1 {
		return err
	}
	return run(dir, "config", "--local", "--remove-section", "branch."+name)
}

// DeleteStaleBranches removes still-matching refs from a SweepBranches
// result. Call DeletePreviewedBranches to distinguish skips from failures.
func DeleteStaleBranches(dir string, result SweepResult) ([]string, []error) {
	deleted, _, errs := DeletePreviewedBranches(dir, result)
	return deleted, errs
}
