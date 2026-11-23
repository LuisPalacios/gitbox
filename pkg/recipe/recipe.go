// Package recipe exchanges portable fleet selections between local installations.
package recipe

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/provider"
)

// MaxSize bounds the document the window reads and previews.
const MaxSize = 4 << 20

var ErrInvalid = errors.New("invalid or unsupported fleet recipe")

type Recipe struct {
	Version  int       `json:"version"`
	Accounts []Account `json:"accounts"`
	Roots    []Root    `json:"roots"`
	Sources  []Source  `json:"sources"`
	Mirrors  []Mirror  `json:"mirrors"`
}

type AccountRef struct {
	Server   string `json:"server"`
	Username string `json:"username"`
}

type Account struct {
	Server         string `json:"server"`
	Username       string `json:"username"`
	Provider       string `json:"provider"`
	CredentialType string `json:"credentialType"`
}

func (a Account) Ref() AccountRef { return AccountRef{a.Server, a.Username} }

type Root struct {
	Slot string `json:"slot"`
}
type Source struct {
	AccountRef AccountRef `json:"accountRef"`
	Folder     string     `json:"folder,omitempty"`
	RootSlot   string     `json:"rootSlot"`
	Repos      []Repo     `json:"repos"`
}
type Repo struct {
	Key         string `json:"key"`
	IDFolder    string `json:"idFolder,omitempty"`
	CloneFolder string `json:"cloneFolder,omitempty"`
	RootSlot    string `json:"rootSlot,omitempty"`
}
type Mirror struct {
	From  AccountRef   `json:"from"`
	To    AccountRef   `json:"to"`
	Repos []MirrorRepo `json:"repos"`
}
type MirrorRepo struct {
	Key       string `json:"key"`
	Direction string `json:"direction"`
	Origin    string `json:"origin"`
	Target    string `json:"target,omitempty"`
}

func slot(i int) string {
	if i == 0 {
		return "primary"
	}
	return "secondary-" + strconv.Itoa(i)
}

// Export projects the allowlist. Machine-local clone overrides use the
// portable default on the destination.
func Export(cfg *config.Config) (Recipe, error) {
	r := Recipe{1, []Account{}, []Root{}, []Source{}, []Mirror{}}
	for i := range cfg.Global.Roots {
		r.Roots = append(r.Roots, Root{slot(i)})
	}
	rootSlot := func(path string) string {
		for i, root := range cfg.Global.Roots {
			if config.SameRoot(root, path) {
				return slot(i)
			}
		}
		return ""
	}
	refs := map[string]AccountRef{}
	for _, k := range slices.Sorted(maps.Keys(cfg.Accounts)) {
		a := cfg.Accounts[k]
		server, ok := canonicalServer(a.URL)
		if !ok {
			return Recipe{}, ErrInvalid
		}
		typ := a.DefaultCredentialType
		if typ == "" {
			typ = "gcm"
		}
		account := Account{server, a.Username, a.Provider, typ}
		r.Accounts = append(r.Accounts, account)
		refs[k] = account.Ref()
	}
	for _, k := range slices.Sorted(maps.Keys(cfg.Sources)) {
		s := cfg.Sources[k]
		folder := s.EffectiveFolder(k)
		if folder == s.Account {
			folder = ""
		}
		src := Source{refs[s.Account], folder, rootSlot(cfg.SourceRoot(k)), []Repo{}}
		for _, key := range slices.Sorted(maps.Keys(s.Repos)) {
			v := s.Repos[key]
			repo := Repo{Key: key}
			if relativePath(v.CloneFolder) {
				repo.CloneFolder = v.CloneFolder
			}
			if relativePath(v.IdFolder) {
				repo.IDFolder = v.IdFolder
			}
			if v.Root != "" && !config.SameRoot(v.Root, cfg.SourceRoot(k)) {
				repo.RootSlot = rootSlot(v.Root)
				if repo.RootSlot == "" {
					return Recipe{}, ErrInvalid
				}
			}
			src.Repos = append(src.Repos, repo)
		}
		r.Sources = append(r.Sources, src)
	}
	for _, k := range slices.Sorted(maps.Keys(cfg.Mirrors)) {
		m := cfg.Mirrors[k]
		mirror := Mirror{refs[m.AccountSrc], refs[m.AccountDst], []MirrorRepo{}}
		for _, name := range slices.Sorted(maps.Keys(m.Repos)) {
			repo := m.Repos[name]
			mirror.Repos = append(mirror.Repos, MirrorRepo{name, repo.Direction, repo.Origin, repo.TargetRepo})
		}
		r.Mirrors = append(r.Mirrors, mirror)
	}
	return r, r.validate()
}

