// Package i18n provides small, stable message-key translation helpers.
package i18n

import (
	"fmt"
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

// Translator resolves stable message keys into human-facing text.
type Translator struct {
	lang string
}

// New returns a translator for lang, falling back to English when unsupported.
func New(lang string) Translator {
	return Translator{lang: Normalize(lang)}
}

// Language returns the resolved language code.
func (t Translator) Language() string {
	return t.lang
}

// T returns a translated string. Missing keys fall back to English, then key.
func (t Translator) T(key string) string {
	if s, ok := catalogs[t.lang][key]; ok {
		return s
	}
	if s, ok := catalogs[FallbackLanguage][key]; ok {
		return s
	}
	return key
}

// F returns a formatted translated string.
func (t Translator) F(key string, args ...any) string {
	return fmt.Sprintf(t.T(key), args...)
}

// Count chooses singular or plural-ish keys based on count.
func (t Translator) Count(count int, singularKey, pluralKey string) string {
	if count == 1 {
		return t.F(singularKey, count)
	}
	return t.F(pluralKey, count)
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

var catalogs = map[string]map[string]string{
	English: {
		"app.description":              "Unified tool for managing Git repositories across multiple accounts and providers",
		"app.long":                     "gitbox %s by Luis Palacios Derqui\nUnified tool for managing Git repositories across multiple accounts and providers.\nhttps://github.com/LuisPalacios/gitbox",
		"flag.config":                  "path to config file (default: ~/.config/gitbox/gitbox.json)",
		"flag.json":                    "output in JSON format",
		"flag.lang":                    "language for human-facing output (en|es)",
		"flag.verbose":                 "verbose output",
		"flag.test_mode":               "run with isolated test config from test-gitbox.json",
		"help.usage":                   "Usage:",
		"help.start_tui":               "Start the interactive TUI",
		"help.cli_mode":                "Run in CLI mode",
		"help.aliases":                 "Aliases:",
		"help.available_commands":      "Available Commands:",
		"help.main_commands":           "Main Commands:",
		"help.additional_commands":     "Additional Commands:",
		"help.shell_completion":        "Shell completion:",
		"help.completion_desc":         "Generate autocompletion for your shell (see docs/completion.md)",
		"help.flags":                   "Flags:",
		"help.global_flags":            "Global Flags:",
		"help.more":                    "Use \"%s [command] --help\" for more information about a command.",
		"cmd.global.short":             "Manage global settings and configuration",
		"cmd.global.show.short":        "Show global settings",
		"cmd.global.update.short":      "Update global settings",
		"cmd.global.config.short":      "Show or locate the configuration file",
		"cmd.global.config.show.short": "Display the full configuration",
		"cmd.global.config.path.short": "Print the configuration file path and status",
		"cmd.init.short":               "Create a new gitbox configuration",
		"cmd.account.short":            "Manage accounts",
		"cmd.source.short":             "Manage sources",
		"cmd.repo.short":               "Manage repos within sources",
		"cmd.clone.short":              "Clone repositories",
		"cmd.status.short":             "Show sync status of all repositories",
		"cmd.pull.short":               "Pull repositories that are behind upstream",
		"cmd.fetch.short":              "Fetch all remotes for repositories (without merging)",
		"cmd.sweep.short":              "Remove stale local branches (merged or gone upstream)",
		"cmd.browse.short":             "Open a repository in the default browser",
		"cmd.mirror.short":             "Manage repository mirrors between providers",
		"cmd.workspace.short":          "Manage multi-repo workspaces (VS Code, tmuxinator)",
		"cmd.reconfigure.short":        "Reconfigure credential isolation for all cloned repos",
		"cmd.identity.short":           "Manage per-repo git identity (user.name, user.email)",
		"cmd.gitignore.short":          "Manage the recommended global gitignore (~/.gitignore_global)",
		"cmd.scan.short":               "Scan filesystem for git repos and show their status",
		"cmd.adopt.short":              "Adopt orphan repos into gitbox",
		"cmd.update.short":             "Check for updates and optionally install them",
		"cmd.version.short":            "Print the version of gitbox",
		"cmd.doctor.short":             "Check that every external tool gitbox relies on is installed",
		"flag.global.folder":           "root folder for all git clones",
		"flag.global.language":         "default language for human-facing output (en|es)",
		"flag.global.gcm_helper":       "GCM credential helper (typically 'manager')",
		"flag.global.gcm_store":        "GCM credential store (wincredman|keychain|secretservice)",
		"flag.global.ssh_folder":       "SSH config directory (default: ~/.ssh)",
		"msg.config_saved":             "Config saved to %s\n",
		"msg.file_missing_init":        "(file does not exist - run 'gitbox init' to create it)",
		"tui.settings.title":           "Settings",
		"tui.settings.root_folder":     "  Root folder: ",
		"tui.settings.language":        "  Language: ",
		"tui.settings.periodic_sync":   "  Periodic sync: ",
		"tui.settings.gitignore":       "Check recommended global gitignore",
		"tui.settings.saved":           "Settings saved.",
		"tui.settings.error":           "Error: %s",
		"tui.hint.navigate":            "↑↓ navigate",
		"tui.hint.change":              "←→ change",
		"tui.hint.save":                "enter save",
		"tui.hint.back":                "ESC back",
	},
	Spanish: {
		"app.description":              "Herramienta unificada para gestionar repositorios Git en varias cuentas y proveedores",
		"app.long":                     "gitbox %s por Luis Palacios Derqui\nHerramienta unificada para gestionar repositorios Git en varias cuentas y proveedores.\nhttps://github.com/LuisPalacios/gitbox",
		"flag.config":                  "ruta del archivo de configuracion (predeterminado: ~/.config/gitbox/gitbox.json)",
		"flag.json":                    "muestra la salida en formato JSON",
		"flag.lang":                    "idioma de la salida para personas (en|es)",
		"flag.verbose":                 "salida detallada",
		"flag.test_mode":               "ejecuta con configuracion de prueba aislada desde test-gitbox.json",
		"help.usage":                   "Uso:",
		"help.start_tui":               "Inicia la TUI interactiva",
		"help.cli_mode":                "Ejecuta en modo CLI",
		"help.aliases":                 "Alias:",
		"help.available_commands":      "Comandos disponibles:",
		"help.main_commands":           "Comandos principales:",
		"help.additional_commands":     "Comandos adicionales:",
		"help.shell_completion":        "Completado de shell:",
		"help.completion_desc":         "Genera autocompletado para tu shell (consulta docs/completion.md)",
		"help.flags":                   "Opciones:",
		"help.global_flags":            "Opciones globales:",
		"help.more":                    "Usa \"%s [command] --help\" para mas informacion sobre un comando.",
		"cmd.global.short":             "Gestiona ajustes globales y configuracion",
		"cmd.global.show.short":        "Muestra los ajustes globales",
		"cmd.global.update.short":      "Actualiza los ajustes globales",
		"cmd.global.config.short":      "Muestra o localiza el archivo de configuracion",
		"cmd.global.config.show.short": "Muestra la configuracion completa",
		"cmd.global.config.path.short": "Imprime la ruta y el estado del archivo de configuracion",
		"cmd.init.short":               "Crea una nueva configuracion de gitbox",
		"cmd.account.short":            "Gestiona cuentas",
		"cmd.source.short":             "Gestiona fuentes",
		"cmd.repo.short":               "Gestiona repos dentro de fuentes",
		"cmd.clone.short":              "Clona repositorios",
		"cmd.status.short":             "Muestra el estado de sincronizacion de todos los repositorios",
		"cmd.pull.short":               "Actualiza repositorios atrasados respecto al upstream",
		"cmd.fetch.short":              "Ejecuta fetch en todos los remotos sin mezclar cambios",
		"cmd.sweep.short":              "Elimina ramas locales obsoletas",
		"cmd.browse.short":             "Abre un repositorio en el navegador predeterminado",
		"cmd.mirror.short":             "Gestiona mirrors de repositorios entre proveedores",
		"cmd.workspace.short":          "Gestiona workspaces multi-repo (VS Code, tmuxinator)",
		"cmd.reconfigure.short":        "Reconfigura el aislamiento de credenciales en clones",
		"cmd.identity.short":           "Gestiona identidad git por repo (user.name, user.email)",
		"cmd.gitignore.short":          "Gestiona el gitignore global recomendado (~/.gitignore_global)",
		"cmd.scan.short":               "Escanea el sistema de archivos y muestra estado de repos",
		"cmd.adopt.short":              "Adopta repos huerfanos en gitbox",
		"cmd.update.short":             "Busca actualizaciones y opcionalmente las instala",
		"cmd.version.short":            "Imprime la version de gitbox",
		"cmd.doctor.short":             "Comprueba que esten instaladas las herramientas externas necesarias",
		"flag.global.folder":           "carpeta raiz para todos los clones git",
		"flag.global.language":         "idioma predeterminado para salida para personas (en|es)",
		"flag.global.gcm_helper":       "helper de credenciales GCM (normalmente 'manager')",
		"flag.global.gcm_store":        "almacen de credenciales GCM (wincredman|keychain|secretservice)",
		"flag.global.ssh_folder":       "directorio de configuracion SSH (predeterminado: ~/.ssh)",
		"msg.config_saved":             "Configuracion guardada en %s\n",
		"msg.file_missing_init":        "(el archivo no existe - ejecuta 'gitbox init' para crearlo)",
		"tui.settings.title":           "Ajustes",
		"tui.settings.root_folder":     "  Carpeta raiz: ",
		"tui.settings.language":        "  Idioma: ",
		"tui.settings.periodic_sync":   "  Sincronizacion periodica: ",
		"tui.settings.gitignore":       "Comprobar gitignore global recomendado",
		"tui.settings.saved":           "Ajustes guardados.",
		"tui.settings.error":           "Error: %s",
		"tui.hint.navigate":            "↑↓ navegar",
		"tui.hint.change":              "←→ cambiar",
		"tui.hint.save":                "enter guardar",
		"tui.hint.back":                "ESC volver",
	},
}
