package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	pb "github.com/lfaoro/swap/gen/go/swap/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type HTTPSwapAPI struct {
	base   string
	client *http.Client
	ctx    context.Context
	cancel context.CancelFunc

	mu         sync.Mutex
	streamBody io.ReadCloser
}

func NewHTTPSwapAPI(baseURL string) APIClient {
	ctx, cancel := context.WithCancel(context.Background())
	return &HTTPSwapAPI{
		base:   baseURL,
		client: &http.Client{Timeout: 10 * time.Second},
		ctx:    ctx,
		cancel: cancel,
	}
}

func (h *HTTPSwapAPI) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.cancel != nil {
		h.cancel()
	}
	if h.streamBody != nil {
		_ = h.streamBody.Close()
		h.streamBody = nil
	}

	// Keep the client reusable after cancelling a running stream.
	h.ctx, h.cancel = context.WithCancel(context.Background())
}

func (h *HTTPSwapAPI) currentContext() context.Context {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.ctx
}

func (h *HTTPSwapAPI) setActiveStreamBody(body io.ReadCloser) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.streamBody != nil {
		_ = h.streamBody.Close()
	}
	h.streamBody = body
}

func (h *HTTPSwapAPI) clearActiveStreamBody(body io.ReadCloser) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.streamBody == body {
		h.streamBody = nil
	}
}

func (h *HTTPSwapAPI) url(path string) string {
	u, _ := url.JoinPath(h.base, path)
	return u
}

// ListCoins calls GET /v1/coins and returns CoinReqRespMsg
func (h *HTTPSwapAPI) ListCoins() tea.Cmd {
	return func() tea.Msg {
		resp, err := h.client.Get(h.url("/v1/coins"))
		if err != nil {
			return tea.Batch(
				AddLog("http api: error (ListCoins): %v", err),
				AddError(fmt.Errorf("connection error: unable to load coins table")),
			)()
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			data, _ := io.ReadAll(resp.Body)
			body := strings.TrimSpace(string(data))
			if body == "" {
				body = resp.Status
			}
			return tea.Batch(
				AddLog("http api: listcoins non-200 (%d): %s", resp.StatusCode, body),
				AddError(fmt.Errorf("coins request failed (%d): %s", resp.StatusCode, body)),
			)()
		}

		var body struct {
			Coins []struct {
				Name    string  `json:"name"`
				Ticker  string  `json:"ticker"`
				Network string  `json:"network"`
				Minimum float64 `json:"minimum"`
				Maximum float64 `json:"maximum"`
			} `json:"coins"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			return tea.Batch(
				AddLog("http api: error decoding ListCoins: %v", err),
				AddError(fmt.Errorf("invalid response from server")),
			)()
		}

		coins := make([]*pb.Coin, 0, len(body.Coins))
		for _, c := range body.Coins {
			coins = append(coins, &pb.Coin{
				Name:    c.Name,
				Ticker:  c.Ticker,
				Network: c.Network,
				Minimum: c.Minimum,
				Maximum: c.Maximum,
			})
		}

		return tea.Batch(
			AddLog("http api: success (ListCoins)"),
			func() tea.Msg { return CoinReqRespMsg{Resp: coins} },
		)()
	}
}

// SwapRate posts a request to /v1/swaprate and returns SwapRateRespMsg
func (h *HTTPSwapAPI) SwapRate(req SwapRateReqMsg) tea.Cmd {
	return func() tea.Msg {
		// For brevity, send a minimal JSON payload and expect a compatible response
		payload := map[string]any{
			"amount_from":  req.AmountFrom,
			"amount_to":    req.AmountTo,
			"ticker_from":  req.TickerFrom,
			"ticker_to":    req.TickerTo,
			"network_from": req.NetworkFrom,
			"network_to":   req.NetworkTo,
			"payment":      req.Payment,
		}
		b, _ := json.Marshal(payload)
		resp, err := h.client.Post(h.url("/v1/swaprate"), "application/json", bytes.NewReader(b))
		if err != nil {
			return tea.Batch(
				AddLog("http api: error (SwapRate): %v", err),
				AddError(fmt.Errorf("connection error: swap rate")),
			)()
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			data, _ := io.ReadAll(resp.Body)
			body := strings.TrimSpace(string(data))
			if body == "" {
				body = resp.Status
			}
			return tea.Batch(
				AddLog("http api: swaprate non-200 (%d): %s", resp.StatusCode, body),
				AddError(fmt.Errorf("swaprate failed (%d): %s", resp.StatusCode, body)),
			)()
		}

		var jr struct {
			TickerFrom string   `json:"ticker_from"`
			TickerTo   string   `json:"ticker_to"`
			Warnings   []string `json:"warnings"`
			Quotes     []struct {
				Provider       string  `json:"provider"`
				AmountTo       string  `json:"amount_to"`
				AmountTo_USD   string  `json:"amount_to_usd"`
				AmountFrom     string  `json:"amount_from"`
				AmountFrom_USD string  `json:"amount_from_usd"`
				Fixed          string  `json:"fixed"`
				Kycrating      string  `json:"kyc"`
				Eta            float64 `json:"eta"`
				Waste          string  `json:"waste"`
			} `json:"quotes"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&jr); err != nil {
			return tea.Batch(
				AddLog("http api: error decoding SwapRate: %v", err),
				AddError(fmt.Errorf("invalid swaprate response")),
			)()
		}

		// Map to protobuf types
		out := &pb.SwapRateResponse{
			TickerFrom: jr.TickerFrom,
			TickerTo:   jr.TickerTo,
			Quotes:     &pb.QuoteList{},
		}
		for _, q := range jr.Quotes {
			out.Quotes.Quotes = append(out.Quotes.Quotes, &pb.QuoteDetails{
				Provider:       q.Provider,
				AmountTo:       q.AmountTo,
				AmountTo_USD:   q.AmountTo_USD,
				AmountFrom:     q.AmountFrom,
				AmountFrom_USD: q.AmountFrom_USD,
				Fixed:          q.Fixed,
				Kycrating:      q.Kycrating,
				Eta:            q.Eta,
				Waste:          q.Waste,
			})
		}

		return tea.Batch(
			AddLog("http api: success (SwapRate)"),
			func() tea.Msg { return SwapRateRespMsg{Resp: out, Warnings: jr.Warnings} },
		)()
	}
}

