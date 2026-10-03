// Package i18n resolves and normalizes the UI language. The GUI ships its own
// string catalogs in the frontend; this package only decides which one to use.
package i18n

import (
	"os"
	"strings"

	"github.com/LuisPalacios/gitbox/pkg/config"
)

const (
	English = "en"
	Spanish = "es"
)

const FallbackLanguage = English

var supported = map[string]bool{
	English: true,
	Spanish: true,
}

// Normalize converts user, config, environment, and OS locale forms to a
// supported base language code. Unsupported or empty input returns English.
func Normalize(lang string) string {
	lang = strings.TrimSpace(strings.ToLower(lang))
	if lang == "" {
		return FallbackLanguage
	}
	lang = strings.ReplaceAll(lang, "_", "-")
	if i := strings.IndexByte(lang, '.'); i >= 0 {
		lang = lang[:i]
	}
	if i := strings.IndexByte(lang, '-'); i >= 0 {
		lang = lang[:i]
	}
	if supported[lang] {
		return lang
	}
	return FallbackLanguage
}

// Supported reports whether lang is one of the explicitly supported language
// codes after normalization.
func Supported(lang string) bool {
	lang = strings.TrimSpace(strings.ToLower(lang))
	lang = strings.ReplaceAll(lang, "_", "-")
	if i := strings.IndexByte(lang, '.'); i >= 0 {
		lang = lang[:i]
	}
	if i := strings.IndexByte(lang, '-'); i >= 0 {
		lang = lang[:i]
	}
	return supported[lang]
}

// Resolve returns the active language using the configured precedence:
// explicit override, GITBOX_LANG, config global.language, OS locale, English.
func Resolve(override string, cfg *config.Config) string {
	if strings.TrimSpace(override) != "" {
		return Normalize(override)
	}
	if env := os.Getenv("GITBOX_LANG"); strings.TrimSpace(env) != "" {
		return Normalize(env)
	}
	if cfg != nil && strings.TrimSpace(cfg.Global.Language) != "" {
		return Normalize(cfg.Global.Language)
	}
	return Normalize(osLocale())
}

func osLocale() string {
	for _, name := range []string{"LC_ALL", "LC_MESSAGES", "LANG", "LANGUAGE"} {
		if v := os.Getenv(name); strings.TrimSpace(v) != "" {
			return v
		}
	}
	return FallbackLanguage
}
