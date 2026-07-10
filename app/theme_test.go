package app

import "testing"

func TestParseThemeName_Default(t *testing.T) {
	tests := []struct {
		in   string
		want ThemeName
	}{
		{in: "", want: ThemeDefault},
		{in: "unknown", want: ThemeDefault},
		{in: "default", want: ThemeDefault},
		{in: "DEFAULT", want: ThemeDefault},
	}
	for _, tt := range tests {
		got := ParseThemeName(tt.in)
		if got != tt.want {
			t.Fatalf("ParseThemeName(%q)=%q want=%q", tt.in, got, tt.want)
		}
	}
}

func TestParseThemeName_Hacker(t *testing.T) {
	tests := []struct {
		in   string
		want ThemeName
	}{
		{in: "hacker", want: ThemeHacker},
		{in: "HACKER", want: ThemeHacker},
		{in: "matrix", want: ThemeHacker},
		{in: "green", want: ThemeHacker},
	}
	for _, tt := range tests {
		got := ParseThemeName(tt.in)
		if got != tt.want {
			t.Fatalf("ParseThemeName(%q)=%q want=%q", tt.in, got, tt.want)
		}
	}
}

func TestParseThemeName_Ocean(t *testing.T) {
	tests := []struct {
		in   string
		want ThemeName
	}{
		{in: "ocean", want: ThemeOcean},
		{in: "blue", want: ThemeOcean},
		{in: "cyan", want: ThemeOcean},
	}
	for _, tt := range tests {
		got := ParseThemeName(tt.in)
		if got != tt.want {
			t.Fatalf("ParseThemeName(%q)=%q want=%q", tt.in, got, tt.want)
		}
	}
}

func TestParseThemeName_Sunset(t *testing.T) {
	tests := []struct {
		in   string
		want ThemeName
	}{
		{in: "sunset", want: ThemeSunset},
		{in: "orange", want: ThemeSunset},
		{in: "magenta", want: ThemeSunset},
	}
	for _, tt := range tests {
		got := ParseThemeName(tt.in)
		if got != tt.want {
			t.Fatalf("ParseThemeName(%q)=%q want=%q", tt.in, got, tt.want)
		}
	}
}

func TestResolveTheme_Known(t *testing.T) {
	if got := resolveTheme(ThemeDefault); got.Accent != ThemeDefaultPalette.Accent {
		t.Fatalf("resolveTheme(default).Accent=%v want=%v", got.Accent, ThemeDefaultPalette.Accent)
	}
	if got := resolveTheme(ThemeHacker); got.Accent != ThemeHackerPalette.Accent {
		t.Fatalf("resolveTheme(hacker).Accent=%v want=%v", got.Accent, ThemeHackerPalette.Accent)
	}
}

func TestResolveTheme_Unknown(t *testing.T) {
	got := resolveTheme("nonexistent")
	if got.Accent != ThemeDefaultPalette.Accent {
		t.Fatalf("resolveTheme(unknown) should return default, got=%v", got.Accent)
	}
}

func TestAllThemeNames(t *testing.T) {
	names := AllThemeNames()
	if len(names) != 4 {
		t.Fatalf("AllThemeNames() should have 4 themes, got %d", len(names))
	}
	seen := map[ThemeName]bool{}
	for _, n := range names {
		seen[n] = true
	}
	if !seen[ThemeDefault] || !seen[ThemeHacker] || !seen[ThemeOcean] || !seen[ThemeSunset] {
		t.Fatalf("AllThemeNames() missing one or more built-in themes: %v", names)
	}
}
