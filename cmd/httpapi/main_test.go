package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func TestIsValidEthAddress(t *testing.T) {
	cases := []struct {
		addr  string
		valid bool
	}{
		{"0x0000000000000000000000000000000000010000", true},
		{"0xA0b86991c6218b36c1d19d4a2e9eb0ce3606eb48", true},
		{"0xEeeeeEeeeEeEeeEeEeEeeEEEeeeeEeeeeeeeEEeE", true},
		{"0x123", false}, // too short
		{"A0b86991c6218b36c1d19d4a2e9eb0ce3606eb48", false}, // no 0x prefix
		{"0xGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGG", false}, // non-hex
		{"", false},
	}
	for _, c := range cases {
		got := isValidEthAddress(c.addr)
		if got != c.valid {
			t.Errorf("isValidEthAddress(%q) = %v, want %v", c.addr, got, c.valid)
		}
	}
}

func TestDecimalToBaseUnits(t *testing.T) {
	cases := []struct {
		amount   string
		decimals int
		want     string
	}{
		{"1", 18, "1000000000000000000"},
		{"0.1", 18, "100000000000000000"},
		{"0.5", 6, "500000"},
		{"100", 6, "100000000"},
		{"0.001", 8, "100000"},
	}
	for _, c := range cases {
		got, err := decimalToBaseUnits(c.amount, c.decimals)
		if err != nil {
			t.Errorf("decimalToBaseUnits(%q, %d): unexpected error: %v", c.amount, c.decimals, err)
			continue
		}
		if got != c.want {
			t.Errorf("decimalToBaseUnits(%q, %d) = %q, want %q", c.amount, c.decimals, got, c.want)
		}
	}

	_, err := decimalToBaseUnits("not-a-number", 18)
	if err == nil {
		t.Error("decimalToBaseUnits(\"not-a-number\") expected error, got nil")
	}
}

func TestWrappedETHAddress(t *testing.T) {
	cases := []struct {
		chainID string
		wantOK  bool
	}{
		{"42161", true},  // Arbitrum
		{"56", true},     // BSC
		{"8453", true},   // Base
		{"43114", true},  // Avalanche
		{"1", false},     // Ethereum – native placeholder is fine, no WETH override
		{"10", false},    // Optimism
		{"137", false},   // Polygon
		{"99999", false}, // unknown
	}
	for _, c := range cases {
		addr, ok := wrappedETHAddress(c.chainID)
		if ok != c.wantOK {
			t.Errorf("wrappedETHAddress(%q) ok=%v, want %v", c.chainID, ok, c.wantOK)
			continue
		}
		if ok && !isValidEthAddress(addr) {
			t.Errorf("wrappedETHAddress(%q) returned invalid address %q", c.chainID, addr)
		}
	}
}

func TestZeroXTokenAddress(t *testing.T) {
	type tc struct {
		chain, sym string
		wantErr    bool
	}
	cases := []tc{
		// supported tokens on each chain
		{"1", "ETH", false},
		{"1", "USDC", false},
		{"10", "ETH", false},
		{"10", "USDC", false},
		{"137", "ETH", false},
		{"137", "USDC", false},
		{"42161", "ETH", false},
		{"42161", "USDC", false},
		{"56", "ETH", false},
		{"56", "USDC", false},
		{"8453", "ETH", false},
		{"8453", "USDC", false},
		{"43114", "ETH", false},
		{"43114", "USDC", false},
		// unknown token
		{"1", "UNKNOWN", true},
		// unsupported chain
		{"99999", "ETH", true},
	}
	for _, c := range cases {
		addr, err := zeroXTokenAddress(c.chain, c.sym)
		if c.wantErr {
			if err == nil {
				t.Errorf("zeroXTokenAddress(%q, %q) expected error, got %q", c.chain, c.sym, addr)
			}
		} else {
			if err != nil {
				t.Errorf("zeroXTokenAddress(%q, %q) unexpected error: %v", c.chain, c.sym, err)
			} else if !isValidEthAddress(addr) {
				t.Errorf("zeroXTokenAddress(%q, %q) = %q is not a valid Ethereum address", c.chain, c.sym, addr)
			}
		}
	}
}