// SwapTrade posts to /v1/swaptrade
func (h *HTTPSwapAPI) SwapTrade(req SwapTradeReq) tea.Cmd {
	return func() tea.Msg {
		payload := map[string]any{
			"id":           req.TradeID,
			"ticker_from":  req.From.Ticker,
			"ticker_to":    req.To.Ticker,
			"network_from": req.From.Network,
			"network_to":   req.To.Network,
			"amount_from":  req.Amount.Value(),
			"amount_to":    req.Amount.Value(),
			"payment":      req.Payment,
			"address":      req.Address.Value(),
			"provider":     req.Exchange,
		}
		b, _ := json.Marshal(payload)
		resp, err := h.client.Post(h.url("/v1/swaptrade"), "application/json", bytes.NewReader(b))
		if err != nil {
			return tea.Batch(
				AddLog("http api: error (SwapTrade): %v", err),
				AddError(err),
			)()
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			data, _ := io.ReadAll(resp.Body)
			body := strings.TrimSpace(string(data))
			if body == "" {
				body = resp.Status
			}
			return tea.Batch(
				AddLog("http api: swaptrade non-200 (%d): %s", resp.StatusCode, body),
				AddError(fmt.Errorf("swaptrade failed (%d): %s", resp.StatusCode, body)),
			)()
		}

		var jr struct {
			Status  string `json:"status"`
			TradeId string `json:"trade_id"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&jr); err != nil {
			return tea.Batch(
				AddLog("http api: error decoding SwapTrade: %v", err),
				AddError(fmt.Errorf("invalid swaptrade response")),
			)()
		}
		out := &pb.SwapTradeResponse{Status: jr.Status, TradeId: jr.TradeId}
		return tea.Batch(
			AddLog("http api: success (SwapTrade) %v", out.GetStatus()),
			func() tea.Msg { return SwapTradeRespMsg{out} },
		)()
	}
}

// SwapStatus connects to an SSE stream at /v1/swapstatus/stream?tradeid=...
func (h *HTTPSwapAPI) SwapStatus(req SwapStatusReqMsg) tea.Cmd {
	return func() tea.Msg {
		// Connect to stream
		streamURL := h.url("/v1/swapstatus/stream") + "?tradeid=" + url.QueryEscape(req.TradeID)
		streamCtx := h.currentContext()
		httpReq, err := http.NewRequestWithContext(streamCtx, http.MethodGet, streamURL, nil)
		if err != nil {
			return tea.Batch(
				AddLog("http api: error (SwapStatus request): %v", err),
				AddError(err),
			)()
		}

		r, err := h.client.Do(httpReq)
		if err != nil {
			return tea.Batch(
				AddLog("http api: error (SwapStatus connect): %v", err),
				AddError(err),
			)()
		}
		h.setActiveStreamBody(r.Body)

		decoder := json.NewDecoder(r.Body)

		var recvCmd tea.Cmd
		recvCmd = func() tea.Msg {
			var jr struct {
				Status          string  `json:"status"`
				TradeId         string  `json:"trade_id"`
				Date            string  `json:"date"`
				Provider        string  `json:"provider"`
				Fixed           bool    `json:"fixed"`
				Payment         bool    `json:"payment"`
				TickerFrom      string  `json:"ticker_from"`
				TickerTo        string  `json:"ticker_to"`
				CoinFrom        string  `json:"coin_from"`
				CoinTo          string  `json:"coin_to"`
				NetworkFrom     string  `json:"network_from"`
				NetworkTo       string  `json:"network_to"`
				AmountFrom      float64 `json:"amount_from"`
				AmountTo        float64 `json:"amount_to"`
				AddressProvider string  `json:"address_provider"`
				AddressUser     string  `json:"address_user"`
			}

			if err := decoder.Decode(&jr); err != nil {
				if err == io.EOF {
					_ = r.Body.Close()
					h.clearActiveStreamBody(r.Body)
					return nil
				}
				_ = r.Body.Close()
				h.clearActiveStreamBody(r.Body)
				if streamCtx.Err() != nil {
					return nil
				}
				return tea.Batch(
					AddLog("http api: error (SwapStatus): %v", err),
					AddError(err),
				)()
			}

			ts := timestamppb.Now()
			if jr.Date != "" {
				if parsed, err := time.Parse(time.RFC3339, jr.Date); err == nil {
					ts = timestamppb.New(parsed)
				}
			}

			ev := &pb.SwapStatusResponse{
				Status:          jr.Status,
				TradeId:         jr.TradeId,
				Date:            ts,
				Provider:        jr.Provider,
				Fixed:           jr.Fixed,
				Payment:         jr.Payment,
				TickerFrom:      jr.TickerFrom,
				TickerTo:        jr.TickerTo,
				CoinFrom:        jr.CoinFrom,
				CoinTo:          jr.CoinTo,
				NetworkFrom:     jr.NetworkFrom,
				NetworkTo:       jr.NetworkTo,
				AmountFrom:      jr.AmountFrom,
				AmountTo:        jr.AmountTo,
				AddressProvider: jr.AddressProvider,
				AddressUser:     jr.AddressUser,
			}

			return tea.Batch(
				AddLog("http api: success (SwapStatus): %v", ev.GetStatus()),
				func() tea.Msg { return SwapStatusRespMsg{ev} },
				recvCmd,
			)()
		}

		return recvCmd()
	}
}