func canonicalServer(raw string) (string, bool) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.RawPath != "" {
		return "", false
	}
	base := strings.TrimSuffix(u.Path, "/")
	if base != "" && !relativePath(strings.TrimPrefix(base, "/")) {
		return "", false
	}
	u.Host = strings.ToLower(u.Host)
	u.Path = base
	return u.String(), true
}

var segment = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._-]*$`)
var device = regexp.MustCompile(`(?i)^(CON|PRN|AUX|NUL|COM[1-9]|LPT[1-9])([.]|$)`)

func relativePath(s string) bool {
	for part := range strings.SplitSeq(s, "/") {
		if part == "" || strings.TrimSpace(part) != part || strings.HasSuffix(part, ".") || strings.HasPrefix(part, "~") || strings.EqualFold(part, ".git") || strings.ContainsAny(part, `<>:"\|?*@`) || strings.IndexFunc(part, unicode.IsControl) >= 0 || device.MatchString(part) || len(part) > 255 {
			return false
		}
	}
	return true
}
func repoName(s string) bool { return strings.Contains(s, "/") && relativePath(s) }
func (r Recipe) validate() error {
	if r.Version != 1 || len(r.Roots) == 0 || r.Accounts == nil || r.Sources == nil || r.Mirrors == nil {
		return ErrInvalid
	}
	roots := map[string]bool{}
	for i, root := range r.Roots {
		if root.Slot != slot(i) {
			return ErrInvalid
		}
		roots[root.Slot] = true
	}
	accounts := map[AccountRef]bool{}
	for _, a := range r.Accounts {
		server, ok := canonicalServer(a.Server)
		_, err := provider.ByName(a.Provider)
		if !ok || server != a.Server || !segment.MatchString(a.Username) || len(a.Username) > 255 || err != nil || !slices.Contains([]string{"token", "gcm", "ssh"}, a.CredentialType) || accounts[a.Ref()] {
			return ErrInvalid
		}
		accounts[a.Ref()] = true
	}
	type sourceID struct {
		AccountRef
		folder string
	}
	sources := map[sourceID]bool{}
	for _, s := range r.Sources {
		id := sourceID{s.AccountRef, s.Folder}
		if !accounts[s.AccountRef] || !roots[s.RootSlot] || (s.Folder != "" && !relativePath(s.Folder)) || sources[id] || s.Repos == nil {
			return ErrInvalid
		}
		sources[id] = true
		keys := map[string]bool{}
		for _, v := range s.Repos {
			if keys[v.Key] || !repoName(v.Key) || (v.RootSlot != "" && !roots[v.RootSlot]) || (v.IDFolder != "" && !relativePath(v.IDFolder)) || (v.CloneFolder != "" && !relativePath(v.CloneFolder)) {
				return ErrInvalid
			}
			keys[v.Key] = true
		}
	}
	for _, m := range r.Mirrors {
		if m.From == m.To || !accounts[m.From] || !accounts[m.To] || m.Repos == nil {
			return ErrInvalid
		}
		keys := map[string]bool{}
		for _, v := range m.Repos {
			if keys[v.Key] || !repoName(v.Key) || (v.Target != "" && !repoName(v.Target)) || (v.Direction != "push" && v.Direction != "pull") || (v.Origin != "src" && v.Origin != "dst") {
				return ErrInvalid
			}
			keys[v.Key] = true
		}
	}
	return nil
}

func Decode(data string) (Recipe, error) {
	var r Recipe
	if len(data) > MaxSize {
		return r, ErrInvalid
	}
	d := json.NewDecoder(strings.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(&r); err != nil {
		return Recipe{}, ErrInvalid
	}
	if d.Decode(new(json.RawMessage)) != io.EOF {
		return Recipe{}, ErrInvalid
	}
	return r, r.validate()
}

func (r Recipe) Document() (string, error) {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return "", err
	}
	if len(data)+1 > MaxSize {
		return "", fmt.Errorf("fleet recipe exceeds %d bytes", MaxSize)
	}
	return string(data) + "\n", nil
}
