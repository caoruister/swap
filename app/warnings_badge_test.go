package app

import "testing"

func TestDeriveUSDPriceBadge(t *testing.T) {
	tests := []struct {
		name     string
		warnings []string
		want     string
	}{
		{name: "fresh when none", warnings: nil, want: "USD: fresh"},
		{name: "cached with age", warnings: []string{"usd prices: using cached market data (7m old)"}, want: "USD: cached 7m old"},
		{name: "cached without age", warnings: []string{"usd prices: using cached market data"}, want: "USD: cached"},
		{name: "fresh with non-usd warnings", warnings: []string{"0x:timeout"}, want: "USD: fresh"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := deriveUSDPriceBadge(tt.warnings)
			if got != tt.want {
				t.Fatalf("deriveUSDPriceBadge(%v)=%q want=%q", tt.warnings, got, tt.want)
			}
		})
	}
}
