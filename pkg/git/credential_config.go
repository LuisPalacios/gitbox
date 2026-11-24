package git

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

var configSubsectionEscaper = strings.NewReplacer(`\`, `\\`, `"`, `\"`)

// MatchingCredentialConfig returns matching HTTPS credential settings in Git's
// config order. The target must be the app's canonical URL, with a validated
// account username and repository path. No raw setting value goes to a child
// process and the synthetic config is read from stdin.
func (s ConfigSnapshot) MatchingCredentialConfig(ctx context.Context, path, target string) ([]ConfigEntry, error) {
	origin, err := url.Parse(target)
	if err != nil || (origin.Scheme != "https" && origin.Scheme != "http") {
		return nil, err
	}
	origin.RawQuery, origin.Fragment, origin.ForceQuery = "", "", false
	username := ""
	if origin.User != nil {
		username = origin.User.Username()
		origin.User = url.User("identity-proof")
	}

	var patterns strings.Builder
	var indexes []int
	matched := make(map[int]bool)
	for index, entry := range s.Entries {
		section, ok := strings.CutPrefix(entry.Key, "credential.")
		if !ok {
			continue
		}
		subsection, setting, scoped := splitCredentialSection(section)
		if setting != "helper" && setting != "username" {
			continue
		}
		if !scoped {
			matched[index] = true
			continue
		}
		if strings.ContainsAny(subsection, "\r\n\x00") {
			continue
		}
		if !strings.Contains(subsection, "://") {
			matched[index] = partialCredentialMatch(subsection, origin, username)
			continue
		}
		pattern, err := url.Parse(subsection)
		if err != nil {
			continue
		}
		if pattern.User != nil {
			if origin.User == nil || pattern.User.Username() != username {
				continue
			}
			pattern.User = url.User("identity-proof")
		}
		// Each entry receives a distinct key, so get-urlmatch cannot discard
		// duplicate helpers or select just the most specific subsection.
		fmt.Fprintf(&patterns, "[credential \"%s\"]\nproof%d = true\n",
			configSubsectionEscaper.Replace(pattern.String()), len(indexes))
		indexes = append(indexes, index)
	}
	if len(indexes) > 0 {
		result, err := Run(ctx, Request{
			Dir: path, Args: []string{"config", "--file", "-", "--null", "--get-urlmatch", "credential", origin.String()},
			Stdin: strings.NewReader(patterns.String()),
		})
		if result.Truncated {
			return nil, errOutputTruncated
		}
		if err != nil && result.ExitCode != 1 {
			return nil, err
		}
		for record := range strings.SplitSeq(string(result.Stdout), "\x00") {
			key, _, _ := strings.Cut(record, "\n")
			if number, ok := strings.CutPrefix(key, "credential.proof"); ok {
				if parsed, err := strconv.Atoi(number); err == nil && parsed >= 0 && parsed < len(indexes) {
					matched[indexes[parsed]] = true
				}
			}
		}
	}
	var entries []ConfigEntry
	for index, entry := range s.Entries {
		if matched[index] {
			entries = append(entries, entry)
		}
	}
	return entries, nil
}

func splitCredentialSection(section string) (subsection, setting string, scoped bool) {
	if index := strings.LastIndexByte(section, '.'); index >= 0 {
		return section[:index], section[index+1:], true
	}
	return "", section, false
}

// Git's credential lookup falls back to an exact match of each available
// field for partial URLs, independently from the full URL prefix rules.
func partialCredentialMatch(subsection string, origin *url.URL, username string) bool {
	u, err := url.Parse(origin.Scheme + "://" + subsection)
	return err == nil &&
		(u.Host == "" || u.Host == origin.Host) &&
		(u.Path == "" || u.Path == origin.Path) &&
		(u.User == nil || (origin.User != nil && u.User.Username() == username))
}
