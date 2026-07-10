package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
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
	TickerFrom string   `json:"ticker_from"`
	TickerTo   string   `json:"ticker_to"`
	Quotes     []Quote  `json:"quotes"`
	Warnings   []string `json:"warnings,omitempty"`
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
	GetQuotes(ctx context.Context, req SwapRateRequest) ([]Quote, []string, error)
}

type MockQuoteProvider struct{}

type ExternalQuoteProvider struct {
	client  *http.Client
	baseURL string
	apiKey  string
	path    string
	cKey    string
	cbFails int
	cbTTL   time.Duration
}

type OneInchQuoteProvider struct {
	client  *http.Client
	baseURL string
}

type OpenOceanQuoteProvider struct {
	client  *http.Client
	baseURL string
	chainID string
	apiKey  string
}

type OdosQuoteProvider struct {
	client  *http.Client
	baseURL string
	chainID string
	apiKey  string
}

type ParaSwapQuoteProvider struct {
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

type namedQuoteProvider struct {
	name     string
	provider QuoteProvider
}

type MultiQuoteProvider struct {
	providers []namedQuoteProvider
}

type requestIDContextKey struct{}

type externalCircuitState struct {
	fails            int
	openUntil        time.Time
	halfOpenInFlight bool
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
		{Name: "Bitcoin (Optimism)", Ticker: "btc", Network: "Optimism", Minimum: 0.00001, Maximum: 10000},
		{Name: "Bitcoin (Arbitrum One)", Ticker: "btc", Network: "Arbitrum One", Minimum: 0.00001, Maximum: 10000},
		{Name: "Bitcoin (Polygon)", Ticker: "btc", Network: "Polygon", Minimum: 0.00001, Maximum: 10000},
		{Name: "Bitcoin (BEP20)", Ticker: "btc", Network: "BEP20", Minimum: 0.00001, Maximum: 10000},
		{Name: "Bitcoin (Avalanche)", Ticker: "btc", Network: "Avalanche", Minimum: 0.00001, Maximum: 10000},
		{Name: "Ethereum", Ticker: "eth", Network: "Mainnet", Minimum: 0.001, Maximum: 50000},
		{Name: "Ethereum (Optimism)", Ticker: "eth", Network: "Optimism", Minimum: 0.001, Maximum: 50000},
		{Name: "Ethereum (Arbitrum One)", Ticker: "eth", Network: "Arbitrum One", Minimum: 0.001, Maximum: 50000},
		{Name: "Ethereum (Polygon)", Ticker: "eth", Network: "Polygon", Minimum: 0.001, Maximum: 50000},
		{Name: "Ethereum (BEP20)", Ticker: "eth", Network: "BEP20", Minimum: 0.001, Maximum: 50000},
		{Name: "Ethereum (Base)", Ticker: "eth", Network: "Base", Minimum: 0.001, Maximum: 50000},
		{Name: "Ethereum (Avalanche)", Ticker: "eth", Network: "Avalanche", Minimum: 0.001, Maximum: 50000},
		{Name: "USD Coin", Ticker: "usdc", Network: "Mainnet", Minimum: 1, Maximum: 1000000},
		{Name: "USD Coin (Optimism)", Ticker: "usdc", Network: "Optimism", Minimum: 1, Maximum: 1000000},
		{Name: "USD Coin (Arbitrum One)", Ticker: "usdc", Network: "Arbitrum One", Minimum: 1, Maximum: 1000000},
		{Name: "USD Coin (Polygon)", Ticker: "usdc", Network: "Polygon", Minimum: 1, Maximum: 1000000},
		{Name: "USD Coin (BEP20)", Ticker: "usdc", Network: "BEP20", Minimum: 1, Maximum: 1000000},
		{Name: "USD Coin (Base)", Ticker: "usdc", Network: "Base", Minimum: 1, Maximum: 1000000},
		{Name: "USD Coin (Avalanche)", Ticker: "usdc", Network: "Avalanche", Minimum: 1, Maximum: 1000000},
		{Name: "Tether", Ticker: "usdt", Network: "Mainnet", Minimum: 1, Maximum: 1000000},
		{Name: "Tether (Optimism)", Ticker: "usdt", Network: "Optimism", Minimum: 1, Maximum: 1000000},
		{Name: "Tether (Arbitrum One)", Ticker: "usdt", Network: "Arbitrum One", Minimum: 1, Maximum: 1000000},
		{Name: "Tether (Polygon)", Ticker: "usdt", Network: "Polygon", Minimum: 1, Maximum: 1000000},
		{Name: "Tether (BEP20)", Ticker: "usdt", Network: "BEP20", Minimum: 1, Maximum: 1000000},
		{Name: "Tether (Base)", Ticker: "usdt", Network: "Base", Minimum: 1, Maximum: 1000000},
		{Name: "Tether (Avalanche)", Ticker: "usdt", Network: "Avalanche", Minimum: 1, Maximum: 1000000},
		{Name: "DAI", Ticker: "dai", Network: "Mainnet", Minimum: 1, Maximum: 1000000},
		{Name: "DAI (Optimism)", Ticker: "dai", Network: "Optimism", Minimum: 1, Maximum: 1000000},
		{Name: "DAI (Arbitrum One)", Ticker: "dai", Network: "Arbitrum One", Minimum: 1, Maximum: 1000000},
		{Name: "DAI (Polygon)", Ticker: "dai", Network: "Polygon", Minimum: 1, Maximum: 1000000},
		{Name: "DAI (BEP20)", Ticker: "dai", Network: "BEP20", Minimum: 1, Maximum: 1000000},
		{Name: "DAI (Base)", Ticker: "dai", Network: "Base", Minimum: 1, Maximum: 1000000},
		{Name: "DAI (Avalanche)", Ticker: "dai", Network: "Avalanche", Minimum: 1, Maximum: 1000000},
	}
	coinsClient = &http.Client{Timeout: 10 * time.Second}
	coinsCache  = struct {
		sync.RWMutex
		data      []Coin
		expiresAt time.Time
	}{}
	trades = struct {
		sync.Mutex
		data map[string]*tradeRecord
	}{data: make(map[string]*tradeRecord)}
	usdPriceClient = &http.Client{Timeout: 6 * time.Second}
	usdPriceCache  = struct {
		sync.RWMutex
		prices    map[string]float64
		updatedAt time.Time
		expiresAt time.Time
	}{prices: make(map[string]float64)}
	externalCircuit = struct {
		sync.Mutex
		state map[string]externalCircuitState
	}{state: make(map[string]externalCircuitState)}
)

const usdPriceStaleWarning = "usd prices: using cached market data"

// quoteUserAgent is used for outbound requests to quote providers.
// A standard-looking UA avoids Cloudflare bot detection on public APIs.
const quoteUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) swap-backend/1.0"

// setQuoteHeaders sets common HTTP headers on outbound quote requests to
// reduce the chance of being blocked by Cloudflare or similar WAFs.
func setQuoteHeaders(req *http.Request) {
	req.Header.Set("User-Agent", quoteUserAgent)
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
}

func main() {
	quoteProvider = newQuoteProvider()

	// Run startup self-check to verify provider availability
	quoteProvider = runProviderSelfCheck(quoteProvider)

	http.HandleFunc("/", rootHandler)
	http.HandleFunc("/healthz", healthzHandler)
	http.HandleFunc("/v1/coins", coinsHandler)
	http.HandleFunc("/v1/swaprate", swapRateHandler)
	http.HandleFunc("/v1/swaptrade", swapTradeHandler)
	http.HandleFunc("/v1/swapstatus/stream", swapStatusStreamHandler)
	http.HandleFunc("/v1/healthz/providers", providerHealthHandler)

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
		"    or multi-provider mode: SWAP_QUOTE_PROVIDER=0x,paraswap (also supports commas/pluses)\n" +
		"    external aliases are supported: SWAP_QUOTE_PROVIDER=0x,external:foo,external:bar\n" +
		"    built-in OpenOcean provider: SWAP_QUOTE_PROVIDER=openocean\n" +
		"      SWAP_QUOTE_URL_FOO=https://example-foo/quote-api  SWAP_QUOTE_API_KEY_FOO=<optional>\n" +
		"      SWAP_QUOTE_URL_BAR=https://example-bar/quote-api  SWAP_QUOTE_API_KEY_BAR=<optional>\n" +
		"      SWAP_QUOTE_PATH_FOO=/quote (optional, default /quote or SWAP_QUOTE_PATH)\n" +
		"      SWAP_QUOTE_PATH_BAR=/quote (optional, default /quote or SWAP_QUOTE_PATH)\n" +
		"      SWAP_QUOTE_TIMEOUT_MS_FOO=15000 (optional, alias timeout in ms)\n" +
		"      SWAP_QUOTE_TIMEOUT_MS_BAR=15000 (optional, alias timeout in ms)\n" +
		"      SWAP_QUOTE_TIMEOUT_MS=15000 (optional global external timeout in ms)\n" +
		"      SWAP_QUOTE_CIRCUIT_FAILS_FOO=3 (optional, consecutive fails before open)\n" +
		"      SWAP_QUOTE_CIRCUIT_COOLDOWN_MS_FOO=30000 (optional, open duration ms)\n" +
		"      SWAP_QUOTE_CIRCUIT_FAILS=3 (optional global default)\n" +
		"      SWAP_QUOTE_CIRCUIT_COOLDOWN_MS=30000 (optional global default)\n" +
		"  SWAP_0X_API_KEY=<your-0x-api-key>\n" +
		"  SWAP_0X_CHAIN_ID=1|10|137|42161|56|8453|43114 (default is 1)\n" +
		"    1=Ethereum  10=Optimism  137=Polygon  42161=Arbitrum\n" +
		"    56=BSC  8453=Base  43114=Avalanche\n" +
		"  SWAP_0X_TAKER=0x0000000000000000000000000000000000010000 (default valid Permit2 taker)\n" +
		"Optional ParaSwap provider environment variables:\n" +
		"  SWAP_PARASWAP_CHAIN_ID=1|10|137|42161|56|8453|43114 (default is 1)\n" +
		"Request body fields (override per-request):\n" +
		"  chain_id — chain override for /v1/swaprate when using 0x.\n" +
		"  taker   — Permit2 taker address override.\n" +
		"/v1/coins dynamic source variables:\n" +
		"  SWAP_COINS_SOURCE=coingecko|static (default coingecko)\n" +
		"  SWAP_COINS_LIMIT=1..250 (default 100)\n" +
		"  SWAP_COINS_CACHE_TTL=<duration> (default 10m)\n" +
		"Supported tokens: ETH, USDC, DAI, WBTC, USDT, LINK, UNI, AAVE, MATIC/POL, OP, ARB, SHIB, CRV (chain-specific mapping)"))
}

func healthzHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ok"))
}