func TestZeroXTokenAddress_PolygonETHUsesWETH(t *testing.T) {
	addr, err := zeroXTokenAddress("137", "ETH")
	if err != nil {
		t.Fatalf("zeroXTokenAddress(137, ETH) unexpected error: %v", err)
	}

	const polygonWETH = "0x7ceB23fD6bC0adD59E62ac25578270cFf1b9f619"
	const polygonNativeMATIC = "0x0000000000000000000000000000000000001010"

	if addr != polygonWETH {
		t.Fatalf("polygon ETH mapping = %q, want WETH %q", addr, polygonWETH)
	}
	if addr == polygonNativeMATIC {
		t.Fatalf("polygon ETH mapping should not use native MATIC placeholder %q", polygonNativeMATIC)
	}
}

type stubQuoteProvider struct {
	delay  time.Duration
	quotes []Quote
	err    error
}

func (s stubQuoteProvider) GetQuotes(ctx context.Context, req SwapRateRequest) ([]Quote, error) {
	if s.delay > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(s.delay):
		}
	}
	if s.err != nil {
		return nil, s.err
	}
	return s.quotes, nil
}

func TestParseProviderList(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want []string
	}{
		{name: "comma", raw: "0x,1inch", want: []string{"0x", "1inch"}},
		{name: "with paraswap", raw: "0x,1inch,paraswap", want: []string{"0x", "1inch", "paraswap"}},
		{name: "plus", raw: "0x+1inch", want: []string{"0x", "1inch"}},
		{name: "space and duplicate", raw: "0x 1inch 0x", want: []string{"0x", "1inch"}},
		{name: "empty becomes mock", raw: "", want: []string{"mock"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseProviderList(tt.raw)
			if len(got) != len(tt.want) {
				t.Fatalf("parseProviderList(%q) len=%d want=%d; got=%v", tt.raw, len(got), len(tt.want), got)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Fatalf("parseProviderList(%q)[%d]=%q want=%q", tt.raw, i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestMultiQuoteProvider_AggregatesSortsAndToleratesFailure(t *testing.T) {
	mp := &MultiQuoteProvider{providers: []namedQuoteProvider{
		{
			name: "0x",
			provider: stubQuoteProvider{
				delay: 50 * time.Millisecond,
				quotes: []Quote{
					{Provider: "0x", AmountTo: "200"},
				},
			},
		},
		{
			name: "1inch",
			provider: stubQuoteProvider{
				delay: 50 * time.Millisecond,
				quotes: []Quote{
					{Provider: "1inch", AmountTo: "300"},
				},
			},
		},
		{
			name:     "bad",
			provider: stubQuoteProvider{delay: 10 * time.Millisecond, err: errors.New("boom")},
		},
	}}

	start := time.Now()
	quotes, err := mp.GetQuotes(context.Background(), SwapRateRequest{TickerFrom: "ETH", TickerTo: "USDC", AmountFrom: "0.1"})
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("GetQuotes unexpected error: %v", err)
	}
	if len(quotes) != 2 {
		t.Fatalf("GetQuotes returned %d quotes, want 2", len(quotes))
	}
	if quotes[0].AmountTo != "300" || quotes[0].Provider != "1inch" {
		t.Fatalf("best quote ordering mismatch: first=%+v", quotes[0])
	}
	if quotes[1].AmountTo != "200" || quotes[1].Provider != "0x" {
		t.Fatalf("second quote ordering mismatch: second=%+v", quotes[1])
	}
	if elapsed >= 90*time.Millisecond {
		t.Fatalf("providers likely not running concurrently; elapsed=%s", elapsed)
	}
}

func TestMultiQuoteProvider_AllFail(t *testing.T) {
	mp := &MultiQuoteProvider{providers: []namedQuoteProvider{
		{name: "0x", provider: stubQuoteProvider{err: errors.New("0x down")}},
		{name: "1inch", provider: stubQuoteProvider{err: errors.New("1inch down")}},
	}}

	quotes, err := mp.GetQuotes(context.Background(), SwapRateRequest{TickerFrom: "ETH", TickerTo: "USDC", AmountFrom: "0.1"})
	if err == nil {
		t.Fatalf("expected error when all providers fail, got nil with quotes=%v", quotes)
	}
}

// ---------------------------------------------------------------------------
// ZeroXQuoteProvider integration-style tests using a mock HTTP server
// ---------------------------------------------------------------------------

// mockZeroXResponse represents the minimal JSON structure returned by 0x permit2/quote.
type mockZeroXResponse struct {
	BuyAmount  string `json:"buyAmount"`
	SellAmount string `json:"sellAmount"`
	Gas        int64  `json:"gas"`
}

// errResponse is the 0x error shape that triggers the WETH fallback.
type errResponse struct {
	Code    int    `json:"code"`
	Reason  string `json:"reason"`
	Message string `json:"message"`
}

// newZeroXProvider creates a ZeroXQuoteProvider wired to the given server URL,
// with the supplied chainID and a default test taker address.
func newZeroXProvider(baseURL, chainID string) *ZeroXQuoteProvider {
	return &ZeroXQuoteProvider{
		client:  &http.Client{},
		baseURL: baseURL,
		apiKey:  "test-key",
		chainID: chainID,
		taker:   defaultZeroXTaker,
	}
}

func newParaSwapProvider(baseURL, chainID string) *ParaSwapQuoteProvider {
	return &ParaSwapQuoteProvider{
		client:  &http.Client{},
		baseURL: baseURL,
		chainID: chainID,
	}
}

func TestParaSwapGetQuotes_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"priceRoute": map[string]any{
				"srcAmount":  "100000000000000000",
				"destAmount": "200000000",
				"gasCost":    "12345",
			},
		})
	}))
	defer srv.Close()

	p := newParaSwapProvider(srv.URL, "1")
	quotes, err := p.GetQuotes(context.Background(), SwapRateRequest{
		TickerFrom: "ETH",
		TickerTo:   "USDC",
		AmountFrom: "0.1",
	})
	if err != nil {
		t.Fatalf("GetQuotes: unexpected error: %v", err)
	}
	if len(quotes) != 1 {
		t.Fatalf("GetQuotes: want 1 quote, got %d", len(quotes))
	}
	if quotes[0].Provider != "paraswap" {
		t.Errorf("quote.Provider = %q, want \"paraswap\"", quotes[0].Provider)
	}
	if quotes[0].AmountTo != "200000000" {
		t.Errorf("quote.AmountTo = %q, want \"200000000\"", quotes[0].AmountTo)
	}
}

