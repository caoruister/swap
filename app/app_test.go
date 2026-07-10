package app

import (
	"os"
	"testing"
)

func TestValidateAmount(t *testing.T) {
	tests := []struct {
		name    string
		amount  float64
		min     float64
		max     float64
		ticker  string
		wantErr bool
	}{
		{name: "valid within range", amount: 0.5, min: 0.01, max: 100, ticker: "BTC", wantErr: false},
		{name: "at minimum", amount: 0.01, min: 0.01, max: 100, ticker: "BTC", wantErr: false},
		{name: "at maximum", amount: 100, min: 0.01, max: 100, ticker: "BTC", wantErr: false},
		{name: "below minimum", amount: 0.001, min: 0.01, max: 100, ticker: "BTC", wantErr: true},
		{name: "exceeds maximum", amount: 200, min: 0.01, max: 100, ticker: "BTC", wantErr: true},
		{name: "zero amount", amount: 0, min: 0.01, max: 100, ticker: "ETH", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateAmount(tt.amount, tt.min, tt.max, tt.ticker)
			if tt.wantErr && err == nil {
				t.Fatalf("validateAmount(%.8f, %.2f, %.2f, %s) expected error", tt.amount, tt.min, tt.max, tt.ticker)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("validateAmount(%.8f, %.2f, %.2f, %s) unexpected error: %v", tt.amount, tt.min, tt.max, tt.ticker, err)
			}
		})
	}
}

func TestParseBoolEnv(t *testing.T) {
	tests := []struct {
		name    string
		envVal  string
		wantVal bool
		wantOk  bool
	}{
		{name: "unset", envVal: "", wantVal: false, wantOk: false},
		{name: "1", envVal: "1", wantVal: true, wantOk: true},
		{name: "true", envVal: "true", wantVal: true, wantOk: true},
		{name: "yes", envVal: "yes", wantVal: true, wantOk: true},
		{name: "on", envVal: "on", wantVal: true, wantOk: true},
		{name: "0", envVal: "0", wantVal: false, wantOk: true},
		{name: "false", envVal: "false", wantVal: false, wantOk: true},
		{name: "no", envVal: "no", wantVal: false, wantOk: true},
		{name: "off", envVal: "off", wantVal: false, wantOk: true},
		{name: "empty string", envVal: "", wantVal: false, wantOk: false},
		{name: "garbage", envVal: "garbage", wantVal: false, wantOk: false},
		{name: "Y", envVal: "Y", wantVal: false, wantOk: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envVal != "" {
				os.Setenv("_TEST_PARSE_BOOL", tt.envVal)
				defer os.Unsetenv("_TEST_PARSE_BOOL")
			} else {
				os.Unsetenv("_TEST_PARSE_BOOL")
			}
			gotVal, gotOk := parseBoolEnv("_TEST_PARSE_BOOL")
			if gotVal != tt.wantVal || gotOk != tt.wantOk {
				t.Fatalf("parseBoolEnv(%q) = (%v, %v) want (%v, %v)", tt.envVal, gotVal, gotOk, tt.wantVal, tt.wantOk)
			}
		})
	}
}

func TestShouldSuppressProviderWarning(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{in: "1inch:upstream-html", want: true},
		{in: "openocean:upstream-html", want: true},
		{in: "external:foo:unavailable", want: true},
		{in: "1inch:unavailable", want: true},
		{in: "0x:timeout", want: false},
		{in: "odos:circuit-open", want: false},
		{in: "paraswap:auth", want: false},
		{in: "mock:unsupported", want: false},
		{in: "usd prices: using cached", want: false},
		{in: "", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got := shouldSuppressProviderWarning(tt.in)
			if got != tt.want {
				t.Fatalf("shouldSuppressProviderWarning(%q) = %v want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestSummarizeProviderWarning_AllKinds(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		// Messages with "provider unavailable:" prefix extract provider name
		{in: "0x unavailable: request timeout", want: "0x:timeout"},
		{in: "1inch unavailable: context deadline exceeded", want: "1inch:timeout"},
		// Messages without "provider unavailable:" prefix fall back to "provider"
		{in: "0x requires swap_0x_api_key", want: "provider:auth"},
		{in: "paraswap requires api key", want: "provider:auth"},
		// Messages with colon after provider extract provider name
		{in: "odos: unsupported token", want: "odos:unsupported"},
		{in: "openocean: error 502", want: "openocean:upstream-5xx"},
		{in: "0x: error 401", want: "0x:upstream-4xx"},
		// No provider prefix at all
		{in: "connection refused", want: "provider:unavailable"},
		// No provider prefix with circuit open message
		{in: "external circuit open for key (10s remaining)", want: "provider:circuit-open"},
	}
	for _, tt := range tests {
		t.Run(tt.in[:min(len(tt.in), 30)], func(t *testing.T) {
			got := summarizeProviderWarning(tt.in)
			if got != tt.want {
				t.Fatalf("summarizeProviderWarning(%q) = %q want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestSummarizeProviderWarning_USDPrefixPassThrough(t *testing.T) {
	got := summarizeProviderWarning("usd prices: using cached market data (7m old)")
	if got != "usd prices: using cached market data (7m old)" {
		t.Fatalf("USD warning should pass through unchanged: %q", got)
	}
}

func TestSummarizeProviderWarning_Empty(t *testing.T) {
	if got := summarizeProviderWarning(""); got != "" {
		t.Fatalf("empty input should return empty: %q", got)
	}
	if got := summarizeProviderWarning("  "); got != "" {
		t.Fatalf("whitespace input should return empty: %q", got)
	}
}

func TestDeriveUSDPriceBadge_Nil(t *testing.T) {
	if got := deriveUSDPriceBadge(nil); got != "USD: fresh" {
		t.Fatalf("deriveUSDPriceBadge(nil)=%q want=%q", got, "USD: fresh")
	}
}