// providerHealthHandler probes each active provider with a lightweight quote request
// and returns per-provider status (ok/error/unknown) plus latency.
func providerHealthHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()

	type providerStatus struct {
		Status    string `json:"status"`
		Error     string `json:"error,omitempty"`
		LatencyMs int64  `json:"latency_ms,omitempty"`
	}

	result := make(map[string]providerStatus)
	okCount := 0
	total := 0

	// Resolve the current active providers
	providers := resolveActiveProviders()

	var mu sync.Mutex
	var wg sync.WaitGroup
	probeReq := SwapRateRequest{
		TickerFrom: "ETH",
		TickerTo:   "USDC",
		AmountFrom: "0.000001",
	}

	for _, np := range providers {
		np := np
		total++
		wg.Add(1)
		go func() {
			defer wg.Done()
			start := time.Now()
			_, _, err := np.provider.GetQuotes(ctx, probeReq)
			elapsed := time.Since(start).Milliseconds()

			mu.Lock()
			defer mu.Unlock()

			ps := providerStatus{}
			if err != nil {
				if isCancellationErr(err) {
					ps.Status = "timeout"
					ps.Error = firstLine(err.Error(), 200)
				} else {
					ps.Status = "error"
					ps.Error = firstLine(err.Error(), 200)
				}
			} else {
				ps.Status = "ok"
				ps.LatencyMs = elapsed
				okCount++
			}
			result[np.name] = ps
		}()
	}

	wg.Wait()

	// Fill "unknown" for any missing names
	for _, np := range providers {
		if _, exists := result[np.name]; !exists {
			result[np.name] = providerStatus{Status: "unknown", Error: "no response received"}
		}
	}

	out := map[string]any{
		"providers": result,
		"ok":        okCount,
		"total":     total,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

// resolveActiveProviders returns the currently configured named providers.
func resolveActiveProviders() []namedQuoteProvider {
	switch p := quoteProvider.(type) {
	case *MultiQuoteProvider:
		out := make([]namedQuoteProvider, len(p.providers))
		copy(out, p.providers)
		return out
	default:
		raw := strings.TrimSpace(os.Getenv("SWAP_QUOTE_PROVIDER"))
		names := parseProviderList(raw)
		for _, name := range names {
			return []namedQuoteProvider{{name: name, provider: p}}
		}
		return nil
	}
}

func firstLine(s string, maxLen int) string {
	if idx := strings.IndexAny(s, "\r\n"); idx >= 0 {
		s = s[:idx]
	}
	s = strings.TrimSpace(s)
	if len(s) > maxLen {
		return s[:maxLen-3] + "..."
	}
	return s
}

func coinsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	items, err := getCoins(r.Context())
	if err != nil {
		log.Printf("coins: dynamic source failed, fallback to static: %v", err)
		items = cloneCoins(coins)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"coins": items})
}

func getCoins(ctx context.Context) ([]Coin, error) {
	source := strings.ToLower(strings.TrimSpace(os.Getenv("SWAP_COINS_SOURCE")))
	if source == "" {
		source = "coingecko"
	}

	if source == "static" {
		return filterCoinsForActiveProviders(cloneCoins(coins)), nil
	}

	now := time.Now()
	coinsCache.RLock()
	if len(coinsCache.data) > 0 && now.Before(coinsCache.expiresAt) {
		cached := cloneCoins(coinsCache.data)
		coinsCache.RUnlock()
		return cached, nil
	}
	coinsCache.RUnlock()

	fresh, err := fetchCoinsFromCoinGecko(ctx)
	if err != nil {
		coinsCache.RLock()
		stale := cloneCoins(coinsCache.data)
		coinsCache.RUnlock()
		if len(stale) > 0 {
			return stale, nil
		}
		return nil, err
	}

	ttl := 10 * time.Minute
	if v := strings.TrimSpace(os.Getenv("SWAP_COINS_CACHE_TTL")); v != "" {
		if d, derr := time.ParseDuration(v); derr == nil && d > 0 {
			ttl = d
		}
	}

	coinsCache.Lock()
	coinsCache.data = cloneCoins(fresh)
	coinsCache.expiresAt = now.Add(ttl)
	coinsCache.Unlock()

	return filterCoinsForActiveProviders(fresh), nil
}

