package git

import (
	"fmt"
	"strings"
)

// IncomingCommit describes one upstream commit that I have not received locally.
type IncomingCommit struct {
	Hash    string `json:"hash"`
	Author  string `json:"author"`
	Subject string `json:"subject"`
}

// IncomingCommits reads all incoming history from the last fetched upstream.
// I use local refs only and leave the index and working files untouched.
func IncomingCommits(repoPath string) ([]IncomingCommit, error) {
	out, err := output(repoPath, "--no-optional-locks", "log", "--no-color", "--no-decorate",
		"--no-show-signature", "--encoding=UTF-8", "--date-order", "-z",
		"--format=%H%x00%an%x00%s", "HEAD..@{upstream}", "--")
	if err != nil {
		return nil, err
	}
	return parseIncomingCommits(out)
}

func parseIncomingCommits(out string) ([]IncomingCommit, error) {
	commits := []IncomingCommit{}
	if out == "" {
		return commits, nil
	}
	if !strings.HasSuffix(out, "\x00") {
		return nil, fmt.Errorf("incomplete incoming commit record")
	}
	fields := strings.Split(strings.TrimSuffix(out, "\x00"), "\x00")
	if len(fields)%3 != 0 {
		return nil, fmt.Errorf("invalid incoming commit record")
	}
	for i := 0; i < len(fields); i += 3 {
		commits = append(commits, IncomingCommit{Hash: fields[i], Author: fields[i+1], Subject: fields[i+2]})
	}
	return commits, nil
}