func TestParaSwapGetQuotes_UnsupportedChain(t *testing.T) {
	p := newParaSwapProvider("http://localhost", "99999")
	_, err := p.GetQuotes(context.Background(), SwapRateRequest{
		TickerFrom: "ETH",
		TickerTo:   "USDC",
		AmountFrom: "0.1",
	})
	if err == nil {
		t.Fatal("expected unsupported chain error, got nil")
	}
}

func TestZeroXGetQuotes_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(mockZeroXResponse{
			BuyAmount:  "200000000", // 200 USDC in base units (6 dec)
			SellAmount: "100000000000000000",
			Gas:        21000,
		})
	}))
	defer srv.Close()

	p := newZeroXProvider(srv.URL, "1")
	quotes, err := p.GetQuotes(context.Background(), SwapRateRequest{
		TickerFrom: "ETH",
		TickerTo:   "USDC",
		AmountFrom: "0.1",
	})
	if err != nil {
		t.Fatalf("GetQuotes: unexpected error: %v", err)
	}
	if len(quotes) != 1 {
		t.Fatalf("GetQuotes: want 1 quote, got %d", len(quotes))
	}
	if quotes[0].Provider != "0x" {
		t.Errorf("quote.Provider = %q, want \"0x\"", quotes[0].Provider)
	}
	if quotes[0].AmountTo != "200000000" {
		t.Errorf("quote.AmountTo = %q, want \"200000000\"", quotes[0].AmountTo)
	}
}

func TestZeroXGetQuotes_ChainIDOverride(t *testing.T) {
	var receivedQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(mockZeroXResponse{BuyAmount: "1", SellAmount: "1", Gas: 0})
	}))
	defer srv.Close()

	p := newZeroXProvider(srv.URL, "1") // provider default is chain 1
	_, err := p.GetQuotes(context.Background(), SwapRateRequest{
		TickerFrom: "ETH",
		TickerTo:   "USDC",
		AmountFrom: "0.1",
		ChainID:    "137", // override to Polygon
	})
	if err != nil {
		t.Fatalf("GetQuotes: unexpected error: %v", err)
	}
	if receivedQuery == "" {
		t.Fatal("no query received by mock server")
	}
	// The request must have chainId=137, not the provider's default 1
	if !contains(receivedQuery, "chainId=137") {
		t.Errorf("expected chainId=137 in query, got: %s", receivedQuery)
	}
}