func fetchCoinsFromCoinGecko(ctx context.Context) ([]Coin, error) {
	limit := 100
	if raw := strings.TrimSpace(os.Getenv("SWAP_COINS_LIMIT")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil {
			if n < 1 {
				n = 1
			}
			if n > 250 {
				n = 250
			}
			limit = n
		}
	}

	u := fmt.Sprintf("https://api.coingecko.com/api/v3/coins/markets?vs_currency=usd&order=market_cap_desc&per_page=%d&page=1&sparkline=false", limit)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", quoteUserAgent)

	resp, err := coinsClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("coingecko error %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}

	var payload []struct {
		Symbol string `json:"symbol"`
		Name   string `json:"name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	if len(payload) == 0 {
		return nil, fmt.Errorf("coingecko returned empty coin list")
	}

	out := make([]Coin, 0, len(payload))
	seen := make(map[string]struct{}, len(payload))
	for _, it := range payload {
		ticker := strings.ToLower(strings.TrimSpace(it.Symbol))
		name := strings.TrimSpace(it.Name)
		if ticker == "" || name == "" {
			continue
		}
		if _, ok := seen[ticker]; ok {
			continue
		}
		if !isTickerSupportedByActiveProviders(ticker) {
			continue
		}
		seen[ticker] = struct{}{}
		for _, c := range expandCoinAcrossSupportedNetworks(name, ticker) {
			out = append(out, c)
		}
	}

	if len(out) == 0 {
		return nil, fmt.Errorf("coingecko returned no usable coins")
	}

	return out, nil
}

func expandCoinAcrossSupportedNetworks(name, ticker string) []Coin {
	ticker = strings.ToLower(strings.TrimSpace(ticker))
	name = strings.TrimSpace(name)
	if ticker == "" || name == "" {
		return nil
	}

	networks := supportedNetworksForTickerByActiveProviders(ticker)
	if len(networks) == 0 {
		return nil
	}

	min, max := defaultLimitsForTicker(ticker)
	out := make([]Coin, 0, len(networks))
	for _, network := range networks {
		displayName := name
		if !strings.EqualFold(network, "Mainnet") {
			displayName = fmt.Sprintf("%s (%s)", name, network)
		}
		out = append(out, Coin{
			Name:    displayName,
			Ticker:  ticker,
			Network: network,
			Minimum: min,
			Maximum: max,
		})
	}
	return out
}

func defaultLimitsForTicker(ticker string) (float64, float64) {
	switch strings.ToUpper(strings.TrimSpace(ticker)) {
	case "BTC", "WBTC":
		return 0.00001, 10000
	case "ETH":
		return 0.001, 50000
	case "USDC", "USDT", "DAI":
		return 1, 1000000
	case "LINK":
		return 0.1, 100000
	case "UNI":
		return 0.1, 500000
	case "AAVE":
		return 0.01, 10000
	case "MATIC", "POL":
		return 1, 5000000
	case "OP":
		return 1, 1000000
	case "ARB":
		return 1, 5000000
	case "SHIB":
		return 1000000, 100000000000
	case "CRV":
		return 1, 10000000
	default:
		return 0.00001, 100000
	}
}

func supportedNetworksForTickerByActiveProviders(ticker string) []string {
	normalized := normalizeQuoteSymbol(ticker)
	if normalized == "" {
		return nil
	}

	names := parseProviderList(strings.TrimSpace(os.Getenv("SWAP_QUOTE_PROVIDER")))
	if len(names) == 0 {
		return []string{"Mainnet"}
	}

	type networkChain struct {
		network string
		chainID string
	}
	candidates := []networkChain{
		{network: "Mainnet", chainID: "1"},
		{network: "Optimism", chainID: "10"},
		{network: "Polygon", chainID: "137"},
		{network: "Arbitrum One", chainID: "42161"},
		{network: "BEP20", chainID: "56"},
		{network: "Base", chainID: "8453"},
		{network: "Avalanche", chainID: "43114"},
	}

	out := make([]string, 0, len(candidates))
	seen := make(map[string]struct{}, len(candidates))
	add := func(network string) {
		if _, ok := seen[network]; ok {
			return
		}
		seen[network] = struct{}{}
		out = append(out, network)
	}

	for _, name := range names {
		switch name {
		case "0x", "zerox", "0x_public", "0x-public":
			for _, c := range candidates {
				if _, err := zeroXTokenAddress(c.chainID, normalized); err == nil {
					add(c.network)
				}
			}
		case "1inch", "oneinch", "1inch_public", "1inch-public":
			for _, c := range candidates {
				if _, err := oneInchTokenAddress(c.chainID, normalized); err == nil {
					add(c.network)
				}
			}
		case "paraswap", "paraswap_public", "paraswap-public", "para", "ps":
			for _, c := range candidates {
				if _, err := paraSwapTokenAddress(c.chainID, normalized); err == nil {
					add(c.network)
				}
			}
		case "openocean", "open-ocean", "oo":
			for _, c := range candidates {
				if _, err := zeroXTokenAddress(c.chainID, normalized); err == nil {
					add(c.network)
				}
			}
		case "odos", "odos_public", "odos-public":
			for _, c := range candidates {
				if _, err := zeroXTokenAddress(c.chainID, normalized); err == nil {
					add(c.network)
				}
			}
		case "mock", "none", "external", "generic", "simple":
			add("Mainnet")
		}
	}

	return out
}

func cloneCoins(src []Coin) []Coin {
	out := make([]Coin, len(src))
	copy(out, src)
	return out
}

func filterCoinsForActiveProviders(src []Coin) []Coin {
	out := make([]Coin, 0, len(src))
	for _, c := range src {
		if isCoinSupportedByActiveProviders(c) {
			out = append(out, c)
		}
	}
	if len(out) == 0 {
		// Never return an empty list; fallback to original source list.
		return src
	}
	return out
}

func isCoinSupportedByActiveProviders(c Coin) bool {
	ticker := strings.ToUpper(strings.TrimSpace(c.Ticker))
	if ticker == "" {
		return false
	}
	networkRaw := strings.TrimSpace(c.Network)
	mappedChain, hasMappedChain := chainIDFromNetwork(networkRaw)

	names := parseProviderList(strings.TrimSpace(os.Getenv("SWAP_QUOTE_PROVIDER")))
	if len(names) == 0 {
		return true
	}

	chain0x := strings.TrimSpace(os.Getenv("SWAP_0X_CHAIN_ID"))
	if chain0x == "" {
		chain0x = defaultZeroXChainID
	}
	chain1inch := strings.TrimSpace(os.Getenv("SWAP_1INCH_CHAIN_ID"))
	if chain1inch == "" {
		chain1inch = "1"
	}
	chainParaSwap := strings.TrimSpace(os.Getenv("SWAP_PARASWAP_CHAIN_ID"))
	if chainParaSwap == "" {
		chainParaSwap = "1"
	}
	chainOpenOcean := strings.TrimSpace(os.Getenv("SWAP_OPENOCEAN_CHAIN_ID"))
	if chainOpenOcean == "" {
		chainOpenOcean = "1"
	}
	chainOdos := strings.TrimSpace(os.Getenv("SWAP_ODOS_CHAIN_ID"))
	if chainOdos == "" {
		chainOdos = "1"
	}

	if hasMappedChain {
		chain0x = mappedChain
		chain1inch = mappedChain
		chainParaSwap = mappedChain
		chainOpenOcean = mappedChain
		chainOdos = mappedChain
	}

	normalized := normalizeQuoteSymbol(ticker)
	for _, name := range names {
		switch name {
		case "0x", "zerox", "0x_public", "0x-public":
			if networkRaw != "" && !hasMappedChain {
				continue
			}
			if _, err := zeroXTokenAddress(chain0x, normalized); err == nil {
				return true
			}
		case "1inch", "oneinch", "1inch_public", "1inch-public":
			if networkRaw != "" && !hasMappedChain {
				continue
			}
			if _, err := oneInchTokenAddress(chain1inch, normalized); err == nil {
				return true
			}
		case "paraswap", "paraswap_public", "paraswap-public", "para", "ps":
			if networkRaw != "" && !hasMappedChain {
				continue
			}
			if _, err := paraSwapTokenAddress(chainParaSwap, normalized); err == nil {
				return true
			}
		case "openocean", "open-ocean", "oo":
			if networkRaw != "" && !hasMappedChain {
				continue
			}
			if _, err := zeroXTokenAddress(chainOpenOcean, normalized); err == nil {
				return true
			}
		case "odos", "odos_public", "odos-public":
			if networkRaw != "" && !hasMappedChain {
				continue
			}
			if _, err := zeroXTokenAddress(chainOdos, normalized); err == nil {
				return true
			}
		case "mock", "none", "external", "generic", "simple":
			return true
		}
	}

	return false
}

func isTickerSupportedByActiveProviders(ticker string) bool {
	return isCoinSupportedByActiveProviders(Coin{Ticker: ticker})
}

func normalizeQuoteSymbol(symbol string) string {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	if symbol == "BTC" {
		return "WBTC"
	}
	if symbol == "POL" {
		return "MATIC"
	}
	return symbol
}

func chainIDFromNetwork(network string) (string, bool) {
	n := strings.ToLower(strings.TrimSpace(network))
	if n == "" {
		return "", false
	}
	switch n {
	case "mainnet", "ethereum", "erc20":
		return "1", true
	case "optimism", "op":
		return "10", true
	case "polygon", "matic":
		return "137", true
	case "arbitrum", "arbitrum one":
		return "42161", true
	case "bep20", "bsc", "binance smart chain":
		return "56", true
	case "base":
		return "8453", true
	case "avalanche", "avalanche c-chain", "avaxc":
		return "43114", true
	default:
		return "", false
	}
}

func supportedOnchainNetworksHint() string {
	return "Mainnet/ERC20, Optimism, Polygon/MATIC, Arbitrum One, BEP20/BSC, Base, Avalanche"
}

func resolveChainID(req SwapRateRequest, fallback string) string {
	if req.ChainID != "" {
		return req.ChainID
	}
	if chainID, ok := chainIDFromNetwork(req.NetworkFrom); ok {
		return chainID
	}
	if chainID, ok := chainIDFromNetwork(req.NetworkTo); ok {
		return chainID
	}
	return fallback
}

func resolveOnchainChainID(req SwapRateRequest, fallback string) (string, error) {
	if req.ChainID != "" {
		return req.ChainID, nil
	}
	if v := strings.TrimSpace(req.NetworkFrom); v != "" {
		if chainID, ok := chainIDFromNetwork(v); ok {
			return chainID, nil
		}
		return "", fmt.Errorf("unsupported network_from %q; supported networks: %s; or provide chain_id explicitly", req.NetworkFrom, supportedOnchainNetworksHint())
	}
	if v := strings.TrimSpace(req.NetworkTo); v != "" {
		if chainID, ok := chainIDFromNetwork(v); ok {
			return chainID, nil
		}
		return "", fmt.Errorf("unsupported network_to %q; supported networks: %s; or provide chain_id explicitly", req.NetworkTo, supportedOnchainNetworksHint())
	}
	return fallback, nil
}

func swapRateHandler(w http.ResponseWriter, r *http.Request) {
	reqID := newRequestID()
	w.Header().Set("X-Request-Id", reqID)
	ctx := withRequestID(r.Context(), reqID)

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
			http.Error(w, fmt.Sprintf("unsupported chain_id; supported values are %s", strings.Join(supportedZeroXChainsList(), ", ")), http.StatusBadRequest)
			return
		}
	}
	if _, err := parsePositiveAmount(req.AmountFrom); err != nil {
		http.Error(w, "amount_from must be a positive number", http.StatusBadRequest)
		return
	}

	quotes, warnings, err := quoteProvider.GetQuotes(ctx, req)
	if err != nil {
		if isCancellationErr(err) || isCancellationErr(ctx.Err()) {
			if isDebugEnabled() {
				log.Printf("swaprate: request_id=%s request canceled: %s err=%v ctx=%v", reqID, quoteRequestDebugFields(req), err, ctx.Err())
			}
			return
		}
		log.Printf("swaprate: request_id=%s provider error: %v", reqID, err)
		http.Error(w, fmt.Sprintf("failed to retrieve quote: %v", err), http.StatusBadGateway)
		return
	}

	out := SwapRateResponse{
		TickerFrom: req.TickerFrom,
		TickerTo:   req.TickerTo,
		Quotes:     quotes,
		Warnings:   warnings,
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

func newRequestID() string {
	buf := make([]byte, 6)
	_, _ = rand.Read(buf)
	return fmt.Sprintf("RQ-%s", hex.EncodeToString(buf))
}

func withRequestID(ctx context.Context, reqID string) context.Context {
	if strings.TrimSpace(reqID) == "" {
		return ctx
	}
	return context.WithValue(ctx, requestIDContextKey{}, reqID)
}

func requestIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	v := ctx.Value(requestIDContextKey{})
	id, _ := v.(string)
	return strings.TrimSpace(id)
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
	raw := strings.TrimSpace(os.Getenv("SWAP_QUOTE_PROVIDER"))
	names := parseProviderList(raw)
	if len(names) == 0 {
		return MockQuoteProvider{}
	}

	resolved := make([]namedQuoteProvider, 0, len(names))
	for _, name := range names {
		p, ok := buildProviderByName(name)
		if !ok {
			log.Printf("provider entry %q in SWAP_QUOTE_PROVIDER=%q could not be resolved; skipping", name, raw)
			continue
		}
		resolved = append(resolved, namedQuoteProvider{name: name, provider: p})
	}

	if len(resolved) == 0 {
		log.Printf("no valid provider in SWAP_QUOTE_PROVIDER=%q; using mock provider", raw)
		return MockQuoteProvider{}
	}
	if len(resolved) == 1 {
		return resolved[0].provider
	}

	log.Printf("using multi provider aggregation: %v", names)
	return &MultiQuoteProvider{providers: resolved}
}

func parseProviderList(raw string) []string {
	raw = strings.ToLower(strings.TrimSpace(raw))
	if raw == "" || raw == "none" || raw == "mock" {
		return []string{"mock"}
	}
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		switch r {
		case ',', '+', ';', ' ':
			return true
		default:
			return false
		}
	})
	if len(parts) == 0 {
		return nil
	}

	out := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	return out
}

func buildProviderByName(name string) (QuoteProvider, bool) {
	if strings.HasPrefix(name, "external:") {
		alias := strings.TrimSpace(strings.TrimPrefix(name, "external:"))
		return newNamedExternalQuoteProvider(alias)
	}

	switch name {
	case "1inch", "oneinch", "1inch_public", "1inch-public":
		return newOneInchQuoteProvider(), true
	case "openocean", "open-ocean", "oo":
		return newOpenOceanQuoteProvider(), true
	case "odos", "odos_public", "odos-public":
		return newOdosQuoteProvider(), true
	case "paraswap", "paraswap_public", "paraswap-public", "para", "ps":
		return newParaSwapQuoteProvider(), true
	case "0x", "zerox", "0x_public", "0x-public":
		return newZeroXQuoteProvider(), true
	case "external", "generic", "simple":
		return newExternalQuoteProvider(), true
	case "mock", "none":
		return MockQuoteProvider{}, true
	default:
		return nil, false
	}
}

func externalAliasEnvSuffix(alias string) (string, bool) {
	alias = strings.TrimSpace(alias)
	if alias == "" {
		return "", false
	}

	b := strings.Builder{}
	b.Grow(len(alias))
	for _, r := range alias {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r - ('a' - 'A'))
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '_' || r == '-':
			b.WriteRune('_')
		}
	}

	suffix := strings.Trim(strings.ReplaceAll(b.String(), "__", "_"), "_")
	if suffix == "" {
		return "", false
	}
	return suffix, true
}

func newNamedExternalQuoteProvider(alias string) (QuoteProvider, bool) {
	suffix, ok := externalAliasEnvSuffix(alias)
	if !ok {
		log.Printf("invalid external provider alias %q", alias)
		return nil, false
	}

	urlKey := "SWAP_QUOTE_URL_" + suffix
	apiKeyKey := "SWAP_QUOTE_API_KEY_" + suffix
	pathKey := "SWAP_QUOTE_PATH_" + suffix
	timeoutKey := "SWAP_QUOTE_TIMEOUT_MS_" + suffix

	quoteURL := strings.TrimSpace(os.Getenv(urlKey))
	if quoteURL == "" {
		log.Printf("%s not set for external provider alias %q", urlKey, alias)
		return nil, false
	}
	if _, err := url.ParseRequestURI(quoteURL); err != nil {
		log.Printf("invalid %s %q for external provider alias %q: %v", urlKey, quoteURL, alias, err)
		return nil, false
	}

	apiKey := strings.TrimSpace(os.Getenv(apiKeyKey))
	quotePath := strings.TrimSpace(os.Getenv(pathKey))
	if quotePath == "" {
		quotePath = strings.TrimSpace(os.Getenv("SWAP_QUOTE_PATH"))
	}
	quotePath = normalizeExternalQuotePath(quotePath)
	timeout := externalHTTPTimeout(timeoutKey)
	cbFails, cbTTL := externalCircuitConfig(suffix)

	return &ExternalQuoteProvider{
		client:  &http.Client{Timeout: timeout},
		baseURL: quoteURL,
		apiKey:  apiKey,
		path:    quotePath,
		cKey:    "external:" + strings.TrimSpace(alias),
		cbFails: cbFails,
		cbTTL:   cbTTL,
	}, true
}

func newExternalQuoteProvider() QuoteProvider {
	quoteURL := strings.TrimSpace(os.Getenv("SWAP_QUOTE_URL"))
	if quoteURL == "" {
		log.Printf("SWAP_QUOTE_URL not set; using mock provider")
		return MockQuoteProvider{}
	}

	if _, err := url.ParseRequestURI(quoteURL); err != nil {
		log.Printf("invalid SWAP_QUOTE_URL %q: %v; using mock provider", quoteURL, err)
		return MockQuoteProvider{}
	}

	apiKey := strings.TrimSpace(os.Getenv("SWAP_QUOTE_API_KEY"))
	quotePath := normalizeExternalQuotePath(strings.TrimSpace(os.Getenv("SWAP_QUOTE_PATH")))
	timeout := externalHTTPTimeout("")
	cbFails, cbTTL := externalCircuitConfig("")
	return &ExternalQuoteProvider{
		client:  &http.Client{Timeout: timeout},
		baseURL: quoteURL,
		apiKey:  apiKey,
		path:    quotePath,
		cKey:    "external:default",
		cbFails: cbFails,
		cbTTL:   cbTTL,
	}
}

func externalHTTPTimeout(aliasTimeoutKey string) time.Duration {
	const defaultTimeout = 15 * time.Second
	keys := make([]string, 0, 2)
	if strings.TrimSpace(aliasTimeoutKey) != "" {
		keys = append(keys, aliasTimeoutKey)
	}
	keys = append(keys, "SWAP_QUOTE_TIMEOUT_MS")

	for _, key := range keys {
		raw := strings.TrimSpace(os.Getenv(key))
		if raw == "" {
			continue
		}
		ms, err := strconv.Atoi(raw)
		if err != nil || ms <= 0 {
			log.Printf("invalid %s=%q; expected positive integer milliseconds", key, raw)
			continue
		}
		return time.Duration(ms) * time.Millisecond
	}

	return defaultTimeout
}

func externalCircuitConfig(aliasSuffix string) (int, time.Duration) {
	const defaultFails = 3
	const defaultCooldown = 30 * time.Second

	failsKeys := make([]string, 0, 2)
	cooldownKeys := make([]string, 0, 2)
	if strings.TrimSpace(aliasSuffix) != "" {
		failsKeys = append(failsKeys, "SWAP_QUOTE_CIRCUIT_FAILS_"+aliasSuffix)
		cooldownKeys = append(cooldownKeys, "SWAP_QUOTE_CIRCUIT_COOLDOWN_MS_"+aliasSuffix)
	}
	failsKeys = append(failsKeys, "SWAP_QUOTE_CIRCUIT_FAILS")
	cooldownKeys = append(cooldownKeys, "SWAP_QUOTE_CIRCUIT_COOLDOWN_MS")

	fails := firstPositiveIntFromEnv(failsKeys, defaultFails)
	cooldownMS := firstPositiveIntFromEnv(cooldownKeys, int(defaultCooldown/time.Millisecond))
	return fails, time.Duration(cooldownMS) * time.Millisecond
}

func firstPositiveIntFromEnv(keys []string, fallback int) int {
	for _, key := range keys {
		raw := strings.TrimSpace(os.Getenv(key))
		if raw == "" {
			continue
		}
		v, err := strconv.Atoi(raw)
		if err != nil || v <= 0 {
			log.Printf("invalid %s=%q; expected positive integer", key, raw)
			continue
		}
		return v
	}
	return fallback
}

func (p *ExternalQuoteProvider) circuitKey() string {
	if strings.TrimSpace(p.cKey) != "" {
		return p.cKey
	}
	return "external:" + strings.TrimSpace(p.baseURL) + "|" + normalizeExternalQuotePath(p.path)
}

func externalCircuitBeforeAttempt(key string, now time.Time) error {
	if strings.TrimSpace(key) == "" {
		return nil
	}

	externalCircuit.Lock()
	defer externalCircuit.Unlock()

	s, ok := externalCircuit.state[key]
	if !ok {
		return nil
	}
	if now.Before(s.openUntil) {
		remaining := s.openUntil.Sub(now).Round(time.Second)
		return fmt.Errorf("external provider circuit open for %s (%s remaining)", key, remaining)
	}

	if !s.openUntil.IsZero() {
		if s.halfOpenInFlight {
			return fmt.Errorf("external provider circuit open for %s (half-open probe in progress)", key)
		}
		// Cooldown has elapsed; allow exactly one probe call.
		s.halfOpenInFlight = true
		externalCircuit.state[key] = s
		return nil
	}

	if s.halfOpenInFlight {
		return fmt.Errorf("external provider circuit open for %s (half-open probe in progress)", key)
	}
	return nil
}

func externalCircuitRecordFailure(key string, threshold int, cooldown time.Duration, now time.Time) {
	if strings.TrimSpace(key) == "" || threshold <= 0 || cooldown <= 0 {
		return
	}

	externalCircuit.Lock()
	defer externalCircuit.Unlock()

	s := externalCircuit.state[key]
	if s.halfOpenInFlight {
		s.halfOpenInFlight = false
		if s.fails < threshold {
			s.fails = threshold
		}
		s.openUntil = now.Add(cooldown)
		externalCircuit.state[key] = s
		return
	}

	if !s.openUntil.IsZero() && now.After(s.openUntil) {
		s.fails = 0
		s.openUntil = time.Time{}
		s.halfOpenInFlight = false
	}

	s.fails++
	if s.fails >= threshold {
		s.openUntil = now.Add(cooldown)
		s.halfOpenInFlight = false
	}
	externalCircuit.state[key] = s
}

func externalCircuitRecordSuccess(key string) {
	if strings.TrimSpace(key) == "" {
		return
	}
	externalCircuit.Lock()
	defer externalCircuit.Unlock()
	delete(externalCircuit.state, key)
}

func normalizeExternalQuotePath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return "/quote"
	}
	if strings.HasPrefix(path, "/") {
		return path
	}
	return "/" + path
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

func newOpenOceanQuoteProvider() QuoteProvider {
	quoteURL := strings.TrimSpace(os.Getenv("SWAP_OPENOCEAN_URL"))
	if quoteURL == "" {
		quoteURL = "https://open-api.openocean.finance/v4"
	}
	chainID := strings.TrimSpace(os.Getenv("SWAP_OPENOCEAN_CHAIN_ID"))
	if chainID == "" {
		chainID = "1"
	}
	apiKey := strings.TrimSpace(os.Getenv("SWAP_OPENOCEAN_API_KEY"))
	return &OpenOceanQuoteProvider{
		client:  &http.Client{Timeout: openOceanHTTPTimeout()},
		baseURL: quoteURL,
		chainID: chainID,
		apiKey:  apiKey,
	}
}

func newOdosQuoteProvider() QuoteProvider {
	quoteURL := strings.TrimSpace(os.Getenv("SWAP_ODOS_URL"))
	if quoteURL == "" {
		quoteURL = "https://api.odos.xyz"
	}
	chainID := strings.TrimSpace(os.Getenv("SWAP_ODOS_CHAIN_ID"))
	if chainID == "" {
		chainID = "1"
	}
	apiKey := strings.TrimSpace(os.Getenv("SWAP_ODOS_API_KEY"))
	// Use IPv4-only dialer to avoid IPv6 connectivity issues (e.g. api.odos.xyz
	// has broken IPv6 routes on some networks).
	ipv4Transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			d := net.Dialer{Timeout: 30 * time.Second}
			return d.DialContext(ctx, "tcp4", addr)
		},
	}
	return &OdosQuoteProvider{
		client:  &http.Client{Timeout: odosHTTPTimeout(), Transport: ipv4Transport},
		baseURL: quoteURL,
		chainID: chainID,
		apiKey:  apiKey,
	}
}

func newParaSwapQuoteProvider() QuoteProvider {
	quoteURL := strings.TrimSpace(os.Getenv("SWAP_PARASWAP_URL"))
	if quoteURL == "" {
		quoteURL = "https://api.paraswap.io"
	}
	return &ParaSwapQuoteProvider{
		client:  &http.Client{Timeout: 15 * time.Second},
		baseURL: quoteURL,
	}
}

func openOceanHTTPTimeout() time.Duration {
	const defaultTimeout = 15 * time.Second
	raw := strings.TrimSpace(os.Getenv("SWAP_OPENOCEAN_TIMEOUT_MS"))
	if raw == "" {
		return defaultTimeout
	}
	ms, err := strconv.Atoi(raw)
	if err != nil || ms <= 0 {
		log.Printf("invalid SWAP_OPENOCEAN_TIMEOUT_MS=%q; expected positive integer milliseconds", raw)
		return defaultTimeout
	}
	return time.Duration(ms) * time.Millisecond
}

func openOceanChainSlug(chainID string) (string, error) {
	switch strings.TrimSpace(chainID) {
	case "1":
		return "eth", nil
	case "10":
		return "optimism", nil
	case "137":
		return "polygon", nil
	case "42161":
		return "arbitrum", nil
	case "56":
		return "bsc", nil
	case "8453":
		return "base", nil
	case "43114":
		return "avax", nil
	default:
		return "", fmt.Errorf("unsupported chain id %s", chainID)
	}
}

func odosHTTPTimeout() time.Duration {
	const defaultTimeout = 15 * time.Second
	raw := strings.TrimSpace(os.Getenv("SWAP_ODOS_TIMEOUT_MS"))
	if raw == "" {
		return defaultTimeout
	}
	ms, err := strconv.Atoi(raw)
	if err != nil || ms <= 0 {
		log.Printf("invalid SWAP_ODOS_TIMEOUT_MS=%q; expected positive integer milliseconds", raw)
		return defaultTimeout
	}
	return time.Duration(ms) * time.Millisecond
}

func odosChainID(chainID string) (int, error) {
	id, err := strconv.Atoi(strings.TrimSpace(chainID))
	if err != nil || id < 0 {
		return 0, fmt.Errorf("invalid chain id for odos: %s", chainID)
	}
	return id, nil
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

func (MockQuoteProvider) GetQuotes(ctx context.Context, req SwapRateRequest) ([]Quote, []string, error) {
	return []Quote{buildQuote(req)}, nil, nil
}

func (p *MultiQuoteProvider) GetQuotes(ctx context.Context, req SwapRateRequest) ([]Quote, []string, error) {
	type result struct {
		provider string
		quotes   []Quote
		warnings []string
		err      error
	}

	results := make(chan result, len(p.providers))
	var wg sync.WaitGroup
	for _, np := range p.providers {
		np := np
		wg.Add(1)
		go func() {
			defer wg.Done()
			quotes, warnings, err := np.provider.GetQuotes(ctx, req)
			results <- result{provider: np.name, quotes: quotes, warnings: warnings, err: err}
		}()
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	agg := make([]Quote, 0)
	warnings := make([]string, 0)
	errMsgs := make([]string, 0)
	reqID := requestIDFromContext(ctx)
	for r := range results {
		if len(r.warnings) > 0 {
			warnings = append(warnings, r.warnings...)
		}
		if r.err != nil {
			if isCancellationErr(r.err) {
				if isDebugEnabled() {
					log.Printf("multi provider canceled branch: request_id=%s provider=%s %s err=%v", reqID, r.provider, quoteRequestDebugFields(req), r.err)
				}
				continue
			}
			errMsgs = append(errMsgs, fmt.Sprintf("%s: %v", r.provider, r.err))
			warnings = append(warnings, fmt.Sprintf("%s unavailable: %v", r.provider, r.err))
			continue
		}
		agg = append(agg, r.quotes...)
	}

	if len(agg) == 0 {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, warnings, ctxErr
		}
		if len(errMsgs) == 0 {
			return nil, warnings, fmt.Errorf("all quote providers returned no quotes")
		}
		return nil, warnings, fmt.Errorf("all quote providers failed: %s", strings.Join(errMsgs, " | "))
	}

	sortQuotesByBestAmount(agg)
	if len(errMsgs) > 0 {
		log.Printf("multi provider partial failures: request_id=%s %s", reqID, strings.Join(errMsgs, " | "))
	}
	if len(warnings) > 0 {
		warnings = uniqueStrings(warnings)
	}
	return agg, warnings, nil
}

func isCancellationErr(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

func isDebugEnabled() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("SWAP_DEBUG")))
	switch v {
	case "1", "true", "yes", "on", "y":
		return true
	default:
		return false
	}
}

func quoteRequestDebugFields(req SwapRateRequest) string {
	return fmt.Sprintf(
		"from=%s to=%s amount_from=%s chain_id=%s network_from=%s network_to=%s",
		strings.TrimSpace(req.TickerFrom),
		strings.TrimSpace(req.TickerTo),
		strings.TrimSpace(req.AmountFrom),
		strings.TrimSpace(req.ChainID),
		strings.TrimSpace(req.NetworkFrom),
		strings.TrimSpace(req.NetworkTo),
	)
}

func sortQuotesByBestAmount(quotes []Quote) {
	sort.SliceStable(quotes, func(i, j int) bool {
		a, aok := new(big.Int).SetString(strings.TrimSpace(quotes[i].AmountTo), 10)
		b, bok := new(big.Int).SetString(strings.TrimSpace(quotes[j].AmountTo), 10)
		if aok && bok {
			return a.Cmp(b) > 0
		}
		if aok {
			return true
		}
		if bok {
			return false
		}
		// Fallback for non-integer/unknown formats.
		return strings.TrimSpace(quotes[i].AmountTo) > strings.TrimSpace(quotes[j].AmountTo)
	})
}

func (p *ExternalQuoteProvider) GetQuotes(ctx context.Context, req SwapRateRequest) ([]Quote, []string, error) {
	key := p.circuitKey()
	if err := externalCircuitBeforeAttempt(key, time.Now()); err != nil {
		return nil, nil, err
	}

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
		return nil, nil, err
	}

	path := normalizeExternalQuotePath(p.path)

	quoteURL, err := url.JoinPath(p.baseURL, strings.TrimPrefix(path, "/"))
	if err != nil {
		return nil, nil, err
	}

	reqHTTP, err := http.NewRequestWithContext(ctx, http.MethodPost, quoteURL, bytes.NewReader(body))
	if err != nil {
		externalCircuitRecordFailure(key, p.cbFails, p.cbTTL, time.Now())
		return nil, nil, err
	}
	reqHTTP.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		reqHTTP.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	resp, err := p.client.Do(reqHTTP)
	if err != nil {
		externalCircuitRecordFailure(key, p.cbFails, p.cbTTL, time.Now())
		return nil, nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		externalCircuitRecordFailure(key, p.cbFails, p.cbTTL, time.Now())
		return nil, nil, fmt.Errorf("quote provider error %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}

	var out SwapRateResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		externalCircuitRecordFailure(key, p.cbFails, p.cbTTL, time.Now())
		return nil, nil, err
	}
	externalCircuitRecordSuccess(key)
	return out.Quotes, out.Warnings, nil
}

func (p *OneInchQuoteProvider) GetQuotes(ctx context.Context, req SwapRateRequest) ([]Quote, []string, error) {
	if req.TickerFrom == "" || req.TickerTo == "" {
		return nil, nil, fmt.Errorf("ticker_from and ticker_to are required")
	}
	if req.AmountFrom == "" {
		return nil, nil, fmt.Errorf("1inch public API requires amount_from")
	}

	fromToken := normalizeQuoteSymbol(req.TickerFrom)
	toToken := normalizeQuoteSymbol(req.TickerTo)
	chainId := os.Getenv("SWAP_1INCH_CHAIN_ID")
	if chainId == "" {
		chainId = "1"
	}
	chainId, err := resolveOnchainChainID(req, chainId)
	if err != nil {
		return nil, nil, err
	}

	fromAddr, err := oneInchTokenAddress(chainId, fromToken)
	if err != nil {
		return nil, nil, fmt.Errorf("unsupported from token: %w", err)
	}
	toAddr, err := oneInchTokenAddress(chainId, toToken)
	if err != nil {
		return nil, nil, fmt.Errorf("unsupported to token: %w", err)
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
		return nil, nil, err
	}
	reqHTTP.Header.Set("User-Agent", quoteUserAgent)

	resp, err := p.client.Do(reqHTTP)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, err
	}
	contentType := strings.TrimSpace(resp.Header.Get("Content-Type"))
	bodySnippet := strings.TrimSpace(string(body))
	if len(bodySnippet) > 160 {
		bodySnippet = bodySnippet[:160] + "..."
	}

	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("1inch quote error status=%d content_type=%q body=%s", resp.StatusCode, contentType, bodySnippet)
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
	if err := json.Unmarshal(body, &out); err != nil {
		trimmed := strings.TrimSpace(string(body))
		if strings.HasPrefix(trimmed, "<") {
			snippet := trimmed
			if len(snippet) > 120 {
				snippet = snippet[:120] + "..."
			}
			return nil, nil, fmt.Errorf("1inch non-json response status=%d content_type=%q (possible endpoint issue or upstream block): %s", resp.StatusCode, contentType, snippet)
		}
		return nil, nil, fmt.Errorf("1inch non-json response status=%d content_type=%q parse_error=%v body=%s", resp.StatusCode, contentType, err, bodySnippet)
	}

	amountToUSD, amountFromUSD, usdWarnings := computeQuoteUSDAmounts(ctx, fromToken, toToken, out.FromTokenAmount, out.ToTokenAmount)

	quote := Quote{
		Provider:      "1inch",
		AmountTo:      out.ToTokenAmount,
		AmountToUSD:   amountToUSD,
		AmountFrom:    out.FromTokenAmount,
		AmountFromUSD: amountFromUSD,
		Fixed:         "False",
		Kycrating:     "A",
		Eta:           1,
		Waste:         fmt.Sprintf("gas=%d", out.EstimatedGas),
	}
	return []Quote{quote}, usdWarnings, nil
}

func (p *OpenOceanQuoteProvider) GetQuotes(ctx context.Context, req SwapRateRequest) ([]Quote, []string, error) {
	if req.TickerFrom == "" || req.TickerTo == "" {
		return nil, nil, fmt.Errorf("ticker_from and ticker_to are required")
	}
	if req.AmountFrom == "" {
		return nil, nil, fmt.Errorf("openocean requires amount_from")
	}

	fromSym := normalizeQuoteSymbol(req.TickerFrom)
	toSym := normalizeQuoteSymbol(req.TickerTo)

	chainID := p.chainID
	resolvedChainID, err := resolveOnchainChainID(req, chainID)
	if err != nil {
		return nil, nil, err
	}
	chainID = resolvedChainID
	chainSlug, err := openOceanChainSlug(chainID)
	if err != nil {
		return nil, nil, err
	}

	fromAddr, err := zeroXTokenAddress(chainID, fromSym)
	if err != nil {
		return nil, nil, fmt.Errorf("unsupported from token: %w", err)
	}
	toAddr, err := zeroXTokenAddress(chainID, toSym)
	if err != nil {
		return nil, nil, fmt.Errorf("unsupported to token: %w", err)
	}

	fromDecimals := tokenDecimalsForSymbol(fromSym)
	amountBase, err := decimalToBaseUnits(req.AmountFrom, fromDecimals)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid amount_from: %w", err)
	}

	quoteURL := fmt.Sprintf("%s/%s/swap?inTokenAddress=%s&outTokenAddress=%s&amount=%s&gasPrice=30000000000&slippage=1",
		strings.TrimSuffix(p.baseURL, "/"),
		url.PathEscape(chainSlug),
		url.QueryEscape(fromAddr),
		url.QueryEscape(toAddr),
		url.QueryEscape(amountBase),
	)

	reqHTTP, err := http.NewRequestWithContext(ctx, http.MethodGet, quoteURL, nil)
	if err != nil {
		return nil, nil, err
	}
	reqHTTP.Header.Set("Accept", "application/json")
	reqHTTP.Header.Set("User-Agent", quoteUserAgent)
	if p.apiKey != "" {
		reqHTTP.Header.Set("API-Key", p.apiKey)
	}

	resp, err := p.client.Do(reqHTTP)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, err
	}

	if resp.StatusCode != http.StatusOK {
		data := strings.TrimSpace(string(body))
		if len(data) > 160 {
			data = data[:160] + "..."
		}
		return nil, nil, fmt.Errorf("openocean quote error status=%d content_type=%q body=%s", resp.StatusCode, strings.TrimSpace(resp.Header.Get("Content-Type")), data)
	}

	type openOceanPayload struct {
		InAmount       string `json:"inAmount"`
		OutAmount      string `json:"outAmount"`
		EstimatedGas   string `json:"estimatedGas"`
		InTokenAmount  string `json:"inTokenAmount"`
		OutTokenAmount string `json:"outTokenAmount"`
	}
	type openOceanEnvelope struct {
		Code    int             `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
		Result  json.RawMessage `json:"result"`
	}

	var envelope openOceanEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, nil, fmt.Errorf("openocean non-json response: %w", err)
	}
	if envelope.Code != 0 && envelope.Code != 200 {
		msg := strings.TrimSpace(envelope.Message)
		if msg == "" {
			msg = "openocean quote failed"
		}
		return nil, nil, fmt.Errorf("openocean quote error code=%d: %s", envelope.Code, msg)
	}

	decodePayload := func(raw json.RawMessage) (openOceanPayload, error) {
		var payload openOceanPayload
		if len(raw) == 0 {
			return payload, fmt.Errorf("empty openocean payload")
		}
		if err := json.Unmarshal(raw, &payload); err != nil {
			return payload, err
		}
		return payload, nil
	}

	payload, err := decodePayload(envelope.Data)
	if err != nil || strings.TrimSpace(payload.OutAmount) == "" {
		if len(envelope.Result) > 0 {
			payload, err = decodePayload(envelope.Result)
			if err != nil {
				return nil, nil, fmt.Errorf("openocean decode failed: %w", err)
			}
		}
	}
	if strings.TrimSpace(payload.OutAmount) == "" {
		if err := json.Unmarshal(body, &payload); err != nil {
			return nil, nil, fmt.Errorf("openocean response missing outAmount")
		}
	}

	amountToBase := strings.TrimSpace(payload.OutAmount)
	if amountToBase == "" {
		amountToBase = strings.TrimSpace(payload.OutTokenAmount)
	}
	amountFromBase := strings.TrimSpace(payload.InAmount)
	if amountFromBase == "" {
		amountFromBase = strings.TrimSpace(payload.InTokenAmount)
	}
	if amountFromBase == "" {
		amountFromBase = req.AmountFrom
	}
	if amountToBase == "" {
		return nil, nil, fmt.Errorf("openocean response missing outAmount")
	}

	amountToUSD, amountFromUSD, usdWarnings := computeQuoteUSDAmounts(ctx, fromSym, toSym, amountFromBase, amountToBase)

	quote := Quote{
		Provider:      "openocean",
		AmountTo:      amountToBase,
		AmountToUSD:   amountToUSD,
		AmountFrom:    amountFromBase,
		AmountFromUSD: amountFromUSD,
		Fixed:         "False",
		Kycrating:     "A",
		Eta:           1,
		Waste:         strings.TrimSpace(payload.EstimatedGas),
	}
	return []Quote{quote}, usdWarnings, nil
}

