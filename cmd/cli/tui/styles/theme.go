package styles

import "github.com/charmbracelet/lipgloss"

// Theme holds pre-built Lip Gloss styles for the TUI.
type Theme struct {
	Palette Palette

	// Layout styles.
	App       lipgloss.Style
	Card      lipgloss.Style
	Title     lipgloss.Style
	Subtitle  lipgloss.Style
	StatusBar lipgloss.Style

	// Text styles.
	Text      lipgloss.Style
	TextMuted lipgloss.Style
	TextBold  lipgloss.Style
	Heading   lipgloss.Style
	Brand     lipgloss.Style

	// Table styles.
	SelectedRow lipgloss.Style
	NormalRow   lipgloss.Style

	// Help styles.
	HelpKey  lipgloss.Style
	HelpDesc lipgloss.Style

	// Tab styles.
	ActiveTab   lipgloss.Style
	InactiveTab lipgloss.Style

	// Border style.
	Border lipgloss.Style
}

// NewTheme creates a Theme from the dark or light palette.
func NewTheme(dark bool) Theme {
	p := Dark
	if !dark {
		p = Light
	}

	return Theme{
		Palette: p,

		App:  lipgloss.NewStyle().Background(lipgloss.Color(p.BgBase)),
		Card: lipgloss.NewStyle().Background(lipgloss.Color(p.BgCard)).Padding(1, 2),

		Title: lipgloss.NewStyle().
			Foreground(lipgloss.Color(p.Brand)).
			Bold(true),

		Subtitle: lipgloss.NewStyle().
			Foreground(lipgloss.Color(p.TextHeading)),

		StatusBar: lipgloss.NewStyle().
			Foreground(lipgloss.Color(p.TextMuted)).
			Background(lipgloss.Color(p.BgCard)),

		Text:      lipgloss.NewStyle().Foreground(lipgloss.Color(p.TextPrimary)),
		TextMuted: lipgloss.NewStyle().Foreground(lipgloss.Color(p.TextMuted)),
		TextBold:  lipgloss.NewStyle().Foreground(lipgloss.Color(p.TextPrimary)).Bold(true),
		Heading:   lipgloss.NewStyle().Foreground(lipgloss.Color(p.TextHeading)).Bold(true),
		Brand:     lipgloss.NewStyle().Foreground(lipgloss.Color(p.Brand)),

		SelectedRow: lipgloss.NewStyle().
			Background(lipgloss.Color(p.Brand)).
			Foreground(lipgloss.Color("#ffffff")).
			Bold(true),

		NormalRow: lipgloss.NewStyle().
			Foreground(lipgloss.Color(p.TextPrimary)),

		HelpKey: lipgloss.NewStyle().
			Foreground(lipgloss.Color(p.Brand)).
			Bold(true),

		HelpDesc: lipgloss.NewStyle().
			Foreground(lipgloss.Color(p.TextMuted)),

		ActiveTab: lipgloss.NewStyle().
			Foreground(lipgloss.Color(p.Brand)).
			Bold(true).
			Underline(true),

		InactiveTab: lipgloss.NewStyle().
			Foreground(lipgloss.Color(p.TextMuted)),

		Border: lipgloss.NewStyle().
			BorderForeground(lipgloss.Color(p.BorderDefault)),
	}
}
