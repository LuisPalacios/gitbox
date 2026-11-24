// Package diag projects local application state into the support bundle.
package diag

import (
	"encoding/json"
	"errors"
	"regexp"
	"runtime"
	"time"

	"github.com/LuisPalacios/gitbox/pkg/config"
)

const SchemaVersion = 1

type Bundle struct {
	SchemaVersion int              `json:"schemaVersion"`
	AppVersion    string           `json:"appVersion"`
	Commit        string           `json:"commit"`
	OS            string           `json:"os"`
	Arch          string           `json:"arch"`
	GitVersion    string           `json:"gitVersion"`
	Counts        Counts           `json:"counts"`
	Operations    []Operation      `json:"operations"`
	Scheduler     []Task           `json:"scheduler"`
	StartupMS     map[string]int64 `json:"startupMs"`
}

type Counts struct {
	AccountsByProvider map[string]int `json:"accountsByProvider"`
	Credentials        map[string]int `json:"credentials"`
	Sources            int            `json:"sources"`
	Repos              int            `json:"repos"`
	Roots              int            `json:"roots"`
}

// State is input to Collect. Only the fields explicitly projected below may
// leave the process.
type State struct {
	AppVersion string
	Commit     string
	GitVersion string
	Config     *config.Config
	Activity   *Activity
	Scheduler  []Task
	StartupMS  map[string]int64
}

type Task struct {
	Name       string `json:"name"`
	NextInMS   *int64 `json:"nextInMs,omitempty"`
	InFlight   bool   `json:"inFlight"`
	LastResult string `json:"lastResult,omitempty"`
}

var (
	appVersionRE = regexp.MustCompile(`^(?:dev|(?:v?[0-9]{1,6}(?:\.[0-9]{1,6}){2}(?:-(?:alpha|beta|rc)[.-]?[0-9]{0,6})?(?:-[0-9]{1,8}-g[0-9a-f]{7,40})?|[0-9a-f]{7,40})(?:-dirty)?(?:-(?:dev|gate))?)$`)
	commitRE     = regexp.MustCompile(`^(?:none|[0-9a-f]{7,40})$`)
	gitVersionRE = regexp.MustCompile(`^git version ([0-9]{1,6}(?:\.[0-9]{1,6}){2,3}(?:\.(?:windows|msysgit)\.[0-9]{1,6})?)(?:\r?\n)?$`)
	appleGitRE   = regexp.MustCompile(`^git version ([0-9]{1,6}(?:\.[0-9]{1,6}){2}) \(Apple Git-[0-9]{1,6}(?:\.[0-9]{1,6})*\)(?:\r?\n)?$`)
)

func Collect(state State, now time.Time) Bundle {
	b := Bundle{
		SchemaVersion: SchemaVersion,
		AppVersion:    "unknown",
		Commit:        "unknown",
		OS:            runtime.GOOS,
		Arch:          runtime.GOARCH,
		GitVersion:    "unknown",
		Counts:        countConfig(state.Config),
		Operations:    state.Activity.recent(now),
		Scheduler:     make([]Task, 0, len(state.Scheduler)),
		StartupMS:     make(map[string]int64),
	}
	if appVersionRE.MatchString(state.AppVersion) {
		b.AppVersion = state.AppVersion
	}
	if commitRE.MatchString(state.Commit) {
		b.Commit = state.Commit
	}
	if match := gitVersionRE.FindStringSubmatch(state.GitVersion); match != nil {
		b.GitVersion = match[1]
	} else if match := appleGitRE.FindStringSubmatch(state.GitVersion); match != nil {
		b.GitVersion = match[1]
	}
	for _, task := range state.Scheduler {
		switch task.Name {
		case "fetch-all", "mirror-status", "pr-refresh", "update-check", "harness-detect":
			b.Scheduler = append(b.Scheduler, Task{
				Name:       task.Name,
				NextInMS:   task.NextInMS,
				InFlight:   task.InFlight,
				LastResult: taskResult(task.LastResult),
			})
		}
	}
	for name, ms := range state.StartupMS {
		switch name {
		case "decode", "mount", "snapshot", "firstRow", "firstStatus", "firstRemote", "fullStatus":
			b.StartupMS[name] = ms
		}
	}
	return b
}

func taskResult(result string) string {
	switch result {
	case "ok", "failed", "offline", "cancelled":
		return result
	default:
		return ""
	}
}

func countConfig(cfg *config.Config) Counts {
	c := Counts{AccountsByProvider: make(map[string]int), Credentials: make(map[string]int)}
	if cfg == nil {
		return c
	}
	c.Sources = len(cfg.Sources)
	c.Roots = len(cfg.Global.Roots)
	for _, account := range cfg.Accounts {
		switch account.Provider {
		case "github", "gitlab", "gitea", "forgejo", "bitbucket":
			c.AccountsByProvider[account.Provider]++
		default:
			c.AccountsByProvider["other"]++
		}
		countCredential(c.Credentials, account.DefaultCredentialType)
	}
	for _, source := range cfg.Sources {
		c.Repos += len(source.Repos)
		for _, repo := range source.Repos {
			// The account default is already counted. Count explicit overrides separately.
			countCredential(c.Credentials, repo.CredentialType)
		}
	}
	return c
}

func countCredential(counts map[string]int, credentialType string) {
	switch credentialType {
	case "":
	case "gcm", "ssh", "token":
		counts[credentialType]++
	default:
		counts["other"]++
	}
}

// Redact is a fail-closed check after serialization. Collect's allowlist
// remains the boundary for data that does not match a recognizable pattern.
func Redact(document string) (string, error) {
	if sensitiveText.MatchString(document) {
		return "", errors.New("diagnostics.unsafe")
	}
	return document, nil
}

var sensitiveText = regexp.MustCompile(`(?i)(?:://|@|"(?:/|~[/\\]|[a-z]:[/\\]|\\\\)|-----BEGIN |(?:gh[pousr]_|github_pat_|glpat-)[a-z0-9_-]+)`)

func (b Bundle) Document() (string, error) {
	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return "", err
	}
	return Redact(string(data) + "\n")
}