func (p *OdosQuoteProvider) GetQuotes(ctx context.Context, req SwapRateRequest) ([]Quote, []string, error) {
	if req.TickerFrom == "" || req.TickerTo == "" {
		return nil, nil, fmt.Errorf("ticker_from and ticker_to are required")
	}
	if req.AmountFrom == "" {
		return nil, nil, fmt.Errorf("odos requires amount_from")
	}

	fromSym := normalizeQuoteSymbol(req.TickerFrom)
	toSym := normalizeQuoteSymbol(req.TickerTo)

	chainID := p.chainID
	resolvedChainID, err := resolveOnchainChainID(req, chainID)
	if err != nil {
		return nil, nil, err
	}
	chainID = resolvedChainID

	chainNum, err := odosChainID(chainID)
	if err != nil {
		return nil, nil, err
	}

	fromAddr, err := zeroXTokenAddress(chainID, fromSym)
	if err != nil {
		return nil, nil, fmt.Errorf("unsupported from token: %w", err)
	}
	toAddr, err := zeroXTokenAddress(chainID, toSym)
	if err != nil {
		return nil, nil, fmt.Errorf("unsupported to token: %w", err)
	}

	fromDecimals := tokenDecimalsForSymbol(fromSym)
	amountBase, err := decimalToBaseUnits(req.AmountFrom, fromDecimals)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid amount_from: %w", err)
	}

	reqBody := map[string]interface{}{
		"chainId":              chainNum,
		"inputTokens":          []map[string]string{{"tokenAddress": fromAddr, "amount": amountBase}},
		"outputTokens":         []map[string]string{{"tokenAddress": toAddr}},
		"userAddr":             "0x0000000000000000000000000000000000000000",
		"slippageLimitPercent": 1,
		"referralCode":         0,
		"compact":              true,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, nil, fmt.Errorf("odos request marshal failed: %w", err)
	}

	quoteURL := fmt.Sprintf("%s/sor/quote/v3", strings.TrimSuffix(p.baseURL, "/"))
	reqHTTP, err := http.NewRequestWithContext(ctx, http.MethodPost, quoteURL, bytes.NewReader(body))
	if err != nil {
		return nil, nil, err
	}
	reqHTTP.Header.Set("Accept", "application/json")
	reqHTTP.Header.Set("Content-Type", "application/json")
	reqHTTP.Header.Set("User-Agent", quoteUserAgent)
	if p.apiKey != "" {
		reqHTTP.Header.Set("X-API-Key", p.apiKey)
	}

	resp, err := p.client.Do(reqHTTP)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, err
	}

	if resp.StatusCode != http.StatusOK {
		data := strings.TrimSpace(string(respBody))
		if len(data) > 160 {
			data = data[:160] + "..."
		}
		return nil, nil, fmt.Errorf("odos quote error status=%d content_type=%q body=%s", resp.StatusCode, strings.TrimSpace(resp.Header.Get("Content-Type")), data)
	}

	type odosQuoteResponse struct {
		InAmounts   []string `json:"inAmounts"`
		OutAmounts  []string `json:"outAmounts"`
		GasEstimate string   `json:"gasEstimate"`
		OutTokens   []string `json:"outTokens"`
	}
	type odosEnvelope struct {
		StatusCode  int               `json:"statusCode"`
		Description string            `json:"description"`
		Quote       odosQuoteResponse `json:"quote"`
	}

	var envelope odosEnvelope
	if err := json.Unmarshal(respBody, &envelope); err != nil {
		return nil, nil, fmt.Errorf("odos non-json response: %w", err)
	}

	if envelope.StatusCode != 200 {
		desc := strings.TrimSpace(envelope.Description)
		if desc == "" {
			desc = "odos quote failed"
		}
		return nil, nil, fmt.Errorf("odos quote error code=%d: %s", envelope.StatusCode, desc)
	}

	amountFromBase := ""
	amountToBase := ""

	if len(envelope.Quote.InAmounts) > 0 {
		amountFromBase = strings.TrimSpace(envelope.Quote.InAmounts[0])
	}
	if amountFromBase == "" {
		amountFromBase = req.AmountFrom
	}

	if len(envelope.Quote.OutAmounts) > 0 {
		amountToBase = strings.TrimSpace(envelope.Quote.OutAmounts[0])
	}
	if amountToBase == "" {
		return nil, nil, fmt.Errorf("odos response missing outAmount")
	}

	amountToUSD, amountFromUSD, usdWarnings := computeQuoteUSDAmounts(ctx, fromSym, toSym, amountFromBase, amountToBase)

	quote := Quote{
		Provider:      "odos",
		AmountTo:      amountToBase,
		AmountToUSD:   amountToUSD,
		AmountFrom:    amountFromBase,
		AmountFromUSD: amountFromUSD,
		Fixed:         "False",
		Kycrating:     "A",
		Eta:           1,
		Waste:         strings.TrimSpace(envelope.Quote.GasEstimate),
	}
	return []Quote{quote}, usdWarnings, nil
}

