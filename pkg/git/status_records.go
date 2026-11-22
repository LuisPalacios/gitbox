package git

import (
	"context"
	"errors"
	"strconv"
	"strings"
)

var errMalformedStatus = errors.New("malformed status output")

type statusRecord struct {
	kind     byte
	xy       string
	path     string
	origPath string
}

type statusSnapshot struct {
	branch  RepoStatus
	records []statusRecord
}

func readStatus(ctx context.Context, repoPath string, opts ...string) (statusSnapshot, error) {
	args := append([]string{"--no-optional-locks", "status", "--porcelain=v2", "--branch", "-z"}, opts...)
	out, err := outputContext(ctx, repoPath, args...)
	if err != nil {
		return statusSnapshot{}, err
	}
	status, err := parseStatusZ(out)
	if err != nil {
		// Neither the malformed record nor the command output belongs in errors.
		gitErr := newGitErrorContext(context.Background(), "status", err, nil)
		gitErr.Detail = "malformed output"
		return statusSnapshot{}, gitErr
	}
	return status, nil
}

// parseStatusZ reads porcelain v2 records without trimming or unquoting paths.
// A rename or copy consumes an extra NUL-terminated original path.
func parseStatusZ(out string) (statusSnapshot, error) {
	var status statusSnapshot
	for out != "" {
		line, rest, ok := strings.Cut(out, "\x00")
		if !ok || len(line) < 3 || line[1] != ' ' {
			return statusSnapshot{}, errMalformedStatus
		}
		out = rest
		record := statusRecord{kind: line[0]}
		switch record.kind {
		case '#':
			if err := readStatusHeader(&status.branch, line[2:]); err != nil {
				return statusSnapshot{}, err
			}
			continue
		case '?':
			record.path = line[2:]
		case '!':
			continue
		case '1', '2', 'u':
			record, ok = readTrackedStatusRecord(line)
			if !ok {
				return statusSnapshot{}, errMalformedStatus
			}
			if record.kind == '2' {
				record.origPath, out, ok = strings.Cut(out, "\x00")
				if !ok || record.origPath == "" {
					return statusSnapshot{}, errMalformedStatus
				}
			}
		default:
			return statusSnapshot{}, errMalformedStatus
		}
		status.records = append(status.records, record)
	}
	return status, nil
}

func readStatusHeader(branch *RepoStatus, line string) error {
	key, value, _ := strings.Cut(line, " ")
	switch key {
	case "branch.head", "branch.upstream":
		if value == "" {
			return errMalformedStatus
		}
		if key == "branch.head" {
			branch.Branch = value
		} else {
			branch.Upstream = value
		}
	case "branch.ab":
		// Git may print placeholders when it skips the ahead/behind count.
		if value == "+? -?" {
			branch.Ahead, branch.Behind = 0, 0
			return nil
		}
		ahead, behind, ok := strings.Cut(value, " ")
		if !ok || !strings.HasPrefix(ahead, "+") || !strings.HasPrefix(behind, "-") {
			return errMalformedStatus
		}
		a, errA := strconv.ParseUint(ahead[1:], 10, strconv.IntSize-1)
		b, errB := strconv.ParseUint(behind[1:], 10, strconv.IntSize-1)
		if errA != nil || errB != nil {
			return errMalformedStatus
		}
		branch.Ahead, branch.Behind = int(a), int(b)
	}
	return nil
}

func readTrackedStatusRecord(line string) (statusRecord, bool) {
	record := statusRecord{kind: line[0]}
	xy, rest, ok := strings.Cut(line[2:], " ")
	if !ok || len(xy) != 2 || strings.IndexByte(".MTADRCU", xy[0]) < 0 || strings.IndexByte(".MTADRCU", xy[1]) < 0 {
		return statusRecord{}, false
	}
	// The path follows the remaining fixed metadata fields. It can start
	// with a space, so I consume exactly the separators before the path.
	fields := 6
	switch record.kind {
	case '2':
		fields = 7
	case 'u':
		fields = 8
	}
	for range fields {
		var field string
		field, rest, ok = strings.Cut(rest, " ")
		if !ok || field == "" {
			return statusRecord{}, false
		}
	}
	if rest == "" {
		return statusRecord{}, false
	}
	record.xy, record.path = xy, rest
	return record, true
}

func (s statusSnapshot) summary() RepoStatus {
	summary := s.branch
	for _, record := range s.records {
		switch record.kind {
		case '?':
			summary.Untracked++
		case 'u':
			summary.Conflicts++
		case '1', '2':
			summary.Changed++
			index, work := record.xy[0], record.xy[1]
			if index != '.' {
				summary.Added++
			}
			if work == 'M' || work == 'A' || work == 'D' {
				summary.Modified++
			}
			if index == 'D' || work == 'D' {
				summary.Deleted++
			}
		}
	}
	return summary
}

func (s statusSnapshot) details() (changed []FileChange, untracked []string) {
	changed = []FileChange{}
	untracked = []string{}
	for _, record := range s.records {
		switch record.kind {
		case '?':
			untracked = append(untracked, record.path)
		case 'u':
			changed = append(changed, FileChange{Kind: "conflict", Path: record.path})
		case '2':
			changed = append(changed, FileChange{Kind: "renamed", Path: record.origPath + " → " + record.path})
		case '1':
			kind := "modified"
			if record.xy[0] == 'D' || record.xy[1] == 'D' {
				kind = "deleted"
			} else if record.xy[0] == 'A' || record.xy[1] == 'A' {
				kind = "added"
			}
			changed = append(changed, FileChange{Kind: kind, Path: record.path})
		}
	}
	return changed, untracked
}