func TestZeroXGetQuotes_TakerOverride(t *testing.T) {
	const customTaker = "0xdead000000000000000000000000000000000001"
	var receivedQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(mockZeroXResponse{BuyAmount: "1", SellAmount: "1", Gas: 0})
	}))
	defer srv.Close()

	p := newZeroXProvider(srv.URL, "1")
	_, err := p.GetQuotes(context.Background(), SwapRateRequest{
		TickerFrom: "ETH",
		TickerTo:   "USDC",
		AmountFrom: "0.1",
		Taker:      customTaker,
	})
	if err != nil {
		t.Fatalf("GetQuotes: unexpected error: %v", err)
	}
	if !contains(receivedQuery, "taker=0xdead") {
		t.Errorf("expected custom taker in query, got: %s", receivedQuery)
	}
}

func TestZeroXGetQuotes_InvalidTakerRejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Should never be reached; provider validates taker before sending.
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	p := newZeroXProvider(srv.URL, "1")
	_, err := p.GetQuotes(context.Background(), SwapRateRequest{
		TickerFrom: "ETH",
		TickerTo:   "USDC",
		AmountFrom: "0.1",
		Taker:      "not-an-address",
	})
	if err == nil {
		t.Error("expected error for invalid taker, got nil")
	}
}

// TestZeroXGetQuotes_WETHFallback_Arbitrum verifies that when the first 0x
// request fails with a sellToken Invalid ethereum address error the provider
// retries automatically with the chain-specific WETH address and succeeds.
func TestZeroXGetQuotes_WETHFallback_Arbitrum(t *testing.T) {
	const wethArbitrum = "0x82af49447d8a07e3bd95bd0d56f35241523fbab1"
	attempt := 0
	var secondSellToken string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt++
		q := r.URL.Query()
		if attempt == 1 {
			// First attempt: reject with sellToken Invalid ethereum address error
			w.WriteHeader(http.StatusBadRequest)
			errBody := fmt.Sprintf(`{"code":100,"reason":"VALIDATION_ERROR","validationErrors":[{"field":"sellToken","code":1000,"reason":"Invalid ethereum address"}]}`)
			_, _ = w.Write([]byte(errBody))
			return
		}
		// Second attempt (WETH fallback): return success
		secondSellToken = q.Get("sellToken")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(mockZeroXResponse{
			BuyAmount:  "500000000",
			SellAmount: "100000000000000000",
			Gas:        200000,
		})
	}))
	defer srv.Close()

	p := newZeroXProvider(srv.URL, "42161")
	quotes, err := p.GetQuotes(context.Background(), SwapRateRequest{
		TickerFrom: "ETH",
		TickerTo:   "USDC",
		AmountFrom: "0.1",
	})
	if err != nil {
		t.Fatalf("GetQuotes after WETH fallback: unexpected error: %v", err)
	}
	if attempt != 2 {
		t.Errorf("expected 2 attempts (initial + retry), got %d", attempt)
	}
	if secondSellToken != wethArbitrum {
		t.Errorf("retry used sellToken=%q, want WETH=%q", secondSellToken, wethArbitrum)
	}
	if len(quotes) != 1 || quotes[0].Provider != "0x" {
		t.Errorf("unexpected quotes: %+v", quotes)
	}
}