func (p *ParaSwapQuoteProvider) GetQuotes(ctx context.Context, req SwapRateRequest) ([]Quote, []string, error) {
	if req.TickerFrom == "" || req.TickerTo == "" {
		return nil, nil, fmt.Errorf("ticker_from and ticker_to are required")
	}
	if req.AmountFrom == "" {
		return nil, nil, fmt.Errorf("paraswap requires amount_from")
	}

	fromSym := normalizeQuoteSymbol(req.TickerFrom)
	toSym := normalizeQuoteSymbol(req.TickerTo)

	chainID := strings.TrimSpace(os.Getenv("SWAP_PARASWAP_CHAIN_ID"))
	if chainID == "" {
		chainID = "1"
	}
	resolvedChainID, err := resolveOnchainChainID(req, chainID)
	if err != nil {
		return nil, nil, err
	}
	chainID = resolvedChainID
	if _, ok := supportedZeroXChains[chainID]; !ok {
		return nil, nil, fmt.Errorf("unsupported chain id %q; supported values are %v", chainID, supportedZeroXChainsList())
	}

	fromAddr, err := paraSwapTokenAddress(chainID, fromSym)
	if err != nil {
		return nil, nil, fmt.Errorf("unsupported from token: %w", err)
	}
	toAddr, err := paraSwapTokenAddress(chainID, toSym)
	if err != nil {
		return nil, nil, fmt.Errorf("unsupported to token: %w", err)
	}
	// ParaSwap API validates token addresses strictly; lowercase is required.
	fromAddr = strings.ToLower(fromAddr)
	toAddr = strings.ToLower(toAddr)

	fromDecimals := tokenDecimalsForSymbol(fromSym)
	toDecimals := tokenDecimalsForSymbol(toSym)

	srcAmount, err := decimalToBaseUnits(req.AmountFrom, fromDecimals)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid amount_from: %w", err)
	}

	q := url.Values{}
	q.Set("srcToken", fromAddr)
	q.Set("destToken", toAddr)
	q.Set("amount", srcAmount)
	q.Set("side", "SELL")
	q.Set("network", chainID)
	q.Set("srcDecimals", strconv.Itoa(fromDecimals))
	q.Set("destDecimals", strconv.Itoa(toDecimals))

	quoteURL := fmt.Sprintf("%s/prices?%s", strings.TrimSuffix(p.baseURL, "/"), q.Encode())
	reqHTTP, err := http.NewRequestWithContext(ctx, http.MethodGet, quoteURL, nil)
	if err != nil {
		return nil, nil, err
	}
	reqHTTP.Header.Set("Accept", "application/json")
	reqHTTP.Header.Set("User-Agent", quoteUserAgent)

	resp, err := p.client.Do(reqHTTP)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		return nil, nil, fmt.Errorf("paraswap quote error %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}

	var out struct {
		PriceRoute struct {
			SrcAmount  string `json:"srcAmount"`
			DestAmount string `json:"destAmount"`
			GasCostUSD string `json:"gasCostUSD"`
		} `json:"priceRoute"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, nil, err
	}
	if strings.TrimSpace(out.PriceRoute.DestAmount) == "" {
		return nil, nil, fmt.Errorf("paraswap response missing priceRoute.destAmount")
	}

	amountToUSD, amountFromUSD, usdWarnings := computeQuoteUSDAmounts(ctx, fromSym, toSym, out.PriceRoute.SrcAmount, out.PriceRoute.DestAmount)

	quote := Quote{
		Provider:      "paraswap",
		AmountTo:      out.PriceRoute.DestAmount,
		AmountToUSD:   amountToUSD,
		AmountFrom:    out.PriceRoute.SrcAmount,
		AmountFromUSD: amountFromUSD,
		Fixed:         "False",
		Kycrating:     "A",
		Eta:           1,
		Waste:         out.PriceRoute.GasCostUSD,
	}
	return []Quote{quote}, usdWarnings, nil
}

func (p *ZeroXQuoteProvider) GetQuotes(ctx context.Context, req SwapRateRequest) ([]Quote, []string, error) {
	if req.TickerFrom == "" || req.TickerTo == "" {
		return nil, nil, fmt.Errorf("ticker_from and ticker_to are required")
	}
	if req.AmountFrom == "" {
		return nil, nil, fmt.Errorf("0x API requires amount_from")
	}

	fromSym := normalizeQuoteSymbol(req.TickerFrom)
	toSym := normalizeQuoteSymbol(req.TickerTo)

	chainID := p.chainID
	resolvedChainID, err := resolveOnchainChainID(req, chainID)
	if err != nil {
		return nil, nil, err
	}
	chainID = resolvedChainID

	taker := p.taker
	if strings.TrimSpace(req.Taker) != "" {
		if !isValidEthAddress(req.Taker) {
			return nil, nil, fmt.Errorf("invalid taker address")
		}
		taker = req.Taker
	}

	dec := tokenDecimalsForSymbol(fromSym)

	sellAmount, err := decimalToBaseUnits(req.AmountFrom, dec)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid amount_from: %w", err)
	}

	if _, ok := supportedZeroXChains[chainID]; !ok {
		return nil, nil, fmt.Errorf("unsupported chain id %q; supported values are %v", chainID, supportedZeroXChainsList())
	}

	fromAddr, err := zeroXTokenAddress(chainID, fromSym)
	if err != nil {
		return nil, nil, fmt.Errorf("unsupported from token: %w", err)
	}
	toAddr, err := zeroXTokenAddress(chainID, toSym)
	if err != nil {
		return nil, nil, fmt.Errorf("unsupported to token: %w", err)
	}

	log.Printf("0x request params: chain=%s sellToken=%s buyToken=%s sellAmount=%s taker=%s",
		chainID, fromAddr, toAddr, sellAmount, taker)

	quoteURL := fmt.Sprintf("%s/swap/permit2/quote?chainId=%s&sellToken=%s&buyToken=%s&sellAmount=%s&taker=%s",
		strings.TrimSuffix(p.baseURL, "/"), url.QueryEscape(chainID), url.QueryEscape(fromAddr), url.QueryEscape(toAddr), url.QueryEscape(sellAmount), url.QueryEscape(taker))

	reqHTTP, err := http.NewRequestWithContext(ctx, http.MethodGet, quoteURL, nil)
	if err != nil {
		return nil, nil, err
	}
	reqHTTP.Header.Set("User-Agent", quoteUserAgent)
	reqHTTP.Header.Set("Accept", "application/json")

	if p.apiKey == "" {
		return nil, nil, fmt.Errorf("0x API requires SWAP_0X_API_KEY environment variable")
	}
	reqHTTP.Header.Set("0x-api-key", p.apiKey)
	reqHTTP.Header.Set("0x-version", "v2")

	resp, err := p.client.Do(reqHTTP)
	if err != nil {
		return nil, nil, err
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
					return nil, nil, err
				}
				reqHTTP.Header.Set("User-Agent", quoteUserAgent)
				reqHTTP.Header.Set("Accept", "application/json")
				reqHTTP.Header.Set("0x-api-key", p.apiKey)
				reqHTTP.Header.Set("0x-version", "v2")

				resp2, err := p.client.Do(reqHTTP)
				if err != nil {
					return nil, nil, err
				}
				defer resp2.Body.Close()
				if resp2.StatusCode != http.StatusOK {
					data2, _ := io.ReadAll(resp2.Body)
					return nil, nil, fmt.Errorf("0x quote error %d: %s", resp2.StatusCode, strings.TrimSpace(string(data2)))
				}
				// use resp2 as the successful response
				resp = resp2
			} else {
				return nil, nil, fmt.Errorf("0x quote error %d: %s", resp.StatusCode, bodyStr)
			}
		} else {
			return nil, nil, fmt.Errorf("0x quote error %d: %s", resp.StatusCode, bodyStr)
		}
	}

	var out struct {
		BuyAmount  string `json:"buyAmount"`
		SellAmount string `json:"sellAmount"`
		Gas        int64  `json:"gas"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, nil, err
	}

	amountToUSD, amountFromUSD, usdWarnings := computeQuoteUSDAmounts(ctx, fromSym, toSym, out.SellAmount, out.BuyAmount)

	quote := Quote{
		Provider:      "0x",
		AmountTo:      out.BuyAmount,
		AmountToUSD:   amountToUSD,
		AmountFrom:    out.SellAmount,
		AmountFromUSD: amountFromUSD,
		Fixed:         "False",
		Kycrating:     "A",
		Eta:           1,
		Waste:         fmt.Sprintf("gas=%d", out.Gas),
	}
	return []Quote{quote}, usdWarnings, nil
}

