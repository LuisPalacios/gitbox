package config

import (
	"encoding/json"
	"reflect"
	"strings"
)

// extraJSON is an immutable object containing unrecognized disk fields.
// Keeping it private excludes it from JSON DTOs. Its comparable value lets
// repo snapshots retain their existing equality behavior.
type extraJSON string

var (
	// Retired keys count as known, so the next save drops them instead of
	// keeping them as extensions: experimental_remote is gone since #336,
	// view_mode and compact_window with the compact view since #342.
	globalJSONKeys = append(jsonKeys[GlobalConfig](), "folder", "extra_folders", "cards_open", "experimental_remote", "view_mode", "compact_window")
	// Test mode reads this fixture metadata separately, including the token.
	accountJSONKeys = append(jsonKeys[Account](), "_test")
	sourceJSONKeys  = jsonKeys[Source]()
	repoJSONKeys    = jsonKeys[Repo]()
)

func jsonKeys[T any]() []string {
	var keys []string
	for field := range reflect.TypeFor[T]().Fields() {
		if field.IsExported() {
			key, _, _ := strings.Cut(field.Tag.Get("json"), ",")
			if key != "-" {
				if key == "" {
					key = field.Name
				}
				keys = append(keys, key)
			}
		}
	}
	return keys
}

// loadExtensions attaches fields only at the four supported object boundaries.
// The regular decode remains responsible for all known keys and validation.
func loadExtensions(data []byte, cfg *Config) error {
	var raw struct {
		Global   json.RawMessage            `json:"global"`
		Accounts map[string]json.RawMessage `json:"accounts"`
		Sources  map[string]json.RawMessage `json:"sources"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	var err error
	if cfg.Global.extra, err = captureExtensions(raw.Global, globalJSONKeys); err != nil {
		return err
	}
	for key, account := range cfg.Accounts {
		if account.extra, err = captureExtensions(raw.Accounts[key], accountJSONKeys); err != nil {
			return err
		}
		cfg.Accounts[key] = account
	}
	for key, source := range cfg.Sources {
		sourceRaw := raw.Sources[key]
		if source.extra, err = captureExtensions(sourceRaw, sourceJSONKeys); err != nil {
			return err
		}
		var nested struct {
			Repos map[string]json.RawMessage `json:"repos"`
		}
		if err := json.Unmarshal(sourceRaw, &nested); err != nil {
			return err
		}
		for repoKey, repo := range source.Repos {
			if repo.extra, err = captureExtensions(nested.Repos[repoKey], repoJSONKeys); err != nil {
				return err
			}
			source.Repos[repoKey] = repo
		}
		cfg.Sources[key] = source
	}
	return nil
}

func captureExtensions(data json.RawMessage, known []string) (extraJSON, error) {
	if len(data) == 0 {
		return "", nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return "", err
	}
	for name := range fields {
		for _, recognized := range known {
			// encoding/json also accepts case variants of known keys.
			if strings.EqualFold(name, recognized) {
				delete(fields, name)
				break
			}
		}
	}
	if len(fields) == 0 {
		return "", nil
	}
	encoded, err := json.Marshal(fields)
	return extraJSON(encoded), err
}

// diskObject extends the known JSON shape only through the persistence codec.
type diskObject[T any] struct {
	known T
	extra extraJSON
}

func (o diskObject[T]) MarshalJSON() ([]byte, error) {
	known, err := json.Marshal(o.known)
	if err != nil || o.extra == "" {
		return known, err
	}
	if string(known) == "{}" {
		return []byte(o.extra), nil
	}
	known[len(known)-1] = ','
	return append(known, o.extra[1:]...), nil
}

type diskSource struct {
	Source
	Repos map[string]diskObject[Repo] `json:"repos"`
}

func diskObjects[T, D any](input map[string]T, encode func(T) diskObject[D]) map[string]diskObject[D] {
	if input == nil {
		return nil
	}
	objects := make(map[string]diskObject[D], len(input))
	for key, entry := range input {
		objects[key] = encode(entry)
	}
	return objects
}

func marshalConfig(cfg *Config) ([]byte, error) {
	if cfg == nil {
		return json.Marshal(cfg)
	}
	return json.Marshal(struct {
		Schema   string                            `json:"$schema,omitempty"`
		Version  int                               `json:"version"`
		Global   diskObject[GlobalConfig]          `json:"global"`
		Accounts map[string]diskObject[Account]    `json:"accounts"`
		Sources  map[string]diskObject[diskSource] `json:"sources"`
		Mirrors  map[string]Mirror                 `json:"mirrors,omitempty"`
	}{
		Schema: cfg.Schema, Version: cfg.Version, Global: diskObject[GlobalConfig]{cfg.Global, cfg.Global.extra},
		Accounts: diskObjects(cfg.Accounts, func(account Account) diskObject[Account] {
			return diskObject[Account]{account, account.extra}
		}),
		Sources: diskObjects(cfg.Sources, func(source Source) diskObject[diskSource] {
			repos := diskObjects(source.Repos, func(repo Repo) diskObject[Repo] {
				return diskObject[Repo]{repo, repo.extra}
			})
			return diskObject[diskSource]{diskSource{source, repos}, source.extra}
		}),
		Mirrors: cfg.Mirrors,
	})
}
