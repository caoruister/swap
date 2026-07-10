package app

import (
	"fmt"
	"strings"

	"github.com/atotto/clipboard"
)

// CopyToClipboard attempts to write text to the system clipboard.
// Never blocks or crashes on error; silently fails if clipboard is unavailable.
func CopyToClipboard(text string) error {
	return clipboard.WriteAll(text)
}

// clipboardStatusText formats a status update message for clipboard copying.
func clipboardStatusText(coinFrom, coinTo, networkFrom, networkTo string, amountFrom, amountTo float64, tickerFrom, tickerTo, addressProvider, addressUser, provider string) string {
	var b strings.Builder
	b.WriteString("=== Swap Transaction ===\n")
	b.WriteString(fmt.Sprintf("From: %.8f %s (%s)\n", amountFrom, strings.ToUpper(tickerFrom), networkFrom))
	b.WriteString(fmt.Sprintf("To:   %.8f %s (%s)\n", amountTo, strings.ToUpper(tickerTo), networkTo))
	b.WriteString(fmt.Sprintf("Provider: %s\n", provider))
	b.WriteString(fmt.Sprintf("Send address: %s\n", addressProvider))
	b.WriteString(fmt.Sprintf("Receive address: %s\n", addressUser))
	if coinFrom != "" && coinTo != "" {
		b.WriteString(fmt.Sprintf("Asset: %s → %s\n", coinFrom, coinTo))
	}
	b.WriteString("=== swapcli.com ===")
	return b.String()
}