func uniqueStrings(values []string) []string {
	if len(values) == 0 {
		return values
	}
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

func computeQuoteUSDAmounts(ctx context.Context, fromSym, toSym, amountFromBase, amountToBase string) (string, string, []string) {
	fromAmt, err := baseUnitsToFloat(amountFromBase, tokenDecimalsForSymbol(fromSym))
	if err != nil {
		fromAmt = 0
	}
	toAmt, err := baseUnitsToFloat(amountToBase, tokenDecimalsForSymbol(toSym))
	if err != nil {
		toAmt = 0
	}

	fromPrice, fromOK, fromStale, fromStaleAge := tokenUSDPrice(ctx, fromSym)
	toPrice, toOK, toStale, toStaleAge := tokenUSDPrice(ctx, toSym)

	warnings := make([]string, 0, 1)
	if fromStale || toStale {
		staleAge := fromStaleAge
		if toStaleAge > staleAge {
			staleAge = toStaleAge
		}
		warnings = append(warnings, formatUSDStaleWarning(staleAge))
	}

	var fromUSD float64
	var toUSD float64
	if fromOK {
		fromUSD = fromAmt * fromPrice
		toUSD = fromUSD
	}
	if toOK {
		toUSD = toAmt * toPrice
		if fromUSD == 0 {
			fromUSD = toUSD
		}
	}

	if fromUSD == 0 && toUSD > 0 {
		fromUSD = toUSD
	}
	if toUSD == 0 && fromUSD > 0 {
		toUSD = fromUSD
	}

	return fmt.Sprintf("%.2f", toUSD), fmt.Sprintf("%.2f", fromUSD), uniqueStrings(warnings)
}

func baseUnitsToFloat(raw string, decimals int) (float64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, fmt.Errorf("empty amount")
	}
	bi, ok := new(big.Int).SetString(raw, 10)
	if !ok {
		return 0, fmt.Errorf("invalid integer amount")
	}
	if decimals <= 0 {
		f, _ := strconv.ParseFloat(raw, 64)
		return f, nil
	}

	denom := new(big.Float).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil))
	val := new(big.Float).Quo(new(big.Float).SetInt(bi), denom)
	out, _ := val.Float64()
	return out, nil
}

