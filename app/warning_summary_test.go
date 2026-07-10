package app

import "testing"

func TestSummarizeProviderWarning_NonJSONHTML(t *testing.T) {
	got := summarizeProviderWarning("1inch unavailable: 1inch non-json response status=200 content_type=\"text/html\" (possible endpoint issue or upstream block): <html>...")
	if got != "1inch:upstream-html" {
		t.Fatalf("summarizeProviderWarning()=%q want=%q", got, "1inch:upstream-html")
	}
}

func TestSummarizeProviderWarning_ParserAngleBracket(t *testing.T) {
	got := summarizeProviderWarning("1inch unavailable: invalid character '<' looking for beginning of value")
	if got != "1inch:upstream-html" {
		t.Fatalf("summarizeProviderWarning()=%q want=%q", got, "1inch:upstream-html")
	}
}

func TestSummarizeProviderWarning_CircuitOpen(t *testing.T) {
	got := summarizeProviderWarning("external:foo unavailable: external provider circuit open for external:foo (28s remaining)")
	if got != "external:foo:circuit-open" {
		t.Fatalf("summarizeProviderWarning()=%q want=%q", got, "external:foo:circuit-open")
	}
}

func TestNormalizeProviderWarnings_SuppressesNoisyKinds(t *testing.T) {
	warnings := normalizeProviderWarnings([]string{
		"openocean unavailable: openocean non-json response: <html>",
		"odos unavailable: request failed",
		"1inch unavailable: invalid character '<' looking for beginning of value",
		"external:foo unavailable: external provider circuit open for external:foo (28s remaining)",
		"usd prices: using cached market data (7m old)",
	})

	want := []string{"external:foo:circuit-open", "usd prices: using cached market data (7m old)"}
	if len(warnings) != len(want) {
		t.Fatalf("normalizeProviderWarnings()=%v want=%v", warnings, want)
	}
	for i := range want {
		if warnings[i] != want[i] {
			t.Fatalf("normalizeProviderWarnings()[%d]=%q want=%q", i, warnings[i], want[i])
		}
	}
}
