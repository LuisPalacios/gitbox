package styles

// Palette holds the color values for a theme.
type Palette struct {
	BgBase, BgCard, BgHover, BgSelected, BgInput string
	BorderDefault, BorderFocus, BorderAccent      string
	TextPrimary, TextMuted, TextHeading           string
	Brand, BrandHover, BrandMuted                 string

	// Status colors.
	Clean, Behind, Dirty, Ahead       string
	Syncing                           string
	NotCloned, NoUpstream             string
	Diverged, Conflict, StatusError   string

	// Credential badge colors.
	CredOK, CredWarning, CredOffline, CredError, CredNone string

	// Accent colors.
	AccentLink, AccentSuccess, AccentWarning string
	AccentDanger, AccentDangerHover          string

	// Card section border.
	CardSectionBorder string
}

// Dark is the dark theme palette from 05_UI_Design.json.
var Dark = Palette{
	BgBase: "#1a1b26", BgCard: "#1e2030", BgHover: "#252740",
	BgSelected: "#2a2d45", BgInput: "#252740",
	BorderDefault: "#363a4f", BorderFocus: "#5b9bff", BorderAccent: "#6e738d",
	TextPrimary: "#eff1ff", TextMuted: "#b0b7d0", TextHeading: "#f5f6ff",
	Brand: "#5b9bff", BrandHover: "#79b0ff", BrandMuted: "#3d4a73",

	Clean: "#61fd5f", Behind: "#D91C9A", Dirty: "#F07623", Ahead: "#4B95E9",
	Syncing:   "#4B95E9",
	NotCloned: "#71717a", NoUpstream: "#71717a",
	Diverged: "#D81E5B", Conflict: "#D81E5B", StatusError: "#D81E5B",

	CredOK: "#61fd5f", CredWarning: "#F07623", CredOffline: "#D81E5B", CredError: "#D81E5B", CredNone: "#4B95E9",

	AccentLink: "#5b9bff", AccentSuccess: "#61fd5f", AccentWarning: "#F07623",
	AccentDanger: "#D81E5B", AccentDangerHover: "#e8305a",

	CardSectionBorder: "#6b5b2e",
}

// Light is the light theme palette from 05_UI_Design.json.
var Light = Palette{
	BgBase: "#f5f5f5", BgCard: "#ffffff", BgHover: "#f0f0f0",
	BgSelected: "#e8e8e8", BgInput: "#f0f0f0",
	BorderDefault: "#d4d4d8", BorderFocus: "#2563eb", BorderAccent: "#a1a1aa",
	TextPrimary: "#18181b", TextMuted: "#71717a", TextHeading: "#3f3f46",
	Brand: "#2563eb", BrandHover: "#1d4ed8", BrandMuted: "#bfdbfe",

	Clean: "#166534", Behind: "#a21caf", Dirty: "#c2410c", Ahead: "#2563eb",
	Syncing:   "#2563eb",
	NotCloned: "#52525b", NoUpstream: "#52525b",
	Diverged: "#be123c", Conflict: "#be123c", StatusError: "#be123c",

	CredOK: "#166534", CredWarning: "#92400e", CredOffline: "#be123c", CredError: "#be123c", CredNone: "#2563eb",

	AccentLink: "#2563eb", AccentSuccess: "#166534", AccentWarning: "#c2410c",
	AccentDanger: "#be123c", AccentDangerHover: "#9f1239",

	CardSectionBorder: "#93c5fd",
}