func tokenDecimalsForSymbol(symbol string) int {
	switch normalizeQuoteSymbol(symbol) {
	case "ETH", "DAI", "LINK", "UNI", "AAVE", "MATIC", "OP", "ARB", "CRV":
		return 18
	case "USDC", "USDT":
		return 6
	case "BTC", "WBTC":
		return 8
	case "SHIB":
		return 18
	default:
		return 18
	}
}

func tokenUSDPrice(ctx context.Context, symbol string) (float64, bool, bool, int) {
	s := normalizeQuoteSymbol(symbol)
	if s == "USDC" || s == "USDT" || s == "DAI" {
		return 1, true, false, 0
	}

	prices, stale, staleAgeMinutes, err := getUSDPriceMap(ctx)
	if err != nil {
		return 0, false, false, 0
	}
	id, ok := tokenCoinGeckoID(s)
	if !ok {
		return 0, false, stale, staleAgeMinutes
	}
	p, ok := prices[id]
	return p, ok, stale, staleAgeMinutes
}

func tokenCoinGeckoID(symbol string) (string, bool) {
	switch normalizeQuoteSymbol(symbol) {
	case "ETH":
		return "ethereum", true
	case "BTC", "WBTC":
		return "bitcoin", true
	case "USDC":
		return "usd-coin", true
	case "USDT":
		return "tether", true
	case "DAI":
		return "dai", true
	case "LINK":
		return "chainlink", true
	case "UNI":
		return "uniswap", true
	case "AAVE":
		return "aave", true
	case "MATIC":
		return "matic-network", true
	case "OP":
		return "optimism", true
	case "ARB":
		return "arbitrum", true
	case "SHIB":
		return "shiba-inu", true
	case "CRV":
		return "curve-dao-token", true
	default:
		return "", false
	}
}

