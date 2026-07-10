package app

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// ThemeName represents a built-in UI color theme.
type ThemeName string

const (
	ThemeDefault ThemeName = "default" // green-accent terminal theme
	ThemeHacker  ThemeName = "hacker"  // matrix green-on-black
	ThemeOcean   ThemeName = "ocean"   // blue-cyan palette
	ThemeSunset  ThemeName = "sunset"  // warm orange-magenta
)

// Theme defines all UI colors used across the TUI.
// All colors are lipgloss ANSI color numbers (0-255).
type Theme struct {
	// Accent is used for highlighted/active elements (swap/pay badge, focused border).
	Accent lipgloss.Color
	// Base is used for secondary/default text and borders.
	Base lipgloss.Color
	// Warning is used for warning messages and stale data indicators.
	Warning lipgloss.Color
	// Error is used for error messages.
	Error lipgloss.Color
	// Info is used for informational notes (e.g. selected rate detail).
	Info lipgloss.Color
	// Dim is used for borders, dividers, and less prominent elements.
	Dim lipgloss.Color
	// SelectedBg is the background color for the selected table row.
	SelectedBg lipgloss.Color
	// SelectedFg is the foreground (text) color for the selected table row.
	SelectedFg lipgloss.Color
	// WarningToggleActive is the color for the "Warnings: ON" label.
	WarningToggleActive lipgloss.Color
	// WarningToggleInactive is the color for the "Warnings: OFF" label.
	WarningToggleInactive lipgloss.Color
}

// Built-in themes.
var (
	ThemeDefaultPalette = Theme{
		Accent:                lipgloss.Color("10"),  // green
		Base:                  lipgloss.Color("8"),   // gray
		Warning:               lipgloss.Color("3"),   // yellow
		Error:                 lipgloss.Color("1"),   // red
		Info:                  lipgloss.Color("6"),   // cyan
		Dim:                   lipgloss.Color("240"), // dark gray
		SelectedBg:            lipgloss.Color("10"),  // green
		SelectedFg:            lipgloss.Color("0"),   // black
		WarningToggleActive:   lipgloss.Color("10"),  // green
		WarningToggleInactive: lipgloss.Color("8"),   // gray
	}

	ThemeHackerPalette = Theme{
		Accent:                lipgloss.Color("10"), // green
		Base:                  lipgloss.Color("22"), // dark green
		Warning:               lipgloss.Color("11"), // bright yellow
		Error:                 lipgloss.Color("9"),  // bright red
		Info:                  lipgloss.Color("14"), // bright cyan
		Dim:                   lipgloss.Color("28"), // medium green
		SelectedBg:            lipgloss.Color("22"), // dark green
		SelectedFg:            lipgloss.Color("10"), // green
		WarningToggleActive:   lipgloss.Color("46"), // bright green
		WarningToggleInactive: lipgloss.Color("22"), // dark green
	}

	ThemeOceanPalette = Theme{
		Accent:                lipgloss.Color("75"), // light blue
		Base:                  lipgloss.Color("12"), // blue
		Warning:               lipgloss.Color("11"), // bright yellow
		Error:                 lipgloss.Color("9"),  // bright red
		Info:                  lipgloss.Color("51"), // cyan
		Dim:                   lipgloss.Color("68"), // medium blue-gray
		SelectedBg:            lipgloss.Color("75"), // light blue
		SelectedFg:            lipgloss.Color("0"),  // black
		WarningToggleActive:   lipgloss.Color("75"), // light blue
		WarningToggleInactive: lipgloss.Color("12"), // blue
	}

	ThemeSunsetPalette = Theme{
		Accent:                lipgloss.Color("214"), // orange
		Base:                  lipgloss.Color("244"), // warm gray
		Warning:               lipgloss.Color("11"),  // bright yellow
		Error:                 lipgloss.Color("9"),   // bright red
		Info:                  lipgloss.Color("213"), // magenta
		Dim:                   lipgloss.Color("95"),  // warm dark gray
		SelectedBg:            lipgloss.Color("214"), // orange
		SelectedFg:            lipgloss.Color("0"),   // black
		WarningToggleActive:   lipgloss.Color("214"), // orange
		WarningToggleInactive: lipgloss.Color("244"), // warm gray
	}

	// themeMap maps ThemeName to Theme for lookup by name.
	themeMap = map[ThemeName]Theme{
		ThemeDefault: ThemeDefaultPalette,
		ThemeHacker:  ThemeHackerPalette,
		ThemeOcean:   ThemeOceanPalette,
		ThemeSunset:  ThemeSunsetPalette,
	}
)

// resolveTheme returns the Theme for the given name, or the default if unknown.
func resolveTheme(name ThemeName) Theme {
	if t, ok := themeMap[name]; ok {
		return t
	}
	return ThemeDefaultPalette
}

// AllThemeNames returns the list of built-in theme names.
func AllThemeNames() []ThemeName {
	return []ThemeName{ThemeDefault, ThemeHacker, ThemeOcean, ThemeSunset}
}

// ParseThemeName parses a string into a ThemeName, case-insensitively.
// Returns ThemeDefault for unknown values.
func ParseThemeName(s string) ThemeName {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "hacker", "matrix", "green":
		return ThemeHacker
	case "ocean", "blue", "cyan":
		return ThemeOcean
	case "sunset", "orange", "magenta":
		return ThemeSunset
	default:
		return ThemeDefault
	}
}
