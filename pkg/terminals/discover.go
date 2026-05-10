package terminals

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/harness"
	"github.com/LuisPalacios/gitbox/pkg/launch"
)

// ─── Windows Terminal settings.json discovery ─────────────────────────────

// wtSettingsCandidates returns the locations gitbox checks for Windows
// Terminal's settings.json, in priority order: Store install, Preview install,
// then unpackaged install. Empty paths (e.g. when LOCALAPPDATA is unset on
// non-Windows hosts) are filtered out.
func wtSettingsCandidates() []string {
	local := os.Getenv("LOCALAPPDATA")
	if local == "" {
		return nil
	}
	return []string{
		filepath.Join(local, "Packages", "Microsoft.WindowsTerminal_8wekyb3d8bbwe", "LocalState", "settings.json"),
		filepath.Join(local, "Packages", "Microsoft.WindowsTerminalPreview_8wekyb3d8bbwe", "LocalState", "settings.json"),
		filepath.Join(local, "Microsoft", "Windows Terminal", "settings.json"),
	}
}

// stripJSONComments removes JSONC-style // line and /* block */ comments
// while preserving string literals. Trailing commas are left to the JSON
// decoder — Microsoft's settings.json doesn't emit them.
func stripJSONComments(in []byte) []byte {
	out := make([]byte, 0, len(in))
	const (
		stCode = iota
		stString
		stStringEscape
		stLineComment
		stBlockComment
		stBlockCommentStar
	)
	state := stCode
	for i := 0; i < len(in); i++ {
		c := in[i]
		switch state {
		case stCode:
			if c == '/' && i+1 < len(in) && in[i+1] == '/' {
				state = stLineComment
				i++
				continue
			}
			if c == '/' && i+1 < len(in) && in[i+1] == '*' {
				state = stBlockComment
				i++
				continue
			}
			out = append(out, c)
			if c == '"' {
				state = stString
			}
		case stString:
			out = append(out, c)
			if c == '\\' {
				state = stStringEscape
			} else if c == '"' {
				state = stCode
			}
		case stStringEscape:
			out = append(out, c)
			state = stString
		case stLineComment:
			if c == '\n' {
				out = append(out, c)
				state = stCode
			}
		case stBlockComment:
			if c == '*' {
				state = stBlockCommentStar
			}
		case stBlockCommentStar:
			if c == '/' {
				state = stCode
			} else if c != '*' {
				state = stBlockComment
			}
		}
	}
	return out
}

// parseWTProfiles parses a JSONC-encoded settings.json blob and returns one
// TerminalProfile per visible WT profile. A profile is excluded when
// `hidden: true` is set, or when its `source` appears in the top-level
// `disabledProfileSources` array — both criteria match WT's own menu rules.
func parseWTProfiles(data []byte) ([]config.TerminalProfile, error) {
	clean := stripJSONComments(data)
	var doc struct {
		DisabledProfileSources []string `json:"disabledProfileSources"`
		Profiles               struct {
			List []struct {
				Name   string `json:"name"`
				Hidden *bool  `json:"hidden,omitempty"`
				Source string `json:"source,omitempty"`
			} `json:"list"`
		} `json:"profiles"`
	}
	if err := json.Unmarshal(clean, &doc); err != nil {
		return nil, err
	}
	if len(doc.Profiles.List) == 0 {
		return nil, errors.New("no profiles in settings.json")
	}
	disabled := make(map[string]bool, len(doc.DisabledProfileSources))
	for _, s := range doc.DisabledProfileSources {
		disabled[s] = true
	}
	var out []config.TerminalProfile
	for _, p := range doc.Profiles.List {
		if p.Name == "" {
			continue
		}
		if p.Hidden != nil && *p.Hidden {
			continue
		}
		if p.Source != "" && disabled[p.Source] {
			continue
		}
		out = append(out, config.TerminalProfile{
			ID:         "wt+" + slugifyASCII(p.Name),
			Name:       p.Name,
			TerminalID: "wt",
			Args:       []string{"--profile", p.Name, "-d", launch.TokenPath, launch.TokenCommand},
			Source:     "wt-profile",
		})
	}
	if len(out) == 0 {
		return nil, errors.New("no visible WT profiles")
	}
	return out, nil
}

// DiscoverWTProfiles locates Windows Terminal's settings.json, parses the
// profile list, and returns one TerminalProfile per visible WT profile.
// Returns an empty slice (no error) when wt.exe is missing or no settings.json
// is present — that is a normal config, not a failure.
func DiscoverWTProfiles() []config.TerminalProfile {
	if _, ok := probeWT(); !ok {
		return nil
	}
	for _, path := range wtSettingsCandidates() {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		profiles, err := parseWTProfiles(data)
		if err != nil {
			return nil
		}
		return profiles
	}
	return nil
}

// ─── WezTerm launch_menu discovery ────────────────────────────────────────

// weztermLuaCandidates returns the canonical wezterm.lua locations for the
// current host, in WezTerm's own lookup order. Mirrors WezTerm itself:
// $WEZTERM_CONFIG_FILE → $XDG_CONFIG_HOME/wezterm/wezterm.lua →
// $HOME/.config/wezterm/wezterm.lua → $HOME/.wezterm.lua.
func weztermLuaCandidates() []string {
	var out []string
	if explicit := os.Getenv("WEZTERM_CONFIG_FILE"); explicit != "" {
		out = append(out, explicit)
	}
	home, _ := os.UserHomeDir()
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		out = append(out, filepath.Join(xdg, "wezterm", "wezterm.lua"))
	}
	if home != "" {
		out = append(out,
			filepath.Join(home, ".config", "wezterm", "wezterm.lua"),
			filepath.Join(home, ".wezterm.lua"),
		)
	}
	return out
}

// DiscoverWeztermProfiles parses the user's wezterm.lua (if any) and returns
// one TerminalProfile per `launch_menu` entry. The Args slice carries the
// entry's argv via the `start --cwd {path} -- <argv...>` shape.
func DiscoverWeztermProfiles() []config.TerminalProfile {
	for _, path := range weztermLuaCandidates() {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		entries, err := harness.ParseWeztermLaunchMenu(data)
		if err != nil {
			return nil
		}
		out := make([]config.TerminalProfile, 0, len(entries))
		for _, e := range entries {
			label := e.Label
			if label == "" && len(e.Args) > 0 {
				label = e.Args[0]
			}
			if label == "" {
				continue
			}
			args := []string{"start", "--cwd", launch.TokenPath, "--"}
			args = append(args, e.Args...)
			out = append(out, config.TerminalProfile{
				ID:         "wezterm+" + slugifyASCII(label),
				Name:       "WezTerm — " + label,
				TerminalID: "wezterm",
				Args:       args,
				Source:     "wezterm-launchmenu",
			})
		}
		return out
	}
	return nil
}
