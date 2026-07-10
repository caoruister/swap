package app

import (
	"strings"
	"testing"
)

func TestClipboardStatusText_Basic(t *testing.T) {
	got := clipboardStatusText(
		"Bitcoin", "Monero",
		"Mainnet", "Mainnet",
		0.00255308, 1.0,
		"btc", "xmr",
		"bc1qtest", "89Xtest",
		"FixedFloat",
	)

	checks := []string{
		"=== Swap Transaction ===",
		"From: 0.00255308 BTC (Mainnet)",
		"To:   1.00000000 XMR (Mainnet)",
		"Provider: FixedFloat",
		"Send address: bc1qtest",
		"Receive address: 89Xtest",
		"Asset: Bitcoin → Monero",
		"=== swapcli.com ===",
	}
	for _, c := range checks {
		if !strings.Contains(got, c) {
			t.Fatalf("clipboardStatusText missing expected content:\n%q\nwant substring: %q", got, c)
		}
	}
}

func TestClipboardStatusText_EmptyCoinNames(t *testing.T) {
	got := clipboardStatusText(
		"", "",
		"Mainnet", "Mainnet",
		0.01, 200,
		"btc", "usdt",
		"addr1", "addr2",
		"TestDEX",
	)
	if strings.Contains(got, "Asset:") {
		t.Fatalf("should not contain Asset line when coin names are empty:\n%s", got)
	}
	if !strings.Contains(got, "TestDEX") {
		t.Fatalf("should contain provider:\n%s", got)
	}
}

func TestClipboardStatusText_TickerCase(t *testing.T) {
	got := clipboardStatusText(
		"", "", "Polygon", "Polygon",
		0.5, 100, "matic", "usdc",
		"0xtest", "0xrecv",
		"QuickSwap",
	)
	if !strings.Contains(got, "MATIC") {
		t.Fatalf("ticker should be uppercased:\n%s", got)
	}
	if !strings.Contains(got, "USDC") {
		t.Fatalf("ticker should be uppercased:\n%s", got)
	}
}
