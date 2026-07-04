package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Coin struct {
	Name    string  `json:"name"`
	Ticker  string  `json:"ticker"`
	Network string  `json:"network"`
	Minimum float64 `json:"minimum"`
	Maximum float64 `json:"maximum"`
}

type Quote struct {
	Provider      string  `json:"provider"`
	AmountTo      string  `json:"amount_to"`
	AmountToUSD   string  `json:"amount_to_usd"`
	AmountFrom    string  `json:"amount_from"`
	AmountFromUSD string  `json:"amount_from_usd"`
	Fixed         string  `json:"fixed"`
	Kycrating     string  `json:"kyc"`
	Eta           float64 `json:"eta"`
	Waste         string  `json:"waste"`
}

type SwapRateRequest struct {
	AmountFrom  string `json:"amount_from"`
	AmountTo    string `json:"amount_to"`
	TickerFrom  string `json:"ticker_from"`
	NetworkFrom string `json:"network_from"`
	TickerTo    string `json:"ticker_to"`
	NetworkTo   string `json:"network_to"`
	ChainID     string `json:"chain_id"`
	Taker       string `json:"taker"`
	Payment     bool   `json:"payment"`
}

type SwapRateResponse struct {
	TickerFrom string  `json:"ticker_from"`
	TickerTo   string  `json:"ticker_to"`
	Quotes     []Quote `json:"quotes"`
}

type SwapTradeRequest struct {
	ID          string `json:"id"`
	TickerFrom  string `json:"ticker_from"`
	NetworkFrom string `json:"network_from"`
	TickerTo    string `json:"ticker_to"`
	NetworkTo   string `json:"network_to"`
	AmountFrom  string `json:"amount_from"`
	AmountTo    string `json:"amount_to"`
	Payment     bool   `json:"payment"`
	Address     string `json:"address"`
	Provider    string `json:"provider"`
}

type SwapTradeResponse struct {
	Status  string `json:"status"`
	TradeId string `json:"trade_id"`
}

type SwapStatusEvent struct {
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

type tradeRecord struct {
	request     SwapTradeRequest
	createdAt   time.Time
	status      string
	addressTo   string
	addressUser string
}

type QuoteProvider interface {
	GetQuotes(ctx context.Context, req SwapRateRequest) ([]Quote, error)
}

type MockQuoteProvider struct{}

type ExternalQuoteProvider struct {
	client  *http.Client
	baseURL string
	apiKey  string
}

type OneInchQuoteProvider struct {
	client  *http.Client
	baseURL string
}

type ZeroXQuoteProvider struct {
	client  *http.Client
	baseURL string
	apiKey  string
	chainID string
	taker   string
}

const (
	defaultZeroXChainID = "1"
	defaultZeroXTaker   = "0x0000000000000000000000000000000000010000"
)

var supportedZeroXChains = map[string]struct{}{
	"1":     {},
	"10":    {},
	"137":   {},
	"42161": {},
	"56":    {},
	"8453":  {}, // Base
	"43114": {}, // Avalanche C-Chain
}

var (
	quoteProvider QuoteProvider
	coins         = []Coin{
		{Name: "Bitcoin", Ticker: "btc", Network: "Mainnet", Minimum: 0.00001, Maximum: 10000},
		{Name: "Monero", Ticker: "xmr", Network: "Mainnet", Minimum: 0.01, Maximum: 100000},
		{Name: "Ethereum", Ticker: "eth", Network: "Mainnet", Minimum: 0.01, Maximum: 50000},
	}
	trades = struct {
		sync.Mutex
		data map[string]*tradeRecord
	}{data: make(map[string]*tradeRecord)}
)

func main() {
	quoteProvider = newQuoteProvider()

	http.HandleFunc("/", rootHandler)
	http.HandleFunc("/healthz", healthzHandler)
	http.HandleFunc("/v1/coins", coinsHandler)
	http.HandleFunc("/v1/swaprate", swapRateHandler)
	http.HandleFunc("/v1/swaptrade", swapTradeHandler)
	http.HandleFunc("/v1/swapstatus/stream", swapStatusStreamHandler)

	addr := ":8081"
	srv := &http.Server{
		Addr:         addr,
		Handler:      nil,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 20 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
	log.Printf("http backend listening on %s using quote provider %T", addr, quoteProvider)
	log.Fatal(srv.ListenAndServe())
}

func rootHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("Swap HTTP backend is running.\n" +
		"Use /v1/coins, /v1/swaprate, /v1/swaptrade, /v1/swapstatus/stream.\n" +
		"Optional 0x provider environment variables:\n" +
		"  SWAP_QUOTE_PROVIDER=0x\n" +
		"  SWAP_0X_API_KEY=<your-0x-api-key>\n" +
		"  SWAP_0X_CHAIN_ID=1|10|137|42161|56 (default is 1)\n" +
		"  SWAP_0X_TAKER=0x0000000000000000000000000000000000010000 (default valid Permit2 taker)\n" +
		"Request body field:\n" +
		"  chain_id — optional chain override for /v1/swaprate when using 0x.\n" +
		"Supported tokens: ETH, USDC, DAI, WBTC (chain-specific mapping)"))
}

func healthzHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ok"))
}

func coinsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"coins": coins})
}

func swapRateHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req SwapRateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	req.TickerFrom = strings.TrimSpace(req.TickerFrom)
	req.TickerTo = strings.TrimSpace(req.TickerTo)
	req.AmountFrom = strings.TrimSpace(req.AmountFrom)

	if req.TickerFrom == "" || req.TickerTo == "" {
		http.Error(w, "missing ticker_from or ticker_to", http.StatusBadRequest)
		return
	}
	if strings.EqualFold(req.TickerFrom, req.TickerTo) {
		http.Error(w, "ticker_from and ticker_to must differ", http.StatusBadRequest)
		return
	}
	if req.AmountFrom == "" {
		http.Error(w, "amount_from is required", http.StatusBadRequest)
		return
	}
	req.ChainID = strings.TrimSpace(req.ChainID)
	if req.ChainID != "" {
		if _, ok := supportedZeroXChains[req.ChainID]; !ok {
			http.Error(w, "unsupported chain_id; supported values are 1, 10, 137, 42161, 56", http.StatusBadRequest)
			return
		}
	}
	if _, err := parsePositiveAmount(req.AmountFrom); err != nil {
		http.Error(w, "amount_from must be a positive number", http.StatusBadRequest)
		return
	}

	quotes, err := quoteProvider.GetQuotes(r.Context(), req)
	if err != nil {
		log.Printf("swaprate: provider error: %v", err)
		http.Error(w, fmt.Sprintf("failed to retrieve quote: %v", err), http.StatusBadGateway)
		return
	}

	out := SwapRateResponse{
		TickerFrom: req.TickerFrom,
		TickerTo:   req.TickerTo,
		Quotes:     quotes,
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

func swapTradeHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req SwapTradeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	tradeID := req.ID
	if tradeID == "" {
		tradeID = newTradeID()
	}
	if req.Provider == "" {
		req.Provider = "MockHTTP"
	}

	trades.Lock()
	trades.data[tradeID] = &tradeRecord{
		request:     req,
		createdAt:   time.Now(),
		status:      "waiting",
		addressTo:   req.Address,
		addressUser: req.Address,
	}
	trades.Unlock()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(SwapTradeResponse{Status: "ok", TradeId: tradeID})
}

func swapStatusStreamHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	tradeID := r.URL.Query().Get("tradeid")
	if tradeID == "" {
		http.Error(w, "tradeid required", http.StatusBadRequest)
		return
	}

	trades.Lock()
	trade, ok := trades.data[tradeID]
	trades.Unlock()
	if !ok {
		http.Error(w, "trade not found", http.StatusNotFound)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")

	statuses := []string{"waiting", "confirming", "sending", "finished"}
	for i, status := range statuses {
		trade.status = status
		event := SwapStatusEvent{
			Status:          status,
			TradeId:         tradeID,
			Date:            time.Now().Format(time.RFC3339),
			Provider:        trade.request.Provider,
			Fixed:           status != "waiting",
			Payment:         trade.request.Payment,
			TickerFrom:      trade.request.TickerFrom,
			TickerTo:        trade.request.TickerTo,
			CoinFrom:        coinName(trade.request.TickerFrom),
			CoinTo:          coinName(trade.request.TickerTo),
			NetworkFrom:     trade.request.NetworkFrom,
			NetworkTo:       trade.request.NetworkTo,
			AmountFrom:      parseAmount(trade.request.AmountFrom),
			AmountTo:        parseAmount(trade.request.AmountTo),
			AddressProvider: trade.addressTo,
			AddressUser:     trade.addressUser,
		}
		_ = json.NewEncoder(w).Encode(event)
		flusher.Flush()
		if i == len(statuses)-1 {
			return
		}
		time.Sleep(2 * time.Second)
	}
}