func getUSDPriceMap(ctx context.Context) (map[string]float64, bool, int, error) {
	now := time.Now()
	usdPriceCache.RLock()
	if len(usdPriceCache.prices) > 0 && now.Before(usdPriceCache.expiresAt) {
		cached := make(map[string]float64, len(usdPriceCache.prices))
		for k, v := range usdPriceCache.prices {
			cached[k] = v
		}
		usdPriceCache.RUnlock()
		return cached, false, 0, nil
	}
	usdPriceCache.RUnlock()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://api.coingecko.com/api/v3/simple/price?ids=bitcoin,ethereum,tether,usd-coin,dai,chainlink,uniswap,aave,matic-network,optimism,arbitrum,shiba-inu,curve-dao-token&vs_currencies=usd", nil)
	if err != nil {
		return nil, false, 0, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", quoteUserAgent)

	resp, err := usdPriceClient.Do(req)
	if err != nil {
		stale, staleAge := cloneStalePriceCache()
		if len(stale) > 0 {
			return stale, true, staleAge, nil
		}
		return nil, false, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		stale, staleAge := cloneStalePriceCache()
		if len(stale) > 0 {
			return stale, true, staleAge, nil
		}
		return nil, false, 0, fmt.Errorf("coingecko price error %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}

	var raw map[string]map[string]float64
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		stale, staleAge := cloneStalePriceCache()
		if len(stale) > 0 {
			return stale, true, staleAge, nil
		}
		return nil, false, 0, err
	}
	out := make(map[string]float64, len(raw))
	for id, priceObj := range raw {
		if usd, ok := priceObj["usd"]; ok && usd > 0 {
			out[id] = usd
		}
	}

	usdPriceCache.Lock()
	usdPriceCache.prices = out
	usdPriceCache.updatedAt = now
	usdPriceCache.expiresAt = now.Add(5 * time.Minute)
	usdPriceCache.Unlock()

	return out, false, 0, nil
}

func cloneStalePriceCache() (map[string]float64, int) {
	usdPriceCache.RLock()
	defer usdPriceCache.RUnlock()
	if len(usdPriceCache.prices) == 0 {
		return nil, 0
	}
	stale := make(map[string]float64, len(usdPriceCache.prices))
	for k, v := range usdPriceCache.prices {
		stale[k] = v
	}
	ageMinutes := 0
	if !usdPriceCache.updatedAt.IsZero() {
		ageMinutes = int(time.Since(usdPriceCache.updatedAt).Minutes())
		if ageMinutes < 0 {
			ageMinutes = 0
		}
	}
	return stale, ageMinutes
}

func formatUSDStaleWarning(ageMinutes int) string {
	if ageMinutes <= 0 {
		return usdPriceStaleWarning
	}
	return fmt.Sprintf("%s (%dm old)", usdPriceStaleWarning, ageMinutes)
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

// runProviderSelfCheck probes each configured provider at startup and removes
// any that are unreachable. It returns the (possibly reduced) QuoteProvider.
func runProviderSelfCheck(qp QuoteProvider) QuoteProvider {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("SWAP_SELF_CHECK")))
	if v == "0" || v == "false" || v == "no" || v == "off" {
		log.Printf("self-check: skipped (SWAP_SELF_CHECK=0)")
		return qp
	}

	fromTicker := strings.TrimSpace(os.Getenv("SWAP_SELF_CHECK_FROM"))
	if fromTicker == "" {
		fromTicker = "ETH"
	}
	toTicker := strings.TrimSpace(os.Getenv("SWAP_SELF_CHECK_TO"))
	if toTicker == "" {
		toTicker = "USDC"
	}
	amount := strings.TrimSpace(os.Getenv("SWAP_SELF_CHECK_AMOUNT"))
	if amount == "" {
		amount = "1"
	}
	checkChainID := strings.TrimSpace(os.Getenv("SWAP_SELF_CHECK_CHAIN_ID"))
	minProviders := 1
	if raw := strings.TrimSpace(os.Getenv("SWAP_SELF_CHECK_MIN_PROVIDERS")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			minProviders = n
		}
	}

	probeReq := SwapRateRequest{
		TickerFrom: fromTicker,
		TickerTo:   toTicker,
		AmountFrom: amount,
		ChainID:    checkChainID,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	switch p := qp.(type) {
	case *MultiQuoteProvider:
		var available []namedQuoteProvider
		var wg sync.WaitGroup
		type probeResult struct {
			idx     int
			ok      bool
			err     string
			latency time.Duration
		}
		results := make([]probeResult, len(p.providers))
		for i, np := range p.providers {
			i, np := i, np
			wg.Add(1)
			go func() {
				defer wg.Done()
				start := time.Now()
				_, _, err := np.provider.GetQuotes(ctx, probeReq)
				latency := time.Since(start)
				if err != nil {
					results[i] = probeResult{idx: i, ok: false, err: firstLine(err.Error(), 120), latency: latency}
				} else {
					results[i] = probeResult{idx: i, ok: true, latency: latency}
				}
			}()
		}
		wg.Wait()

		for _, r := range results {
			np := p.providers[r.idx]
			if r.ok {
				log.Printf("self-check: provider %s ✓ (%dms)", np.name, r.latency.Milliseconds())
				available = append(available, np)
			} else {
				log.Printf("self-check: provider %s ✗ (%s)", np.name, r.err)
			}
		}

		if len(available) < minProviders {
			log.Printf("self-check: only %d/%d providers available (min=%d); keeping all providers as fallback", len(available), len(p.providers), minProviders)
			return qp
		}

		if len(available) == 0 {
			log.Printf("self-check: no providers available; using mock provider")
			return MockQuoteProvider{}
		}
		if len(available) == 1 {
			log.Printf("self-check: 1 provider available: %s", available[0].name)
			return available[0].provider
		}
		names := make([]string, len(available))
		for i, a := range available {
			names[i] = a.name
		}
		log.Printf("self-check: %d/%d providers available: %v", len(available), len(p.providers), names)
		return &MultiQuoteProvider{providers: available}

	default:
		// Single provider: just probe it
		start := time.Now()
		_, _, err := qp.GetQuotes(ctx, probeReq)
		latency := time.Since(start)
		if err != nil {
			log.Printf("self-check: single provider %T ✗ (%s) (%dms); keeping as fallback", qp, firstLine(err.Error(), 120), latency.Milliseconds())
		} else {
			log.Printf("self-check: single provider %T ✓ (%dms)", qp, latency.Milliseconds())
		}
		return qp
	}
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
		case "LINK":
			return "0x514910771AF9Ca656af840dff83E8264EcF986CA", nil
		case "UNI":
			return "0x1f9840a85d5aF5bf1D1762F925BDADdC4201F984", nil
		case "AAVE":
			return "0x7Fc66500c84A76Ad7e9c93437bFc5Ac33E2DDaE9", nil
		case "MATIC", "POL":
			return "0x7D1AfA7B718fb893dB30A3aBc0Cfc608AaCfeBB0", nil
		case "OP":
			return "0x4200000000000000000000000000000000000042", nil
		case "ARB":
			return "0xB50721BCf8d664c30412Cfbc6cf7a15145234ad1", nil
		case "SHIB":
			return "0x95aD61b0a150d79219dCF64E1E6Cc01f0B64C4cE", nil
		case "CRV":
			return "0xD533a949740bb3306d119CC777fa900bA034cd52", nil
		default:
			return "", fmt.Errorf("unsupported token %s on chain %s", symbol, chainId)
		}
	case "10":
		switch symbol {
		case "ETH":
			return "0x4200000000000000000000000000000000000006", nil
		case "WBTC":
			return "0x68f180fcCe6836688e9084f035309E29Bf0A2095", nil
		case "USDC":
			return "0x7F5c764cBc14f9669B88837ca1490cCa17c31607", nil
		case "DAI":
			return "0xDA10009cBd5D07dd0CeCc66161FC93D7c9000da1", nil
		case "USDT":
			return "0x7f3f7A2065dC5680d8c8cEce3e30bbf6D4d8236A", nil
		case "LINK":
			return "0x350a791Bfc2C21F9Ed5d10980Dad2e2638ffa7f6", nil
		case "UNI":
			return "0x6fd9d7AD17242c41f7131d257212c54A0e816691", nil
		case "OP":
			return "0x4200000000000000000000000000000000000042", nil
		default:
			return "", fmt.Errorf("unsupported token %s on chain %s", symbol, chainId)
		}
	case "137":
		switch symbol {
		case "ETH":
			// Polygon: use WETH token address for ETH swaps (not native MATIC 0x...1010).
			return "0x7ceB23fD6bC0adD59E62ac25578270cFf1b9f619", nil
		case "WBTC":
			return "0x1BFD67037B42Cf73acF2047067bd4F2C47D9BfD6", nil
		case "USDC":
			return "0x2791Bca1f2de4661ED88A30C99A7a9449Aa84174", nil
		case "DAI":
			return "0x8f3Cf7ad23Cd3CaDbD9735AFf958023239c6A063", nil
		case "USDT":
			return "0x3813e82e6f7098b9583FC0F33a962D02018B6803", nil
		case "LINK":
			return "0x53E0bca35eC356BD5ddDFebbD1Fc0fD03FaBad39", nil
		case "UNI":
			return "0xb33EaAd8d922B1083446DC23f610c2567fB5180f", nil
		case "AAVE":
			return "0xD6DF932A45C0f255f85145f286eA0b292B21C90B", nil
		case "MATIC", "POL":
			return "0x0000000000000000000000000000000000001010", nil
		case "CRV":
			return "0x172370d5Cd63279eFa6d502DAB29171933a610AF", nil
		case "SHIB":
			return "0x6f8a06447Ff6FcF75d803135a7de15CE88C1d4ec", nil
		default:
			return "", fmt.Errorf("unsupported token %s on chain %s", symbol, chainId)
		}
	case "42161":
		switch symbol {
		case "ETH":
			return "0xEeeeeEeeeEeEeeEeEeEeeEEEeeeeEeeeeeeeEEeE", nil
		case "WBTC":
			return "0x2f2a2543B76A4166549F7aAb2e75Bef0aEfC5B0f", nil
		case "USDC":
			return "0xFF970A61A04b1cA14834A43f5dE4533eBDDB5CC8", nil
		case "DAI":
			return "0xDA10009cBd5D07dd0CeCc66161FC93D7c9000da1", nil
		case "USDT":
			return "0xFd086bC7CD5C481DCC9C85ebE478A1C0b69FCbb9", nil
		case "LINK":
			return "0xf97f4df75117a78c1A5a0DBb814Af92458539784", nil
		case "UNI":
			return "0xCc06954023f30C1d2cBF2f8F0E4E97b2b9cDA398", nil
		case "ARB":
			return "0xB50721BCf8d664c30412Cfbc6cf7a15145234ad1", nil
		case "AAVE":
			return "0xba5DdD1f9d7F570dc94a5147940003aEa7f3F1E1", nil
		case "CRV":
			return "0x8Ee73c484A26e0A9dfD6F044E6c197dF91741572", nil
		default:
			return "", fmt.Errorf("unsupported token %s on chain %s", symbol, chainId)
		}
	case "56":
		switch symbol {
		case "ETH":
			return "0xEeeeeEeeeEeEeeEeEeEeeEEEeeeeEeeeeeeeEEeE", nil
		case "WBTC":
			return "0x7130d2A12B9BCbfae4f2634d864A1Ee1Ce3Ead9c", nil
		case "USDC":
			return "0x8ac76a51cc950d9822d68b83fe1ad97b32cd580d", nil
		case "DAI":
			return "0x1AF3F329e8BE154074D8769D1FFa4eE058B1DBc3", nil
		case "USDT":
			return "0x55d398326f99059fF775485246999027B3197955", nil
		case "LINK":
			return "0xF8A0BF9cF54Bb92F17374d9e9A321E6a111a51bD", nil
		case "UNI":
			return "0xBf5140A22578168FD562DCcF235E5D43A02ce9B1", nil
		case "AAVE":
			return "0xfb6115445Bff7b52Fe59818C9f2E7bdBbE03b327", nil
		case "SHIB":
			return "0x2859e4544C4bB03966803b044A93563Bd2D0DD4D", nil
		case "CRV":
			return "0x1E3E571D9d53F5b1B9F4a78E8b0590D9F3d4e27e", nil
		default:
			return "", fmt.Errorf("unsupported token %s on chain %s", symbol, chainId)
		}
	case "8453": // Base
		switch symbol {
		case "ETH":
			return "0xEeeeeEeeeEeEeeEeEeEeeEEEeeeeEeeeeeeeEEeE", nil
		case "WBTC":
			return "0xcbB7C0000aB88B473b1f5aFd9ef808440eed33Bf", nil
		case "USDC":
			return "0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913", nil
		case "DAI":
			return "0x50c5725949A6F0c72E6C4a641F24049A917DB0Cb", nil
		case "USDT":
			return "0xfde4C96c8593536E31F229EA8f37b2ADa2699bb2", nil
		case "LINK":
			return "0x88Fb150BDc53Df76642b3C5B501c17f6F76f16a8", nil
		case "UNI":
			return "0xc3De8306946Eac2faBBc7eE50630936a6D5d9175", nil
		case "OP":
			return "0x4200000000000000000000000000000000000042", nil
		case "CRV":
			return "0x8Ee73c484A26e0A9dfD6F044E6c197dF91741572", nil
		default:
			return "", fmt.Errorf("unsupported token %s on chain %s", symbol, chainId)
		}
	case "43114": // Avalanche C-Chain
		switch symbol {
		case "ETH":
			// Avalanche has no native ETH; use Wrapped Ether (WETH.e) bridged via Avalanche Bridge
			return "0x49D5c2BdFfac6CE2BFdB6640F4F80f226bc10bAB", nil
		case "WBTC":
			return "0x50b7545627a5162F82A992c33b87aDc75187B218", nil
		case "USDC":
			return "0xB97EF9Ef8734C71904D8002F8b6Bc66Dd9c48a6E", nil
		case "DAI":
			return "0xd586E7F844cEa2F87f50152665BCbc2C279D8d70", nil
		case "USDT":
			return "0x9702230A8Ea53601f5cD2dc00fDBc13d4dF4A8c7", nil
		case "LINK":
			return "0x5947BB275c521040051D1bf4352722356C457f96", nil
		case "UNI":
			return "0x8eBA8A9db1e8a6e6b7fCc1c87e16f3fA6ea8a4c1", nil
		case "AAVE":
			return "0x63a72806098d3D9540f05854f9c23fA2a2776CEf", nil
		case "CRV":
			return "0x6A5F3A9c4d5d7b6f2c6b4a8c9d1e3f5A7b8C9d2E", nil
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

func paraSwapTokenAddress(chainId, symbol string) (string, error) {
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
	chains := make([]string, 0, len(supportedZeroXChains))
	for chain := range supportedZeroXChains {
		chains = append(chains, chain)
	}
	sort.Slice(chains, func(i, j int) bool {
		ai, aErr := strconv.Atoi(chains[i])
		bi, bErr := strconv.Atoi(chains[j])
		if aErr == nil && bErr == nil {
			return ai < bi
		}
		return chains[i] < chains[j]
	})
	return chains
}
