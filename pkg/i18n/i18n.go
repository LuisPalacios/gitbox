// Package i18n resolves and normalizes the UI language. The GUI ships its own
// string catalogs in the frontend; this package only decides which one to use.
package i18n

import "strings"

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