func buildQuote(req SwapRateRequest) Quote {
	amountFrom := parseAmount(req.AmountFrom)
	amountTo := parseAmount(req.AmountTo)
	if amountTo == 0 {
		amountTo = amountFrom * 50000
	}
	if amountFrom == 0 {
		amountFrom = 0.0001
		if req.Payment {
			amountTo = 1
		}
	}

	return Quote{
		Provider:      "MockHTTP",
		AmountTo:      fmt.Sprintf("%.6f", amountTo),
		AmountToUSD:   fmt.Sprintf("%.2f", amountTo*20000),
		AmountFrom:    fmt.Sprintf("%.8f", amountFrom),
		AmountFromUSD: fmt.Sprintf("%.2f", amountFrom*20000),
		Fixed:         "True",
		Kycrating:     "A",
		Eta:           5,
		Waste:         "0.5",
	}
}

func newTradeID() string {
	buf := make([]byte, 6)
	_, _ = rand.Read(buf)
	return fmt.Sprintf("TR-%s", hex.EncodeToString(buf))
}

func coinName(ticker string) string {
	for _, c := range coins {
		if c.Ticker == ticker {
			return c.Name
		}
	}
	return ticker
}

func newQuoteProvider() QuoteProvider {
	provider := strings.ToLower(strings.TrimSpace(os.Getenv("SWAP_QUOTE_PROVIDER")))
	switch provider {
	case "1inch", "oneinch", "1inch_public", "1inch-public":
		return newOneInchQuoteProvider()
	case "0x", "zerox", "0x_public", "0x-public":
		return newZeroXQuoteProvider()
	case "external", "generic", "simple":
		return newExternalQuoteProvider()
	case "mock", "", "none":
		return MockQuoteProvider{}
	default:
		log.Printf("unknown SWAP_QUOTE_PROVIDER=%q; using mock provider", provider)
		return MockQuoteProvider{}
	}
}

func newExternalQuoteProvider() QuoteProvider {
	quoteURL := os.Getenv("SWAP_QUOTE_URL")
	if quoteURL == "" {
		log.Printf("SWAP_QUOTE_URL not set; using mock provider")
		return MockQuoteProvider{}
	}

	if _, err := url.ParseRequestURI(quoteURL); err != nil {
		log.Printf("invalid SWAP_QUOTE_URL %q: %v; using mock provider", quoteURL, err)
		return MockQuoteProvider{}
	}

	apiKey := os.Getenv("SWAP_QUOTE_API_KEY")
	return &ExternalQuoteProvider{
		client:  &http.Client{Timeout: 15 * time.Second},
		baseURL: quoteURL,
		apiKey:  apiKey,
	}
}

func newOneInchQuoteProvider() QuoteProvider {
	quoteURL := os.Getenv("SWAP_QUOTE_URL")
	if quoteURL == "" {
		quoteURL = "https://api.1inch.io/v5.0"
	}
	return &OneInchQuoteProvider{
		client:  &http.Client{Timeout: 15 * time.Second},
		baseURL: quoteURL,
	}
}

func newZeroXQuoteProvider() QuoteProvider {
	quoteURL := os.Getenv("SWAP_QUOTE_URL")
	if quoteURL == "" {
		quoteURL = "https://api.0x.org"
	}
	apiKey := strings.TrimSpace(os.Getenv("SWAP_0X_API_KEY"))
	if apiKey == "" {
		log.Printf("SWAP_0X_API_KEY not set; 0x provider will fail until configured")
	}

	chainID := strings.TrimSpace(os.Getenv("SWAP_0X_CHAIN_ID"))
	if chainID == "" {
		chainID = defaultZeroXChainID
		log.Printf("SWAP_0X_CHAIN_ID not set; defaulting to %s", chainID)
	} else if _, ok := supportedZeroXChains[chainID]; !ok {
		log.Printf("SWAP_0X_CHAIN_ID %q configured; supported values are %v; defaulting to %s", chainID, supportedZeroXChainsList(), defaultZeroXChainID)
		chainID = defaultZeroXChainID
	}

	taker := strings.TrimSpace(os.Getenv("SWAP_0X_TAKER"))
	if taker == "" {
		taker = defaultZeroXTaker
		log.Printf("SWAP_0X_TAKER not set; defaulting to %s", taker)
	} else if !isValidEthAddress(taker) {
		log.Printf("SWAP_0X_TAKER %q is invalid; defaulting to %s", taker, defaultZeroXTaker)
		taker = defaultZeroXTaker
	}

	return &ZeroXQuoteProvider{
		client:  &http.Client{Timeout: 15 * time.Second},
		baseURL: quoteURL,
		apiKey:  apiKey,
		chainID: chainID,
		taker:   taker,
	}
}

