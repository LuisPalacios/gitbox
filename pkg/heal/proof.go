package heal

import (
	"context"
	"net/url"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/credential"
	"github.com/LuisPalacios/gitbox/pkg/git"
	"github.com/LuisPalacios/gitbox/pkg/identity"
	"github.com/LuisPalacios/gitbox/pkg/status"
)

// IdentityProof carries display-safe evidence only. Raw Git config never
// leaves the host through this type.
type IdentityProof struct {
	Account        string          `json:"account"`
	Server         string          `json:"server"`
	Username       string          `json:"username"`
	NameSource     string          `json:"nameSource"`
	EmailSource    string          `json:"emailSource"`
	OriginURL      string          `json:"originURL"`
	HelperChain    []string        `json:"helperChain"`
	CredentialFile string          `json:"credentialFile,omitempty"`
	Overrides      []ProofOverride `json:"overrides"`
	LastVerified   *time.Time      `json:"lastVerified,omitempty"`
	Verdict        string          `json:"verdict"`
	Reason         string          `json:"reason,omitempty"`
}

// ProofOverride identifies a setting that contradicts the configured account.
type ProofOverride struct {
	Scope string `json:"scope"`
	Key   string `json:"key"`
	Value string `json:"value"`
}

// Verification describes a previous successful check; proof never starts one.
type Verification struct {
	CheckedAt time.Time
	ExpiresAt time.Time
	Failed    bool
}

// Proof reads a clone's identity without repairing it or checking credentials.
// The caller can supply metadata from a credential check it already ran.
func Proof(ctx context.Context, cfg *config.Config, source, repoKey string, check Verification, now time.Time) IdentityProof {
	p := IdentityProof{Verdict: "unsafe", Reason: "unavailable", HelperChain: []string{}, Overrides: []ProofOverride{}}
	if cfg == nil {
		return p
	}
	src, sourceFound := cfg.Sources[source]
	repo, repoFound := src.Repos[repoKey]
	acct, accountFound := cfg.Accounts[src.Account]
	if !sourceFound || !repoFound || !accountFound {
		return p
	}
	p.Account = src.Account
	p.Username = acct.Username
	_, p.Server = proofOrigin(acct.URL, "")
	p.NameSource = identitySource(repo.Name, acct.Name)
	p.EmailSource = identitySource(repo.Email, acct.Email)
	if !check.CheckedAt.IsZero() {
		checked := check.CheckedAt.UTC()
		p.LastVerified = &checked
	}

	path := status.RepoPath(cfg, source, repoKey, repo)
	if !git.IsRepo(path) && !git.IsLinkedWorktree(path) {
		return p
	}
	snapshot, err := git.ReadConfigSnapshotContext(ctx, path)
	if err != nil {
		return p
	}

	credType := repo.EffectiveCredentialType(&acct)
	if credType != "ssh" && credType != "token" && credType != "gcm" {
		return p
	}
	p.Reason = ""
	name, email := identity.ResolveIdentity(repo, acct)
	for _, setting := range []struct{ field, want string }{{"name", name}, {"email", email}} {
		key := "user." + setting.field
		if setting.want == "" || snapshot.Get(key) != setting.want {
			p.addOverride(snapshot.Entries, key)
		}
		for _, role := range []string{"author", "committer"} {
			key := role + "." + setting.field
			if len(snapshot.All(key)) > 0 && snapshot.Get(key) != setting.want {
				p.addOverride(snapshot.Entries, key)
			}
			envKey := "GIT_" + strings.ToUpper(role+"_"+setting.field)
			if value, exists := os.LookupEnv(envKey); exists && value != setting.want {
				p.Overrides = append(p.Overrides, ProofOverride{Scope: "environment", Key: envKey, Value: "redacted"})
			}
		}
	}

	wantURL := ExpectedOriginURL(acct, repoKey, credType)
	origin, err := git.ResolvedOrigin(ctx, path)
	p.OriginURL, _ = proofOrigin(origin, wantURL)
	// The complete comparison also catches a different SSH alias, HTTP user
	// or repository on the same server, and credentials embedded in the URL.
	if err != nil || origin != wantURL {
		key := "remote.origin.url"
		if stored := snapshot.All(key); len(stored) > 0 && stored[0] == wantURL && err == nil {
			p.Overrides = append(p.Overrides, ProofOverride{Key: "url.*.insteadOf", Value: "redacted"})
		} else {
			p.addOverride(snapshot.Entries, key)
		}
	}

	wantHelpers := credential.RepoHelpers(src.Account, credType, cfg.Global)

	var settings []git.ConfigEntry
	var values []string
	originURL, _ := url.Parse(wantURL)
	username := ""
	if originURL != nil && originURL.User != nil {
		username = originURL.User.Username()
	}
	hasURLUser := username != ""
	var userSetting git.ConfigEntry
	credentialEntries := snapshot.Entries
	if credType != "ssh" && origin == wantURL {
		credentialEntries, err = snapshot.MatchingCredentialConfig(ctx, path, wantURL)
		if err != nil {
			p.Reason = "unavailable"
			return p
		}
	}
	for _, entry := range credentialEntries {
		switch {
		case entry.Key == "credential.helper" || (credType != "ssh" && origin == wantURL && strings.HasSuffix(entry.Key, ".helper")):
			settings = append(settings, entry)
			values = append(values, entry.Value)
		case strings.HasSuffix(entry.Key, ".username") && credType != "ssh" && origin == wantURL && !hasURLUser:
			username, userSetting = entry.Value, entry
		}
	}
	helpers := credential.EffectiveHelperChain(values)
	for _, helper := range helpers {
		switch {
		case helper == "":
			p.HelperChain = append(p.HelperChain, "reset")
		case len(wantHelpers) == 2 && helper == wantHelpers[1] && credType == "token":
			p.HelperChain = append(p.HelperChain, "store")
			p.CredentialFile = credential.CredentialFilePath(src.Account)
		case len(wantHelpers) == 2 && helper == wantHelpers[1] && credType == "gcm":
			p.HelperChain = append(p.HelperChain, "manager")
		default:
			p.HelperChain = append(p.HelperChain, "redacted")
		}
	}

	if !slices.Equal(helpers, wantHelpers) {
		scope, key := "", "credential.helper"
		for _, entry := range settings[len(values)-len(helpers):] {
			if entry.Key != "credential.helper" {
				scope, key = entry.Scope, "credential.*.helper"
			} else if key == "credential.helper" {
				scope = entry.Scope
			}
		}
		p.Overrides = append(p.Overrides, ProofOverride{Scope: scope, Key: key, Value: "redacted"})
	}
	if credType != "ssh" && !hasURLUser && username != acct.Username {
		p.Overrides = append(p.Overrides, ProofOverride{Scope: userSetting.Scope, Key: "credential.*.username", Value: "redacted"})
	}

	switch {
	case len(p.Overrides) > 0:
		p.Reason = "mismatch"
	case check.Failed:
		p.Verdict, p.Reason = "warning", "checkFailed"
	case !check.ExpiresAt.IsZero() && !check.ExpiresAt.After(now.Add(14*24*time.Hour)):
		p.Verdict, p.Reason = "warning", "expires"
	case check.CheckedAt.IsZero():
		p.Verdict, p.Reason = "warning", "unverified"
	case now.Sub(check.CheckedAt) > 24*time.Hour:
		p.Verdict, p.Reason = "warning", "stale"
	default:
		p.Verdict = "safe"
	}
	return p
}