// TestZeroXGetQuotes_WETHFallback_BSC mirrors the Arbitrum test for BSC (56).
func TestZeroXGetQuotes_WETHFallback_BSC(t *testing.T) {
	const wethBSC = "0x2170Ed0880ac9A755fd29B2688956BD959F933F8"
	attempt := 0
	var secondSellToken string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt++
		if attempt == 1 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"validationErrors":[{"field":"sellToken","reason":"Invalid ethereum address"}]}`))
			return
		}
		secondSellToken = r.URL.Query().Get("sellToken")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(mockZeroXResponse{BuyAmount: "300000000", SellAmount: "100000000000000000", Gas: 0})
	}))
	defer srv.Close()

	p := newZeroXProvider(srv.URL, "56")
	_, err := p.GetQuotes(context.Background(), SwapRateRequest{
		TickerFrom: "ETH",
		TickerTo:   "USDC",
		AmountFrom: "0.1",
	})
	if err != nil {
		t.Fatalf("GetQuotes after WETH fallback on BSC: %v", err)
	}
	if attempt != 2 {
		t.Errorf("expected 2 attempts, got %d", attempt)
	}
	if secondSellToken != wethBSC {
		t.Errorf("retry used sellToken=%q, want WETH=%q", secondSellToken, wethBSC)
	}
}

// TestZeroXGetQuotes_WETHFallback_Base verifies WETH fallback on Base (8453).
func TestZeroXGetQuotes_WETHFallback_Base(t *testing.T) {
	const wethBase = "0x4200000000000000000000000000000000000006"
	attempt := 0
	var secondSellToken string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt++
		if attempt == 1 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"validationErrors":[{"field":"sellToken","reason":"Invalid ethereum address"}]}`))
			return
		}
		secondSellToken = r.URL.Query().Get("sellToken")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(mockZeroXResponse{BuyAmount: "400000000", SellAmount: "100000000000000000", Gas: 0})
	}))
	defer srv.Close()

	p := newZeroXProvider(srv.URL, "8453")
	_, err := p.GetQuotes(context.Background(), SwapRateRequest{
		TickerFrom: "ETH",
		TickerTo:   "USDC",
		AmountFrom: "0.1",
	})
	if err != nil {
		t.Fatalf("GetQuotes after WETH fallback on Base: %v", err)
	}
	if attempt != 2 {
		t.Errorf("expected 2 attempts, got %d", attempt)
	}
	if secondSellToken != wethBase {
		t.Errorf("retry used sellToken=%q, want WETH=%q", secondSellToken, wethBase)
	}
}

// TestZeroXGetQuotes_WETHFallback_Avalanche verifies WETH fallback on Avalanche (43114).
func TestZeroXGetQuotes_WETHFallback_Avalanche(t *testing.T) {
	const wethAvax = "0x49D5c2BdFfac6CE2BFdB6640F4F80f226bc10bAB"
	attempt := 0
	var secondSellToken string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt++
		if attempt == 1 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"validationErrors":[{"field":"sellToken","reason":"Invalid ethereum address"}]}`))
			return
		}
		secondSellToken = r.URL.Query().Get("sellToken")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(mockZeroXResponse{BuyAmount: "600000000", SellAmount: "100000000000000000", Gas: 0})
	}))
	defer srv.Close()

	p := newZeroXProvider(srv.URL, "43114")
	_, err := p.GetQuotes(context.Background(), SwapRateRequest{
		TickerFrom: "ETH",
		TickerTo:   "USDC",
		AmountFrom: "0.1",
	})
	if err != nil {
		t.Fatalf("GetQuotes after WETH fallback on Avalanche: %v", err)
	}
	if attempt != 2 {
		t.Errorf("expected 2 attempts, got %d", attempt)
	}
	if secondSellToken != wethAvax {
		t.Errorf("retry used sellToken=%q, want WETH=%q", secondSellToken, wethAvax)
	}
}

// TestZeroXGetQuotes_NonETH_NoFallback verifies that the WETH fallback is NOT
// triggered for non-ETH tokens, even if the error mentions sellToken.
func TestZeroXGetQuotes_NonETH_NoFallback(t *testing.T) {
	attempt := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt++
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"validationErrors":[{"field":"sellToken","reason":"Invalid ethereum address"}]}`))
	}))
	defer srv.Close()

	p := newZeroXProvider(srv.URL, "42161")
	_, err := p.GetQuotes(context.Background(), SwapRateRequest{
		TickerFrom: "DAI", // not ETH → no WETH fallback
		TickerTo:   "USDC",
		AmountFrom: "100",
	})
	if err == nil {
		t.Error("expected error for non-ETH token with invalid address, got nil")
	}
	if attempt != 1 {
		t.Errorf("expected 1 attempt (no retry for non-ETH), got %d", attempt)
	}
}

// TestZeroXGetQuotes_MissingAPIKey checks that missing API key returns early with an error.
func TestZeroXGetQuotes_MissingAPIKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("server should not be called when API key is missing")
	}))
	defer srv.Close()

	p := &ZeroXQuoteProvider{
		client:  &http.Client{},
		baseURL: srv.URL,
		apiKey:  "", // intentionally empty
		chainID: "1",
		taker:   defaultZeroXTaker,
	}
	_, err := p.GetQuotes(context.Background(), SwapRateRequest{
		TickerFrom: "ETH",
		TickerTo:   "USDC",
		AmountFrom: "0.1",
	})
	if err == nil {
		t.Error("expected error for missing API key, got nil")
	}
}

// ---------------------------------------------------------------------------
// test helper
// ---------------------------------------------------------------------------

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}())
}