func (MockQuoteProvider) GetQuotes(ctx context.Context, req SwapRateRequest) ([]Quote, error) {
	return []Quote{buildQuote(req)}, nil
}

func (p *ExternalQuoteProvider) GetQuotes(ctx context.Context, req SwapRateRequest) ([]Quote, error) {
	payload := map[string]any{
		"amount_from":  req.AmountFrom,
		"amount_to":    req.AmountTo,
		"ticker_from":  req.TickerFrom,
		"network_from": req.NetworkFrom,
		"ticker_to":    req.TickerTo,
		"network_to":   req.NetworkTo,
		"payment":      req.Payment,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	path := os.Getenv("SWAP_QUOTE_PATH")
	if path == "" {
		path = "/quote"
	}

	quoteURL, err := url.JoinPath(p.baseURL, strings.TrimPrefix(path, "/"))
	if err != nil {
		return nil, err
	}

	reqHTTP, err := http.NewRequestWithContext(ctx, http.MethodPost, quoteURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	reqHTTP.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		reqHTTP.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	resp, err := p.client.Do(reqHTTP)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("quote provider error %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}

	var out SwapRateResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out.Quotes, nil
}

func (p *OneInchQuoteProvider) GetQuotes(ctx context.Context, req SwapRateRequest) ([]Quote, error) {
	if req.TickerFrom == "" || req.TickerTo == "" {
		return nil, fmt.Errorf("ticker_from and ticker_to are required")
	}
	if req.AmountFrom == "" {
		return nil, fmt.Errorf("1inch public API requires amount_from")
	}

	fromToken := strings.ToUpper(req.TickerFrom)
	toToken := strings.ToUpper(req.TickerTo)
	chainId := os.Getenv("SWAP_1INCH_CHAIN_ID")
	if chainId == "" {
		chainId = "1"
	}

	fromAddr, err := oneInchTokenAddress(chainId, fromToken)
	if err != nil {
		return nil, fmt.Errorf("unsupported from token: %w", err)
	}
	toAddr, err := oneInchTokenAddress(chainId, toToken)
	if err != nil {
		return nil, fmt.Errorf("unsupported to token: %w", err)
	}

	quoteURL := fmt.Sprintf("%s/%s/quote?fromTokenAddress=%s&toTokenAddress=%s&amount=%s",
		strings.TrimSuffix(p.baseURL, "/"),
		chainId,
		url.QueryEscape(fromAddr),
		url.QueryEscape(toAddr),
		url.QueryEscape(req.AmountFrom),
	)

	reqHTTP, err := http.NewRequestWithContext(ctx, http.MethodGet, quoteURL, nil)
	if err != nil {
		return nil, err
	}
	reqHTTP.Header.Set("User-Agent", "swap-http-backend/1.0")

	resp, err := p.client.Do(reqHTTP)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("1inch quote error %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}

	var out struct {
		FromToken struct {
			Symbol string `json:"symbol"`
		} `json:"fromToken"`
		ToToken struct {
			Symbol string `json:"symbol"`
		} `json:"toToken"`
		ToTokenAmount   string `json:"toTokenAmount"`
		FromTokenAmount string `json:"fromTokenAmount"`
		EstimatedGas    int64  `json:"estimatedGas"`
		Protocols       any    `json:"protocols"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}

	quote := Quote{
		Provider:      "1inch",
		AmountTo:      out.ToTokenAmount,
		AmountToUSD:   "0",
		AmountFrom:    out.FromTokenAmount,
		AmountFromUSD: "0",
		Fixed:         "False",
		Kycrating:     "A",
		Eta:           1,
		Waste:         fmt.Sprintf("gas=%d", out.EstimatedGas),
	}
	return []Quote{quote}, nil
}

func (p *ZeroXQuoteProvider) GetQuotes(ctx context.Context, req SwapRateRequest) ([]Quote, error) {
	if req.TickerFrom == "" || req.TickerTo == "" {
		return nil, fmt.Errorf("ticker_from and ticker_to are required")
	}
	if req.AmountFrom == "" {
		return nil, fmt.Errorf("0x API requires amount_from")
	}

	fromSym := strings.ToUpper(req.TickerFrom)
	toSym := strings.ToUpper(req.TickerTo)

	chainID := p.chainID
	if req.ChainID != "" {
		chainID = req.ChainID
	}

	taker := p.taker
	if strings.TrimSpace(req.Taker) != "" {
		if !isValidEthAddress(req.Taker) {
			return nil, fmt.Errorf("invalid taker address")
		}
		taker = req.Taker
	}

	decimalsMap := map[string]int{
		"ETH":  18,
		"DAI":  18,
		"USDC": 6,
		"USDT": 6,
		"WBTC": 8,
	}
	dec := 18
	if d, ok := decimalsMap[fromSym]; ok {
		dec = d
	}

	sellAmount, err := decimalToBaseUnits(req.AmountFrom, dec)
	if err != nil {
		return nil, fmt.Errorf("invalid amount_from: %w", err)
	}

	if _, ok := supportedZeroXChains[chainID]; !ok {
		return nil, fmt.Errorf("unsupported chain id %q; supported values are %v", chainID, supportedZeroXChainsList())
	}

	fromAddr, err := zeroXTokenAddress(chainID, fromSym)
	if err != nil {
		return nil, fmt.Errorf("unsupported from token: %w", err)
	}
	toAddr, err := zeroXTokenAddress(chainID, toSym)
	if err != nil {
		return nil, fmt.Errorf("unsupported to token: %w", err)
	}

	log.Printf("0x request params: chain=%s sellToken=%s buyToken=%s sellAmount=%s taker=%s",
		chainID, fromAddr, toAddr, sellAmount, taker)

	quoteURL := fmt.Sprintf("%s/swap/permit2/quote?chainId=%s&sellToken=%s&buyToken=%s&sellAmount=%s&taker=%s",
		strings.TrimSuffix(p.baseURL, "/"), url.QueryEscape(chainID), url.QueryEscape(fromAddr), url.QueryEscape(toAddr), url.QueryEscape(sellAmount), url.QueryEscape(taker))

	reqHTTP, err := http.NewRequestWithContext(ctx, http.MethodGet, quoteURL, nil)
	if err != nil {
		return nil, err
	}
	reqHTTP.Header.Set("User-Agent", "swap-http-backend/1.0")
	reqHTTP.Header.Set("Accept", "application/json")

	if p.apiKey == "" {
		return nil, fmt.Errorf("0x API requires SWAP_0X_API_KEY environment variable")
	}
	reqHTTP.Header.Set("0x-api-key", p.apiKey)
	reqHTTP.Header.Set("0x-version", "v2")

	resp, err := p.client.Do(reqHTTP)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		bodyStr := strings.TrimSpace(string(data))
		// If 0x complains about sellToken being invalid, and we're quoting ETH,
		// try once more using the chain WETH contract address as a fallback.
		if strings.Contains(bodyStr, "sellToken") && strings.Contains(bodyStr, "Invalid ethereum address") && fromSym == "ETH" {
			if w, ok := wrappedETHAddress(chainID); ok {
				log.Printf("0x sellToken invalid; retrying with WETH=%s on chain %s", w, chainID)
				fromAddr = w
				// rebuild URL with wrapped ETH
				quoteURL = fmt.Sprintf("%s/swap/permit2/quote?chainId=%s&sellToken=%s&buyToken=%s&sellAmount=%s&taker=%s",
					strings.TrimSuffix(p.baseURL, "/"), url.QueryEscape(chainID), url.QueryEscape(fromAddr), url.QueryEscape(toAddr), url.QueryEscape(sellAmount), url.QueryEscape(taker))
				reqHTTP, err = http.NewRequestWithContext(ctx, http.MethodGet, quoteURL, nil)
				if err != nil {
					return nil, err
				}
				reqHTTP.Header.Set("User-Agent", "swap-http-backend/1.0")
				reqHTTP.Header.Set("Accept", "application/json")
				reqHTTP.Header.Set("0x-api-key", p.apiKey)
				reqHTTP.Header.Set("0x-version", "v2")

				resp2, err := p.client.Do(reqHTTP)
				if err != nil {
					return nil, err
				}
				defer resp2.Body.Close()
				if resp2.StatusCode != http.StatusOK {
					data2, _ := io.ReadAll(resp2.Body)
					return nil, fmt.Errorf("0x quote error %d: %s", resp2.StatusCode, strings.TrimSpace(string(data2)))
				}
				// use resp2 as the successful response
				resp = resp2
			} else {
				return nil, fmt.Errorf("0x quote error %d: %s", resp.StatusCode, bodyStr)
			}
		} else {
			return nil, fmt.Errorf("0x quote error %d: %s", resp.StatusCode, bodyStr)
		}
	}

	var out struct {
		BuyAmount  string `json:"buyAmount"`
		SellAmount string `json:"sellAmount"`
		Gas        int64  `json:"gas"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}

	quote := Quote{
		Provider:      "0x",
		AmountTo:      out.BuyAmount,
		AmountToUSD:   "0",
		AmountFrom:    out.SellAmount,
		AmountFromUSD: "0",
		Fixed:         "False",
		Kycrating:     "A",
		Eta:           1,
		Waste:         fmt.Sprintf("gas=%d", out.Gas),
	}
	return []Quote{quote}, nil
}

// decimalToBaseUnits converts a decimal amount string (e.g. "0.1") to base units
// for the given decimals (e.g. 18 -> wei). It returns an integer string.
func decimalToBaseUnits(amount string, decimals int) (string, error) {
	f, ok := new(big.Float).SetPrec(256).SetString(amount)
	if !ok {
		return "", fmt.Errorf("invalid decimal amount")
	}
	mul := new(big.Float).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil))
	f.Mul(f, mul)
	bi := new(big.Int)
	f.Int(bi) // truncate toward zero
	return bi.String(), nil
}
func zeroXTokenAddress(chainId, symbol string) (string, error) {
	symbol = strings.ToUpper(symbol)
	switch chainId {
	case "1":
		switch symbol {
		case "ETH":
			return "0xEeeeeEeeeEeEeeEeEeEeeEEEeeeeEeeeeeeeEEeE", nil
		case "USDC":
			return "0xA0b86991c6218b36c1d19d4a2e9eb0ce3606eb48", nil
		case "DAI":
			return "0x6B175474E89094C44Da98b954EedeAC495271d0F", nil
		case "WBTC":
			return "0x2260FAC5E5542a773Aa44fBCfeDf7C193bc2C599", nil
		case "USDT":
			return "0xdAC17F958D2ee523a2206206994597C13D831ec7", nil
		default:
			return "", fmt.Errorf("unsupported token %s on chain %s", symbol, chainId)
		}
	case "10":
		switch symbol {
		case "ETH":
			return "0x4200000000000000000000000000000000000006", nil
		case "USDC":
			return "0x7F5c764cBc14f9669B88837ca1490cCa17c31607", nil
		case "DAI":
			return "0xDA10009cBd5D07dd0CeCc66161FC93D7c9000da1", nil
		case "USDT":
			return "0x7f3f7A2065dC5680d8c8cEce3e30bbf6D4d8236A", nil
		default:
			return "", fmt.Errorf("unsupported token %s on chain %s", symbol, chainId)
		}
	case "137":
		switch symbol {
		case "ETH":
			return "0x0000000000000000000000000000000000001010", nil
		case "USDC":
			return "0x2791Bca1f2de4661ED88A30C99A7a9449Aa84174", nil
		case "DAI":
			return "0x8f3Cf7ad23Cd3CaDbD9735AFf958023239c6A063", nil
		case "USDT":
			return "0x3813e82e6f7098b9583FC0F33a962D02018B6803", nil
		default:
			return "", fmt.Errorf("unsupported token %s on chain %s", symbol, chainId)
		}
	case "42161":
		switch symbol {
		case "ETH":
			return "0xEeeeeEeeeEeEeeEeEeEeeEEEeeeeEeeeeeeeEEeE", nil
		case "USDC":
			return "0xFF970A61A04b1cA14834A43f5dE4533eBDDB5CC8", nil
		case "DAI":
			return "0xDA10009cBd5D07dd0CeCc66161FC93D7c9000da1", nil
		case "USDT":
			return "0xFd086bC7CD5C481DCC9C85ebE478A1C0b69FCbb9", nil
		default:
			return "", fmt.Errorf("unsupported token %s on chain %s", symbol, chainId)
		}
	case "56":
		switch symbol {
		case "ETH":
			return "0xEeeeeEeeeEeEeeEeEeEeeEEEeeeeEeeeeeeeEEeE", nil
		case "USDC":
			return "0x8ac76a51cc950d9822d68b83fe1ad97b32cd580d", nil
		case "DAI":
			return "0x1AF3F329e8BE154074D8769D1FFa4eE058B1DBc3", nil
		case "USDT":
			return "0x55d398326f99059fF775485246999027B3197955", nil
		default:
			return "", fmt.Errorf("unsupported token %s on chain %s", symbol, chainId)
		}
	case "8453": // Base
		switch symbol {
		case "ETH":
			return "0xEeeeeEeeeEeEeeEeEeEeeEEEeeeeEeeeeeeeEEeE", nil
		case "USDC":
			return "0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913", nil
		case "DAI":
			return "0x50c5725949A6F0c72E6C4a641F24049A917DB0Cb", nil
		case "USDT":
			return "0xfde4C96c8593536E31F229EA8f37b2ADa2699bb2", nil
		default:
			return "", fmt.Errorf("unsupported token %s on chain %s", symbol, chainId)
		}
	case "43114": // Avalanche C-Chain
		switch symbol {
		case "ETH":
			// Avalanche has no native ETH; use Wrapped Ether (WETH.e) bridged via Avalanche Bridge
			return "0x49D5c2BdFfac6CE2BFdB6640F4F80f226bc10bAB", nil
		case "USDC":
			return "0xB97EF9Ef8734C71904D8002F8b6Bc66Dd9c48a6E", nil
		case "DAI":
			return "0xd586E7F844cEa2F87f50152665BCbc2C279D8d70", nil
		case "USDT":
			return "0x9702230A8Ea53601f5cD2dc00fDBc13d4dF4A8c7", nil
		default:
			return "", fmt.Errorf("unsupported token %s on chain %s", symbol, chainId)
		}
	default:
		return "", fmt.Errorf("unsupported chain id %s", chainId)
	}
}

// wrappedETHAddress returns a common WETH contract address for chains where
// 0x may require a wrapped token address instead of the native placeholder.
func wrappedETHAddress(chainId string) (string, bool) {
	switch chainId {
	case "42161":
		// Arbitrum WETH
		return "0x82af49447d8a07e3bd95bd0d56f35241523fbab1", true
	case "56":
		// BSC: Binance-Peg Ethereum Token
		return "0x2170Ed0880ac9A755fd29B2688956BD959F933F8", true
	case "8453":
		// Base WETH
		return "0x4200000000000000000000000000000000000006", true
	case "43114":
		// Avalanche: WETH.e (bridged via Avalanche Bridge)
		return "0x49D5c2BdFfac6CE2BFdB6640F4F80f226bc10bAB", true
	default:
		return "", false
	}
}

func oneInchTokenAddress(chainId, symbol string) (string, error) {
	return zeroXTokenAddress(chainId, symbol)
}

func parseAmount(value string) float64 {
	if value == "" {
		return 0
	}
	f, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0
	}
	return math.Round(f*1e8) / 1e8
}

func parsePositiveAmount(value string) (float64, error) {
	if value == "" {
		return 0, fmt.Errorf("empty amount")
	}
	amt, err := strconv.ParseFloat(value, 64)
	if err != nil || amt <= 0 {
		return 0, fmt.Errorf("invalid positive amount")
	}
	return amt, nil
}

func isValidEthAddress(value string) bool {
	if !strings.HasPrefix(value, "0x") {
		return false
	}
	if len(value) != 42 {
		return false
	}
	_, err := hex.DecodeString(value[2:])
	return err == nil
}

func supportedZeroXChainsList() []string {
	return []string{"1", "10", "137", "42161", "56"}
}