func identitySource(repoValue, accountValue string) string {
	switch {
	case repoValue != "":
		return "repo"
	case accountValue != "":
		return "account"
	default:
		return "unset"
	}
}

func (p *IdentityProof) addOverride(entries []git.ConfigEntry, key string) {
	scope := ""
	for _, entry := range entries {
		if entry.Key == key {
			scope = entry.Scope
			if key == "remote.origin.url" {
				break
			}
		}
	}
	p.Overrides = append(p.Overrides, ProofOverride{Scope: scope, Key: key, Value: "redacted"})
}

// proofOrigin exposes a supported network origin's server and a path only
// when it matches the configured repo. User info, query and fragment are
// never necessary to identify the server.
func proofOrigin(raw, expected string) (display, server string) {
	if raw == "" {
		return "", ""
	}
	networkURL := func(value string) (*url.URL, error) {
		if !strings.Contains(value, "://") {
			_, rest, hasUser := strings.Cut(value, "@")
			host, path, hasPath := strings.Cut(rest, ":")
			if hasUser && hasPath {
				value = "ssh://" + host + "/" + path
			}
		}
		return url.Parse(value)
	}
	u, err := networkURL(raw)
	if err != nil || u.Hostname() == "" {
		return "", ""
	}
	switch u.Scheme {
	case "http", "https", "ssh", "git":
	default:
		return "", ""
	}
	path := ""
	want, _ := networkURL(expected)
	if want != nil && want.Path == u.Path {
		path = u.Path
	} else if u.Path != "" {
		path = "/…"
	}
	return (&url.URL{Scheme: u.Scheme, Host: u.Host, Path: path}).String(), u.Host
}
