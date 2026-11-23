package recipe

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"maps"
	"net/mail"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/ops"
	"github.com/LuisPalacios/gitbox/pkg/status"
)

// Options carries only destination-machine choices. Identities are indexed
// by server/username and are required only when creating a local account.
type Options struct {
	Slots      map[string]string   `json:"slots"`
	Identities map[string]Identity `json:"identities"`
}
type Identity struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}
type Row struct {
	Kind  string `json:"kind"`
	Item  string `json:"item"`
	State string `json:"state"`
}
type ImportPlan struct {
	Rows       []Row  `json:"rows"`
	Digest     string `json:"digest"`
	Collisions int    `json:"collisions"`
	Accounts   int    `json:"accounts"`
	Sources    int    `json:"sources"`
	Repos      int    `json:"repos"`
	result     *config.Config
	base       config.Hash
}

func (r AccountRef) label() string { return r.Server + "/" + r.Username }

// accountKeys matches portable references with the keys on this machine.
// Ambiguous existing identities need a local decision before import.
func accountKeys(c *config.Config, r Recipe) (map[AccountRef]string, error) {
	out := map[AccountRef]string{}
	for _, a := range r.Accounts {
		out[a.Ref()] = ""
	}
	for k, a := range c.Accounts {
		server, ok := canonicalServer(a.URL)
		if !ok {
			continue
		}
		ref := AccountRef{server, a.Username}
		prev, wanted := out[ref]
		if !wanted {
			continue
		}
		if prev != "" {
			return nil, errors.New("multiple local accounts match a recipe identity")
		}
		out[ref] = k
	}
	return out, nil
}

func NewAccounts(c *config.Config, r Recipe) ([]AccountRef, error) {
	keys, err := accountKeys(c, r)
	if err != nil {
		return nil, err
	}
	missing := []AccountRef{}
	for _, a := range r.Accounts {
		if keys[a.Ref()] == "" {
			missing = append(missing, a.Ref())
		}
	}
	return missing, nil
}

// Plan builds and validates the complete proposed configuration on a deep
// copy. It never saves, clones, moves folders, or prepares credentials.
func Plan(c *config.Config, r Recipe, options Options) (ImportPlan, error) {
	if err := r.validate(); err != nil {
		return ImportPlan{}, err
	}
	for _, root := range r.Roots {
		path := options.Slots[root.Slot]
		if path == "" || !c.HasRoot(path) {
			return ImportPlan{}, errors.New("map every slot to a configured clone root")
		}
	}
	planned, err := c.Clone()
	if err != nil {
		return ImportPlan{}, err
	}
	keys, err := accountKeys(c, r)
	if err != nil {
		return ImportPlan{}, err
	}
	p := ImportPlan{Rows: []Row{}, result: planned, base: config.DomainHash(c)}
	for _, a := range r.Accounts {
		ref := a.Ref()
		key := keys[ref]
		if key != "" {
			if planned.Accounts[key].Provider != a.Provider {
				p.row("account", ref.label(), "collision")
			} else {
				p.row("account", ref.label(), "keep")
			}
			continue
		}
		id := options.Identities[ref.label()]
		addr, err := mail.ParseAddress(id.Email)
		if err != nil || addr.Address != id.Email || strings.TrimSpace(id.Name) == "" || strings.ContainsAny(id.Name, "\x00\r\n") {
			return ImportPlan{}, errors.New("enter a local Git author name and email for each new account")
		}
		key = ops.SuggestAccountKey(planned, a.Provider, a.Username)
		err = ops.AddAccount(planned, key, config.Account{Provider: a.Provider, URL: a.Server, Username: a.Username, DefaultCredentialType: a.CredentialType, Name: id.Name, Email: id.Email})
		if err != nil {
			return ImportPlan{}, ErrInvalid
		}
		// AddAccount supplies credential defaults and an empty source. The
		// recipe carries the actual source set, including its folder and root.
		if err := planned.DeleteSource(key); err != nil {
			return ImportPlan{}, err
		}
		keys[ref] = key
		p.row("account", ref.label(), "add")
	}
	paths := map[string]bool{}
	for sk, s := range planned.Sources {
		for k, v := range s.Repos {
			paths[pathKey(status.RepoPath(planned, sk, k, v))] = true
		}
	}
	for _, s := range r.Sources {
		if err := p.addSource(s, keys[s.AccountRef], options.Slots, paths); err != nil {
			return ImportPlan{}, err
		}
	}
	for _, m := range r.Mirrors {
		if err := p.addMirror(m, keys); err != nil {
			return ImportPlan{}, err
		}
	}
	h := sha256.New()
	for _, v := range []any{p.base, r, options, p.Rows} {
		if err := json.NewEncoder(h).Encode(v); err != nil {
			return ImportPlan{}, err
		}
	}
	p.Digest = hex.EncodeToString(h.Sum(nil))
	return p, nil
}

func (p *ImportPlan) row(kind, item, state string) {
	p.Rows = append(p.Rows, Row{kind, item, state})
	if state == "collision" {
		p.Collisions++
		return
	}
	if state != "add" {
		return
	}
	switch kind {
	case "account":
		p.Accounts++
	case "source":
		p.Sources++
	case "repo":
		p.Repos++
	}
}

