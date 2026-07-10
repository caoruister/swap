package app

import (
	"sort"
	"testing"

	pb "github.com/lfaoro/swap/gen/go/swap/v1"
)

func TestFormatQuoteAmount(t *testing.T) {
	tests := []struct {
		name   string
		raw    string
		ticker string
		want   string
	}{
		{name: "eth wei to eth", raw: "62854157130", ticker: "ETH", want: "0.0000000628"},
		{name: "usdt units to decimal", raw: "62938434027", ticker: "USDT", want: "62938.434027"},
		{name: "wbtc sats to btc", raw: "100000000", ticker: "WBTC", want: "1"},
		{name: "already decimal pass through", raw: "12.345", ticker: "USDT", want: "12.345"},
		{name: "unknown ticker unchanged", raw: "12345", ticker: "XMR", want: "12345"},
		{name: "long integer compacted", raw: "12345678901234567890", ticker: "XMR", want: "12345678901+"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatQuoteAmount(tt.raw, tt.ticker)
			if got != tt.want {
				t.Fatalf("formatQuoteAmount(%q, %q)=%q want=%q", tt.raw, tt.ticker, got, tt.want)
			}
		})
	}
}

func TestCompactAmountDisplay(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "0.00000006285415713", want: "0.0000000628"},
		{in: "62938.434027", want: "62938.434027"},
		{in: "123456789012345", want: "12345678901+"},
		{in: "12345.678901234", want: "12345.678901"},
	}

	for _, tt := range tests {
		got := compactAmountDisplay(tt.in)
		if got != tt.want {
			t.Fatalf("compactAmountDisplay(%q)=%q want=%q", tt.in, got, tt.want)
		}
	}
}

func TestComputeSpreadPercentsByBestSwapMode(t *testing.T) {
	quotes := []*pb.QuoteDetails{
		{AmountTo: "1000"},
		{AmountTo: "990"},
		{AmountTo: "900"},
	}
	got := computeSpreadPercentsByBest(quotes, false)
	want := []string{"0.00%", "1.00%", "10.00%"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("swap spread[%d]=%s want=%s", i, got[i], want[i])
		}
	}
}

func TestComputeSpreadPercentsByBestPaymentMode(t *testing.T) {
	quotes := []*pb.QuoteDetails{
		{AmountFrom: "100"},
		{AmountFrom: "102"},
		{AmountFrom: "110"},
	}
	got := computeSpreadPercentsByBest(quotes, true)
	want := []string{"0.00%", "2.00%", "10.00%"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("payment spread[%d]=%s want=%s", i, got[i], want[i])
		}
	}
}

func TestQuoteSortSwapModeByAmountToUSD(t *testing.T) {
	quotes := []*pb.QuoteDetails{
		{Provider: "a", AmountTo_USD: "90"},
		{Provider: "b", AmountTo_USD: "110"},
		{Provider: "c", AmountTo_USD: "100"},
	}

	sort.Slice(quotes, func(i, j int) bool {
		return parseUSDValue(quotes[i].GetAmountTo_USD()) > parseUSDValue(quotes[j].GetAmountTo_USD())
	})

	if quotes[0].Provider != "b" || quotes[1].Provider != "c" || quotes[2].Provider != "a" {
		t.Fatalf("unexpected swap sort order: %s,%s,%s", quotes[0].Provider, quotes[1].Provider, quotes[2].Provider)
	}
}

func TestQuoteSortPaymentModeByAmountFromUSD(t *testing.T) {
	quotes := []*pb.QuoteDetails{
		{Provider: "a", AmountFrom_USD: "110"},
		{Provider: "b", AmountFrom_USD: "90"},
		{Provider: "c", AmountFrom_USD: "100"},
	}

	sort.Slice(quotes, func(i, j int) bool {
		return parseUSDValue(quotes[i].GetAmountFrom_USD()) < parseUSDValue(quotes[j].GetAmountFrom_USD())
	})

	if quotes[0].Provider != "b" || quotes[1].Provider != "c" || quotes[2].Provider != "a" {
		t.Fatalf("unexpected payment sort order: %s,%s,%s", quotes[0].Provider, quotes[1].Provider, quotes[2].Provider)
	}
}
