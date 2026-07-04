package app

import tea "github.com/charmbracelet/bubbletea"

// APIClient defines the methods used by the UI layer.
type APIClient interface {
    ListCoins() tea.Cmd
    SwapRate(req SwapRateReqMsg) tea.Cmd
    SwapTrade(req SwapTradeReq) tea.Cmd
    SwapStatus(req SwapStatusReqMsg) tea.Cmd
    Close()
}