func (p *ImportPlan) addSource(s Source, account string, slots map[string]string, paths map[string]bool) error {
	cfg := p.result
	folder := s.Folder
	if folder == "" {
		folder = account
	}
	name := s.AccountRef.label()
	if s.Folder != "" {
		name += "/" + s.Folder
	}
	key := ""
	for sk, existing := range cfg.Sources {
		if existing.Account == account && existing.EffectiveFolder(sk) == folder {
			if key != "" {
				p.row("source", name, "collision")
				return nil
			}
			key = sk
		}
	}
	if key == "" {
		key = unusedKey(cfg.Sources, account)
		if err := cfg.AddSource(key, config.Source{Account: account, Folder: folder}); err != nil {
			return ErrInvalid
		}
		cfg.SetSourceRoot(key, slots[s.RootSlot])
		p.row("source", name, "add")
	} else {
		p.row("source", name, "keep")
	}
	for _, v := range s.Repos {
		if _, exists := cfg.Sources[key].Repos[v.Key]; exists {
			p.row("repo", v.Key, "keep")
			continue
		}
		root := slots[s.RootSlot]
		if v.RootSlot != "" {
			root = slots[v.RootSlot]
		}
		entry := config.Repo{IdFolder: v.IDFolder, CloneFolder: v.CloneFolder}
		if !config.SameRoot(root, cfg.SourceRoot(key)) {
			entry.Root = cfg.ListedRoot(root)
		}
		root = config.ExpandTilde(cfg.ListedRoot(root))
		path := status.RepoPath(cfg, key, v.Key, entry)
		id := pathKey(path)
		if paths[id] || cfg.IsIgnoredClone(path) || !availablePath(root, path) {
			p.row("repo", v.Key, "collision")
			continue
		}
		if err := cfg.AddRepo(key, v.Key, entry); err != nil {
			return ErrInvalid
		}
		src := cfg.Sources[key]
		if len(src.RepoOrder) > 0 {
			src.RepoOrder = append(src.RepoOrder, v.Key)
			cfg.Sources[key] = src
		}
		paths[id] = true
		p.row("repo", v.Key, "add")
	}
	return nil
}

// Resolve the existing prefix before comparing paths, including a symlink
// used as a configured root, and fold case consistently with config.SamePath.
func pathKey(path string) string {
	path = filepath.Clean(path)
	parent := path
	for {
		if real, err := filepath.EvalSymlinks(parent); err == nil {
			tail, _ := filepath.Rel(parent, path)
			return strings.ToLower(filepath.Clean(filepath.Join(real, tail)))
		}
		next := filepath.Dir(parent)
		if next == parent {
			return strings.ToLower(path)
		}
		parent = next
	}
}

func availablePath(root, path string) bool {
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return false
	}
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." || !filepath.IsLocal(rel) {
		return false
	}
	dir := root
	for part := range strings.SplitSeq(rel, string(filepath.Separator)) {
		dir = filepath.Join(dir, part)
		info, err := os.Lstat(dir)
		if errors.Is(err, os.ErrNotExist) {
			return true
		}
		if err != nil || !info.IsDir() {
			return false
		}
	}
	return false // An existing untracked destination needs an explicit adoption.
}

func unusedKey[V any](m map[string]V, base string) string {
	key := base
	for i := 2; ; i++ {
		if _, ok := m[key]; !ok {
			return key
		}
		key = base + "-" + strconv.Itoa(i)
	}
}

func (p *ImportPlan) addMirror(m Mirror, keys map[AccountRef]string) error {
	cfg := p.result
	src, dst := keys[m.From], keys[m.To]
	name := m.From.label() + " → " + m.To.label()
	key := ""
	for _, k := range slices.Sorted(maps.Keys(cfg.Mirrors)) {
		v := cfg.Mirrors[k]
		if v.AccountSrc == src && v.AccountDst == dst {
			if key != "" {
				p.row("mirror", name, "collision")
				return nil
			}
			key = k
		}
	}
	if key == "" {
		key = unusedKey(cfg.Mirrors, src+"-"+dst)
		if err := cfg.AddMirror(key, config.Mirror{AccountSrc: src, AccountDst: dst}); err != nil {
			return ErrInvalid
		}
		if len(cfg.MirrorOrder) > 0 {
			cfg.MirrorOrder = append(cfg.MirrorOrder, key)
		}
		p.row("mirror", name, "add")
	} else {
		p.row("mirror", name, "keep")
	}
	for _, v := range m.Repos {
		entry := config.MirrorRepo{Direction: v.Direction, Origin: v.Origin, TargetRepo: v.Target}
		if existing, ok := cfg.Mirrors[key].Repos[v.Key]; ok {
			state := "keep"
			if existing.Direction != entry.Direction || existing.Origin != entry.Origin || existing.TargetRepo != entry.TargetRepo {
				state = "collision"
			}
			p.row("mirrorRepo", v.Key, state)
			continue
		}
		if err := cfg.AddMirrorRepo(key, v.Key, entry); err != nil {
			return ErrInvalid
		}
		group := cfg.Mirrors[key]
		if len(group.RepoOrder) > 0 {
			group.RepoOrder = append(group.RepoOrder, v.Key)
			cfg.Mirrors[key] = group
		}
		p.row("mirrorRepo", v.Key, "add")
	}
	return nil
}

// Apply accepts only a fresh collision-free plan built against the current
// domain configuration. The caller owns serialization and persistence.
func Apply(cfg *config.Config, p ImportPlan) error {
	if p.result == nil || p.Collisions != 0 || config.DomainHash(cfg) != p.base {
		return errors.New("review the import preview again")
	}
	cfg.Accounts = p.result.Accounts
	cfg.Sources, cfg.SourceOrder = p.result.Sources, p.result.SourceOrder
	cfg.Mirrors, cfg.MirrorOrder = p.result.Mirrors, p.result.MirrorOrder
	return nil
}
