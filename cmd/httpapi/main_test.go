package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type requestIDCapturingProvider struct {
	gotID string
}

func TestOneInchGetQuotes_NonJSONResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<html><body>blocked</body></html>"))
	}))
	defer srv.Close()

	p := &OneInchQuoteProvider{client: &http.Client{}, baseURL: srv.URL}
	_, _, err := p.GetQuotes(context.Background(), SwapRateRequest{
		TickerFrom: "BTC",
		TickerTo:   "USDT",
		AmountFrom: "1",
		ChainID:    "1",
	})
	if err == nil {
		t.Fatal("expected non-json error, got nil")
	}
	errText := strings.ToLower(err.Error())
	if !strings.Contains(errText, "non-json response") {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(errText, "status=200") {
		t.Fatalf("expected status details in error, got: %v", err)
	}
	if !strings.Contains(errText, "content_type=\"text/html\"") {
		t.Fatalf("expected content-type details in error, got: %v", err)
	}
}
func TestOneInchGetQuotes_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-1inch-key" {
			t.Fatalf("expected Authorization header, got %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"srcAmount": "1000000000000000000",
			"dstAmount": "3000000000",
			"gas":       21000,
		})
	}))
	defer srv.Close()

	p := &OneInchQuoteProvider{client: &http.Client{}, baseURL: srv.URL, apiKey: "test-1inch-key"}
	quotes, warnings, err := p.GetQuotes(context.Background(), SwapRateRequest{
		TickerFrom: "BTC",
		TickerTo:   "USDT",
		AmountFrom: "1",
		ChainID:    "1",
	})
	if err != nil {
		t.Fatalf("GetQuotes unexpected error: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("expected no warnings, got %v", warnings)
	}
	if len(quotes) != 1 {
		t.Fatalf("quotes len=%d want=1", len(quotes))
	}
	if quotes[0].Provider != "1inch" {
		t.Fatalf("provider=%q want=1inch", quotes[0].Provider)
	}
	if quotes[0].AmountTo != "3000000000" {
		t.Fatalf("amount_to=%q want=3000000000", quotes[0].AmountTo)
	}
	if quotes[0].AmountFrom != "1000000000000000000" {
		t.Fatalf("amount_from=%q want=1000000000000000000", quotes[0].AmountFrom)
	}
}

func TestBuildProviderByName_OneInch(t *testing.T) {
	t.Setenv("SWAP_1INCH_API_KEY", "test-key-1inch")

	p, ok := buildProviderByName("1inch")
	if !ok {
		t.Fatal("buildProviderByName(1inch) returned ok=false")
	}
	oip, ok := p.(*OneInchQuoteProvider)
	if !ok {
		t.Fatalf("buildProviderByName(1inch) = %T, want *OneInchQuoteProvider", p)
	}
	if oip.apiKey != "test-key-1inch" {
		t.Fatalf("1inch apiKey=%q want=%q", oip.apiKey, "test-key-1inch")
	}
	if oip.client == nil {
		t.Fatal("1inch client is nil")
	}
}

func (p *requestIDCapturingProvider) GetQuotes(ctx context.Context, req SwapRateRequest) ([]Quote, []string, error) {
	p.gotID = requestIDFromContext(ctx)
	return []Quote{{Provider: "capture", AmountTo: "1"}}, nil, nil
}

type failingQuoteProvider struct{}

func (failingQuoteProvider) GetQuotes(ctx context.Context, req SwapRateRequest) ([]Quote, []string, error) {
	return nil, nil, errors.New("upstream failed")
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

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

func TestIsDebugEnabled(t *testing.T) {
	t.Setenv("SWAP_DEBUG", "1")
	if !isDebugEnabled() {
		t.Fatal("expected SWAP_DEBUG=1 to enable debug")
	}

	t.Setenv("SWAP_DEBUG", "true")
	if !isDebugEnabled() {
		t.Fatal("expected SWAP_DEBUG=true to enable debug")
	}

	t.Setenv("SWAP_DEBUG", "on")
	if !isDebugEnabled() {
		t.Fatal("expected SWAP_DEBUG=on to enable debug")
	}

	t.Setenv("SWAP_DEBUG", "0")
	if isDebugEnabled() {
		t.Fatal("expected SWAP_DEBUG=0 to disable debug")
	}

	t.Setenv("SWAP_DEBUG", "")
	if isDebugEnabled() {
		t.Fatal("expected empty SWAP_DEBUG to disable debug")
	}
}

func TestQuoteRequestDebugFields(t *testing.T) {
	req := SwapRateRequest{
		TickerFrom:  " BTC ",
		TickerTo:    " USDT ",
		AmountFrom:  " 1.25 ",
		ChainID:     " 1 ",
		NetworkFrom: " Mainnet ",
		NetworkTo:   " Arbitrum One ",
	}

	got := quoteRequestDebugFields(req)
	want := "from=BTC to=USDT amount_from=1.25 chain_id=1 network_from=Mainnet network_to=Arbitrum One"
	if got != want {
		t.Fatalf("quoteRequestDebugFields() = %q, want %q", got, want)
	}
}

func TestNewRequestIDFormat(t *testing.T) {
	id := newRequestID()
	if !strings.HasPrefix(id, "RQ-") {
		t.Fatalf("newRequestID() prefix mismatch: %q", id)
	}
	if len(id) != 15 {
		t.Fatalf("newRequestID() length=%d, want 15", len(id))
	}
}

func TestRequestIDContextRoundTrip(t *testing.T) {
	ctx := withRequestID(context.Background(), "RQ-abc123")
	if got := requestIDFromContext(ctx); got != "RQ-abc123" {
		t.Fatalf("requestIDFromContext() = %q, want RQ-abc123", got)
	}
}

func TestSwapRateHandler_AttachesRequestIDToContext(t *testing.T) {
	orig := quoteProvider
	cp := &requestIDCapturingProvider{}
	quoteProvider = cp
	defer func() { quoteProvider = orig }()

	body := bytes.NewBufferString(`{"ticker_from":"BTC","ticker_to":"USDT","amount_from":"1"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/swaprate", body)
	w := httptest.NewRecorder()

	swapRateHandler(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d want %d", resp.StatusCode, http.StatusOK)
	}
	if got := resp.Header.Get("X-Request-Id"); !strings.HasPrefix(got, "RQ-") {
		t.Fatalf("expected X-Request-Id header with RQ- prefix, got %q", got)
	}
	if !strings.HasPrefix(cp.gotID, "RQ-") {
		t.Fatalf("expected propagated request id prefix RQ-, got %q", cp.gotID)
	}
}

func TestSwapRateHandler_ErrorPath_IncludesRequestIDHeader(t *testing.T) {
	orig := quoteProvider
	quoteProvider = failingQuoteProvider{}
	defer func() { quoteProvider = orig }()

	body := bytes.NewBufferString(`{"ticker_from":"BTC","ticker_to":"USDT","amount_from":"1"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/swaprate", body)
	w := httptest.NewRecorder()

	swapRateHandler(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("status=%d want %d", resp.StatusCode, http.StatusBadGateway)
	}
	if got := resp.Header.Get("X-Request-Id"); !strings.HasPrefix(got, "RQ-") {
		t.Fatalf("expected X-Request-Id header with RQ- prefix, got %q", got)
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

func (s stubQuoteProvider) GetQuotes(ctx context.Context, req SwapRateRequest) ([]Quote, []string, error) {
	if s.delay > 0 {
		select {
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		case <-time.After(s.delay):
		}
	}
	if s.err != nil {
		return nil, nil, s.err
	}
	return s.quotes, nil, nil
}

func TestParseProviderList(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want []string
	}{
		{name: "comma", raw: "0x,1inch", want: []string{"0x", "1inch"}},
		{name: "comma with paraswap", raw: "0x,paraswap", want: []string{"0x", "paraswap"}},
		{name: "external aliases", raw: "0x,external:foo,external:bar", want: []string{"0x", "external:foo", "external:bar"}},
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

func TestChainIDFromNetwork(t *testing.T) {
	tests := []struct {
		name    string
		network string
		want    string
		ok      bool
	}{
		{name: "optimism", network: "Optimism", want: "10", ok: true},
		{name: "bep20", network: "BEP20", want: "56", ok: true},
		{name: "arbitrum", network: "Arbitrum One", want: "42161", ok: true},
		{name: "base", network: "base", want: "8453", ok: true},
		{name: "unknown", network: "Lightning", want: "", ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := chainIDFromNetwork(tt.network)
			if ok != tt.ok || got != tt.want {
				t.Fatalf("chainIDFromNetwork(%q)=(%q,%v) want=(%q,%v)", tt.network, got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestResolveChainIDUsesNetworkWhenMissingChainID(t *testing.T) {
	req := SwapRateRequest{NetworkFrom: "Optimism", NetworkTo: "Mainnet"}
	if got := resolveChainID(req, "1"); got != "10" {
		t.Fatalf("resolveChainID()=%q want=%q", got, "10")
	}

	req = SwapRateRequest{NetworkFrom: "", NetworkTo: "Base"}
	if got := resolveChainID(req, "1"); got != "8453" {
		t.Fatalf("resolveChainID()=%q want=%q", got, "8453")
	}

	req = SwapRateRequest{ChainID: "42161", NetworkFrom: "Optimism"}
	if got := resolveChainID(req, "1"); got != "42161" {
		t.Fatalf("resolveChainID()=%q want explicit=%q", got, "42161")
	}
}

func TestIsCoinSupportedByActiveProvidersUsesNetworkChain(t *testing.T) {
	t.Setenv("SWAP_QUOTE_PROVIDER", "0x")
	t.Setenv("SWAP_0X_CHAIN_ID", "1")

	if !isCoinSupportedByActiveProviders(Coin{Ticker: "btc", Network: "Optimism"}) {
		t.Fatalf("btc on Optimism should be supported via WBTC mapping on chain 10")
	}
	if isCoinSupportedByActiveProviders(Coin{Ticker: "xmr", Network: "Optimism"}) {
		t.Fatalf("xmr on Optimism should not be supported by 0x token map")
	}
	if isCoinSupportedByActiveProviders(Coin{Ticker: "btc", Network: "SOL"}) {
		t.Fatalf("btc on unsupported SOL network should not be supported by onchain providers")
	}
}

func TestResolveOnchainChainID(t *testing.T) {
	got, err := resolveOnchainChainID(SwapRateRequest{NetworkFrom: "Optimism"}, "1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "10" {
		t.Fatalf("resolveOnchainChainID Optimism=%q want=10", got)
	}

	_, err = resolveOnchainChainID(SwapRateRequest{NetworkFrom: "SOL"}, "1")
	if err == nil {
		t.Fatalf("expected error for unsupported network_from")
	}
	if !strings.Contains(err.Error(), "supported networks") || !strings.Contains(err.Error(), "chain_id") {
		t.Fatalf("unsupported network error should include guidance, got: %v", err)
	}

	got, err = resolveOnchainChainID(SwapRateRequest{}, "1")
	if err != nil || got != "1" {
		t.Fatalf("resolveOnchainChainID fallback=(%q,%v) want=(1,nil)", got, err)
	}
}

func TestSupportedNetworksForTickerByActiveProviders(t *testing.T) {
	t.Setenv("SWAP_QUOTE_PROVIDER", "0x,paraswap")

	networks := supportedNetworksForTickerByActiveProviders("btc")
	if len(networks) < 5 {
		t.Fatalf("expected multiple btc networks, got=%v", networks)
	}

	networks = supportedNetworksForTickerByActiveProviders("xmr")
	if len(networks) != 0 {
		t.Fatalf("xmr should not be supported by onchain providers, got=%v", networks)
	}
}

func TestExpandCoinAcrossSupportedNetworks(t *testing.T) {
	t.Setenv("SWAP_QUOTE_PROVIDER", "0x")

	coins := expandCoinAcrossSupportedNetworks("Bitcoin", "btc")
	if len(coins) == 0 {
		t.Fatalf("expected expanded coins for btc")
	}
	if coins[0].Ticker != "btc" {
		t.Fatalf("unexpected ticker in first coin: %s", coins[0].Ticker)
	}
	if coins[0].Minimum <= 0 || coins[0].Maximum <= 0 {
		t.Fatalf("unexpected limits in expanded coin: %+v", coins[0])
	}

	var hasMainnet bool
	var hasOptimism bool
	for _, c := range coins {
		if c.Network == "Mainnet" && c.Name == "Bitcoin" {
			hasMainnet = true
		}
		if c.Network == "Optimism" && strings.Contains(c.Name, "(Optimism)") {
			hasOptimism = true
		}
	}
	if !hasMainnet || !hasOptimism {
		t.Fatalf("expected mainnet and optimism name variants, got=%v", coins)
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
	quotes, warnings, err := mp.GetQuotes(context.Background(), SwapRateRequest{TickerFrom: "ETH", TickerTo: "USDC", AmountFrom: "0.1"})
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("GetQuotes unexpected error: %v", err)
	}
	if len(warnings) == 0 {
		t.Fatalf("expected warnings for partial failure, got none")
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

	quotes, warnings, err := mp.GetQuotes(context.Background(), SwapRateRequest{TickerFrom: "ETH", TickerTo: "USDC", AmountFrom: "0.1"})
	if err == nil {
		t.Fatalf("expected error when all providers fail, got nil with quotes=%v", quotes)
	}
	if len(warnings) == 0 {
		t.Fatalf("expected warnings when all providers fail")
	}
}

func TestMultiQuoteProvider_IgnoreCancellationPartialFailure(t *testing.T) {
	mp := &MultiQuoteProvider{providers: []namedQuoteProvider{
		{
			name: "0x",
			provider: stubQuoteProvider{
				quotes: []Quote{{Provider: "0x", AmountTo: "123"}},
			},
		},
		{name: "paraswap", provider: stubQuoteProvider{err: context.Canceled}},
	}}

	quotes, warnings, err := mp.GetQuotes(context.Background(), SwapRateRequest{TickerFrom: "ETH", TickerTo: "USDC", AmountFrom: "0.1"})
	if err != nil {
		t.Fatalf("GetQuotes unexpected error: %v", err)
	}
	if len(quotes) != 1 {
		t.Fatalf("GetQuotes returned %d quotes, want 1", len(quotes))
	}
	if len(warnings) != 0 {
		t.Fatalf("expected no warnings for cancellation partial failure, got %v", warnings)
	}
}

type cancelingQuoteProvider struct{}

func (cancelingQuoteProvider) GetQuotes(ctx context.Context, req SwapRateRequest) ([]Quote, []string, error) {
	return nil, nil, context.Canceled
}

func TestSwapRateHandler_CanceledRequest_NoGatewayError(t *testing.T) {
	orig := quoteProvider
	quoteProvider = cancelingQuoteProvider{}
	defer func() { quoteProvider = orig }()

	body := bytes.NewBufferString(`{"ticker_from":"BTC","ticker_to":"USDT","amount_from":"1"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/swaprate", body)
	w := httptest.NewRecorder()

	swapRateHandler(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d want %d", resp.StatusCode, http.StatusOK)
	}
	if got := resp.Header.Get("X-Request-Id"); !strings.HasPrefix(got, "RQ-") {
		t.Fatalf("expected X-Request-Id header with RQ- prefix, got %q", got)
	}
	if got := strings.TrimSpace(w.Body.String()); got != "" {
		t.Fatalf("expected empty body on canceled request, got %q", got)
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
	quotes, _, err := p.GetQuotes(context.Background(), SwapRateRequest{
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
	if quotes[0].AmountToUSD == "0" || quotes[0].AmountToUSD == "0.00" {
		t.Errorf("quote.AmountToUSD should be non-zero, got %q", quotes[0].AmountToUSD)
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
	_, _, err := p.GetQuotes(context.Background(), SwapRateRequest{
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
	_, _, err := p.GetQuotes(context.Background(), SwapRateRequest{
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
	_, _, err := p.GetQuotes(context.Background(), SwapRateRequest{
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
	quotes, _, err := p.GetQuotes(context.Background(), SwapRateRequest{
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
	_, _, err := p.GetQuotes(context.Background(), SwapRateRequest{
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
	_, _, err := p.GetQuotes(context.Background(), SwapRateRequest{
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
	_, _, err := p.GetQuotes(context.Background(), SwapRateRequest{
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
	_, _, err := p.GetQuotes(context.Background(), SwapRateRequest{
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
	_, _, err := p.GetQuotes(context.Background(), SwapRateRequest{
		TickerFrom: "ETH",
		TickerTo:   "USDC",
		AmountFrom: "0.1",
	})
	if err == nil {
		t.Error("expected error for missing API key, got nil")
	}
}

func TestBuildProviderByName_ParaSwap(t *testing.T) {
	p, ok := buildProviderByName("paraswap")
	if !ok {
		t.Fatal("buildProviderByName(paraswap) returned ok=false")
	}
	if _, ok := p.(*ParaSwapQuoteProvider); !ok {
		t.Fatalf("buildProviderByName(paraswap) = %T, want *ParaSwapQuoteProvider", p)
	}
}

func TestBuildProviderByName_OpenOcean(t *testing.T) {
	t.Setenv("SWAP_OPENOCEAN_URL", "https://openocean.example")
	t.Setenv("SWAP_OPENOCEAN_CHAIN_ID", "137")
	t.Setenv("SWAP_OPENOCEAN_TIMEOUT_MS", "2500")

	p, ok := buildProviderByName("openocean")
	if !ok {
		t.Fatal("buildProviderByName(openocean) returned ok=false")
	}
	oo, ok := p.(*OpenOceanQuoteProvider)
	if !ok {
		t.Fatalf("buildProviderByName(openocean) = %T, want *OpenOceanQuoteProvider", p)
	}
	if oo.baseURL != "https://openocean.example" {
		t.Fatalf("openocean baseURL=%q want=%q", oo.baseURL, "https://openocean.example")
	}
	if oo.chainID != "137" {
		t.Fatalf("openocean chainID=%q want=%q", oo.chainID, "137")
	}
	if oo.client == nil || oo.client.Timeout != 2500*time.Millisecond {
		t.Fatalf("openocean timeout=%v want=%v", oo.client.Timeout, 2500*time.Millisecond)
	}
}

func TestOpenOceanChainSlug(t *testing.T) {
	cases := []struct {
		chain string
		want  string
	}{
		{chain: "1", want: "eth"},
		{chain: "10", want: "optimism"},
		{chain: "137", want: "polygon"},
		{chain: "42161", want: "arbitrum"},
		{chain: "56", want: "bsc"},
		{chain: "8453", want: "base"},
		{chain: "43114", want: "avax"},
	}
	for _, tc := range cases {
		got, err := openOceanChainSlug(tc.chain)
		if err != nil {
			t.Fatalf("openOceanChainSlug(%q) unexpected error: %v", tc.chain, err)
		}
		if got != tc.want {
			t.Fatalf("openOceanChainSlug(%q)=%q want=%q", tc.chain, got, tc.want)
		}
	}
	if _, err := openOceanChainSlug("99999"); err == nil {
		t.Fatal("expected error for unsupported chain id")
	}
}

func TestOpenOceanGetQuotes_Success(t *testing.T) {
	var seenPath string
	var seenQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		seenQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 200,
			"data": map[string]any{
				"inAmount":     "1000000000000000000",
				"outAmount":    "300000000",
				"estimatedGas": "123456",
			},
		})
	}))
	defer srv.Close()

	p := &OpenOceanQuoteProvider{client: &http.Client{}, baseURL: srv.URL, chainID: "1"}
	quotes, warnings, err := p.GetQuotes(context.Background(), SwapRateRequest{
		TickerFrom: "ETH",
		TickerTo:   "USDT",
		AmountFrom: "1",
	})
	if err != nil {
		t.Fatalf("GetQuotes unexpected error: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("expected no warnings, got %v", warnings)
	}
	if len(quotes) != 1 {
		t.Fatalf("quotes len=%d want=1", len(quotes))
	}
	if quotes[0].Provider != "openocean" {
		t.Fatalf("provider=%q want=openocean", quotes[0].Provider)
	}
	if quotes[0].AmountTo != "300000000" {
		t.Fatalf("amount_to=%q want=300000000", quotes[0].AmountTo)
	}
	if !strings.Contains(seenPath, "/eth/swap") {
		t.Fatalf("seen path=%q want to contain /eth/swap", seenPath)
	}
	if !contains(seenQuery, "inTokenAddress=") || !contains(seenQuery, "outTokenAddress=") || !contains(seenQuery, "amount=1") {
		t.Fatalf("unexpected query=%q", seenQuery)
	}
}

func TestOpenOceanGetQuotes_ResultField(t *testing.T) {
	// OpenOcean fallback: when envelope.Data is empty, it tries envelope.Result
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 200,
			"result": map[string]any{
				"inAmount":     "500000000000000000",
				"outAmount":    "900000000",
				"estimatedGas": "50000",
			},
		})
	}))
	defer srv.Close()

	p := &OpenOceanQuoteProvider{client: &http.Client{}, baseURL: srv.URL, chainID: "1"}
	quotes, warnings, err := p.GetQuotes(context.Background(), SwapRateRequest{
		TickerFrom: "ETH",
		TickerTo:   "USDC",
		AmountFrom: "1",
	})
	if err != nil {
		t.Fatalf("GetQuotes unexpected error: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("expected no warnings, got %v", warnings)
	}
	if len(quotes) != 1 {
		t.Fatalf("quotes len=%d want=1", len(quotes))
	}
	if quotes[0].Provider != "openocean" {
		t.Fatalf("provider=%q want=openocean", quotes[0].Provider)
	}
	if quotes[0].AmountTo != "900000000" {
		t.Fatalf("amount_to=%q want=900000000", quotes[0].AmountTo)
	}
}

func TestOpenOceanGetQuotes_TopLevelPayload(t *testing.T) {
	// OpenOcean fallback: when both data and result are missing, it tries
	// unmarshalling the entire body as openOceanPayload.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"inAmount":     "2000000000000000000",
			"outAmount":    "600000000",
			"estimatedGas": "100000",
		})
	}))
	defer srv.Close()

	p := &OpenOceanQuoteProvider{client: &http.Client{}, baseURL: srv.URL, chainID: "10"}
	quotes, warnings, err := p.GetQuotes(context.Background(), SwapRateRequest{
		TickerFrom: "ETH",
		TickerTo:   "USDT",
		AmountFrom: "2",
	})
	if err != nil {
		t.Fatalf("GetQuotes unexpected error: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("expected no warnings, got %v", warnings)
	}
	if len(quotes) != 1 {
		t.Fatalf("quotes len=%d want=1", len(quotes))
	}
	if quotes[0].AmountTo != "600000000" {
		t.Fatalf("amount_to=%q want=600000000", quotes[0].AmountTo)
	}
}

func TestOpenOceanGetQuotes_OutTokenAmountFallback(t *testing.T) {
	// When outAmount is empty, falls back to outTokenAmount
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 200,
			"data": map[string]any{
				"inAmount":       "1000000000000000000",
				"outAmount":      "",
				"outTokenAmount": "400000000",
				"estimatedGas":   "80000",
			},
		})
	}))
	defer srv.Close()

	p := &OpenOceanQuoteProvider{client: &http.Client{}, baseURL: srv.URL, chainID: "1"}
	quotes, _, err := p.GetQuotes(context.Background(), SwapRateRequest{
		TickerFrom: "ETH",
		TickerTo:   "USDT",
		AmountFrom: "1",
	})
	if err != nil {
		t.Fatalf("GetQuotes unexpected error: %v", err)
	}
	if len(quotes) != 1 {
		t.Fatalf("quotes len=%d want=1", len(quotes))
	}
	if quotes[0].AmountTo != "400000000" {
		t.Fatalf("amount_to=%q want=400000000", quotes[0].AmountTo)
	}
}

func TestOpenOceanGetQuotes_InTokenAmountFallback(t *testing.T) {
	// When inAmount is empty, falls back to inTokenAmount
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 200,
			"data": map[string]any{
				"inAmount":      "",
				"inTokenAmount": "999999999999999999",
				"outAmount":     "300000000",
				"estimatedGas":  "123456",
			},
		})
	}))
	defer srv.Close()

	p := &OpenOceanQuoteProvider{client: &http.Client{}, baseURL: srv.URL, chainID: "1"}
	quotes, _, err := p.GetQuotes(context.Background(), SwapRateRequest{
		TickerFrom: "ETH",
		TickerTo:   "USDT",
		AmountFrom: "1",
	})
	if err != nil {
		t.Fatalf("GetQuotes unexpected error: %v", err)
	}
	if len(quotes) != 1 {
		t.Fatalf("quotes len=%d want=1", len(quotes))
	}
	if quotes[0].AmountFrom != "999999999999999999" {
		t.Fatalf("amount_from=%q want=999999999999999999", quotes[0].AmountFrom)
	}
}

func TestOpenOceanGetQuotes_Non200Status(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"code":502,"message":"upstream error"}`))
	}))
	defer srv.Close()

	p := &OpenOceanQuoteProvider{client: &http.Client{}, baseURL: srv.URL, chainID: "1"}
	_, _, err := p.GetQuotes(context.Background(), SwapRateRequest{
		TickerFrom: "ETH",
		TickerTo:   "USDT",
		AmountFrom: "1",
	})
	if err == nil {
		t.Fatal("expected error for non-200 status, got nil")
	}
	if !strings.Contains(err.Error(), "openocean quote error status=502") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestOpenOceanGetQuotes_ErrorCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code":    400,
			"message": "invalid token",
		})
	}))
	defer srv.Close()

	p := &OpenOceanQuoteProvider{client: &http.Client{}, baseURL: srv.URL, chainID: "1"}
	_, _, err := p.GetQuotes(context.Background(), SwapRateRequest{
		TickerFrom: "ETH",
		TickerTo:   "USDT",
		AmountFrom: "1",
	})
	if err == nil {
		t.Fatal("expected error for code=400, got nil")
	}
	if !strings.Contains(err.Error(), "openocean quote error code=400: invalid token") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestOpenOceanGetQuotes_NonJSONResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("rate limit exceeded"))
	}))
	defer srv.Close()

	p := &OpenOceanQuoteProvider{client: &http.Client{}, baseURL: srv.URL, chainID: "1"}
	_, _, err := p.GetQuotes(context.Background(), SwapRateRequest{
		TickerFrom: "ETH",
		TickerTo:   "USDT",
		AmountFrom: "1",
	})
	if err == nil {
		t.Fatal("expected error for non-json response, got nil")
	}
	if !strings.Contains(err.Error(), "non-json response") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestOpenOceanGetQuotes_UnsupportedChain(t *testing.T) {
	p := &OpenOceanQuoteProvider{client: &http.Client{}, baseURL: "http://unused", chainID: "99999"}
	_, _, err := p.GetQuotes(context.Background(), SwapRateRequest{
		TickerFrom: "ETH",
		TickerTo:   "USDT",
		AmountFrom: "1",
	})
	if err == nil {
		t.Fatal("expected error for unsupported chain, got nil")
	}
	if !strings.Contains(err.Error(), "unsupported chain id") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestOpenOceanGetQuotes_UnsupportedFromToken(t *testing.T) {
	p := &OpenOceanQuoteProvider{client: &http.Client{}, baseURL: "http://unused", chainID: "1"}
	_, _, err := p.GetQuotes(context.Background(), SwapRateRequest{
		TickerFrom: "UNKNOWN",
		TickerTo:   "USDT",
		AmountFrom: "1",
	})
	if err == nil {
		t.Fatal("expected error for unsupported from token, got nil")
	}
	if !strings.Contains(err.Error(), "unsupported from token") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestOpenOceanGetQuotes_MissingAmountFrom(t *testing.T) {
	p := &OpenOceanQuoteProvider{client: &http.Client{}, baseURL: "http://unused", chainID: "1"}
	_, _, err := p.GetQuotes(context.Background(), SwapRateRequest{
		TickerFrom: "ETH",
		TickerTo:   "USDT",
		AmountFrom: "",
	})
	if err == nil {
		t.Fatal("expected error for missing amount_from, got nil")
	}
}

func TestBuildProviderByName_OpenOcean_ApiKey(t *testing.T) {
	t.Setenv("SWAP_OPENOCEAN_URL", "https://openocean.example")
	t.Setenv("SWAP_OPENOCEAN_CHAIN_ID", "137")
	t.Setenv("SWAP_OPENOCEAN_API_KEY", "test-openocean-key")

	p, ok := buildProviderByName("openocean")
	if !ok {
		t.Fatal("buildProviderByName(openocean) returned ok=false")
	}
	oo, ok := p.(*OpenOceanQuoteProvider)
	if !ok {
		t.Fatalf("buildProviderByName(openocean) = %T, want *OpenOceanQuoteProvider", p)
	}
	if oo.apiKey != "test-openocean-key" {
		t.Fatalf("openocean apiKey=%q want=%q", oo.apiKey, "test-openocean-key")
	}
}

func TestOpenOceanGetQuotes_ChainSlugVariants(t *testing.T) {
	chains := map[string]string{
		"10":    "optimism",
		"137":   "polygon",
		"42161": "arbitrum",
		"56":    "bsc",
		"8453":  "base",
		"43114": "avax",
	}
	for chainID, expectedSlug := range chains {
		var seenSlug string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/"), "/")
			if len(parts) > 0 {
				seenSlug = parts[0]
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 200,
				"data": map[string]any{
					"inAmount":     "1000000000000000000",
					"outAmount":    "300000000",
					"estimatedGas": "123456",
				},
			})
		}))
		p := &OpenOceanQuoteProvider{client: &http.Client{}, baseURL: srv.URL, chainID: chainID}
		quotes, _, err := p.GetQuotes(context.Background(), SwapRateRequest{
			TickerFrom: "ETH",
			TickerTo:   "USDT",
			AmountFrom: "1",
		})
		if err != nil {
			t.Fatalf("GetQuotes chain=%q slug=%q unexpected error: %v", chainID, expectedSlug, err)
		}
		if seenSlug != expectedSlug {
			t.Fatalf("chain=%q: slug=%q want=%q", chainID, seenSlug, expectedSlug)
		}
		if len(quotes) != 1 || quotes[0].Provider != "openocean" {
			t.Fatalf("chain=%q: unexpected quotes=%+v", chainID, quotes)
		}
		srv.Close()
	}
}

func TestBuildProviderByName_ExternalAlias(t *testing.T) {
	t.Setenv("SWAP_QUOTE_URL_FOO", "https://quotes.foo.example")
	t.Setenv("SWAP_QUOTE_API_KEY_FOO", "abc123")
	t.Setenv("SWAP_QUOTE_PATH_FOO", "/custom-quote")
	t.Setenv("SWAP_QUOTE_TIMEOUT_MS_FOO", "2500")

	p, ok := buildProviderByName("external:foo")
	if !ok {
		t.Fatal("buildProviderByName(external:foo) returned ok=false")
	}
	ext, ok := p.(*ExternalQuoteProvider)
	if !ok {
		t.Fatalf("buildProviderByName(external:foo) = %T, want *ExternalQuoteProvider", p)
	}
	if ext.baseURL != "https://quotes.foo.example" {
		t.Fatalf("external baseURL=%q want=%q", ext.baseURL, "https://quotes.foo.example")
	}
	if ext.apiKey != "abc123" {
		t.Fatalf("external apiKey=%q want=%q", ext.apiKey, "abc123")
	}
	if ext.path != "/custom-quote" {
		t.Fatalf("external path=%q want=%q", ext.path, "/custom-quote")
	}
	if ext.client == nil || ext.client.Timeout != 2500*time.Millisecond {
		t.Fatalf("external timeout=%v want=%v", ext.client.Timeout, 2500*time.Millisecond)
	}
}

func TestBuildProviderByName_ExternalAliasMissingURL(t *testing.T) {
	p, ok := buildProviderByName("external:nope")
	if ok {
		t.Fatalf("buildProviderByName(external:nope) expected ok=false, got provider=%T", p)
	}
}

func TestNormalizeExternalQuotePath(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{in: "", want: "/quote"},
		{in: "quote", want: "/quote"},
		{in: "/quote", want: "/quote"},
		{in: "  /v2/price  ", want: "/v2/price"},
	}
	for _, tc := range cases {
		if got := normalizeExternalQuotePath(tc.in); got != tc.want {
			t.Fatalf("normalizeExternalQuotePath(%q)=%q want=%q", tc.in, got, tc.want)
		}
	}
}

func TestExternalHTTPTimeout(t *testing.T) {
	t.Setenv("SWAP_QUOTE_TIMEOUT_MS", "7000")
	if got := externalHTTPTimeout(""); got != 7*time.Second {
		t.Fatalf("externalHTTPTimeout(\"\")=%v want=%v", got, 7*time.Second)
	}

	t.Setenv("SWAP_QUOTE_TIMEOUT_MS_FOO", "900")
	if got := externalHTTPTimeout("SWAP_QUOTE_TIMEOUT_MS_FOO"); got != 900*time.Millisecond {
		t.Fatalf("externalHTTPTimeout(alias)=%v want=%v", got, 900*time.Millisecond)
	}

	t.Setenv("SWAP_QUOTE_TIMEOUT_MS_BAR", "oops")
	if got := externalHTTPTimeout("SWAP_QUOTE_TIMEOUT_MS_BAR"); got != 7*time.Second {
		t.Fatalf("externalHTTPTimeout(invalid alias)=%v want=%v", got, 7*time.Second)
	}
}

func TestExternalCircuitConfig_AliasOverride(t *testing.T) {
	t.Setenv("SWAP_QUOTE_CIRCUIT_FAILS", "5")
	t.Setenv("SWAP_QUOTE_CIRCUIT_COOLDOWN_MS", "9000")
	t.Setenv("SWAP_QUOTE_CIRCUIT_FAILS_FOO", "2")
	t.Setenv("SWAP_QUOTE_CIRCUIT_COOLDOWN_MS_FOO", "1500")

	fails, ttl := externalCircuitConfig("FOO")
	if fails != 2 {
		t.Fatalf("fails=%d want=2", fails)
	}
	if ttl != 1500*time.Millisecond {
		t.Fatalf("ttl=%v want=%v", ttl, 1500*time.Millisecond)
	}
}

func TestExternalCircuitStateFlow(t *testing.T) {
	externalCircuit.Lock()
	externalCircuit.state = make(map[string]externalCircuitState)
	externalCircuit.Unlock()

	key := "external:test"

	if err := externalCircuitBeforeAttempt(key, time.Now()); err != nil {
		t.Fatalf("unexpected pre-attempt error before failures: %v", err)
	}

	externalCircuitRecordFailure(key, 2, 3*time.Second, time.Now())
	if err := externalCircuitBeforeAttempt(key, time.Now()); err != nil {
		t.Fatalf("circuit should still be closed after first failure, got: %v", err)
	}

	externalCircuitRecordFailure(key, 2, 3*time.Second, time.Now())
	if err := externalCircuitBeforeAttempt(key, time.Now()); err == nil {
		t.Fatal("expected open circuit error after threshold failures")
	}

	// Simulate cooldown elapsed and allow exactly one half-open probe.
	externalCircuit.Lock()
	s := externalCircuit.state[key]
	s.openUntil = time.Now().Add(-time.Second)
	s.halfOpenInFlight = false
	externalCircuit.state[key] = s
	externalCircuit.Unlock()

	if err := externalCircuitBeforeAttempt(key, time.Now()); err != nil {
		t.Fatalf("expected first half-open probe to be allowed, got: %v", err)
	}
	if err := externalCircuitBeforeAttempt(key, time.Now()); err == nil {
		t.Fatal("expected second concurrent half-open attempt to be blocked")
	}

	// Failed half-open probe should reopen the circuit.
	externalCircuitRecordFailure(key, 2, 3*time.Second, time.Now())
	if err := externalCircuitBeforeAttempt(key, time.Now()); err == nil {
		t.Fatal("expected circuit reopened after failed half-open probe")
	}

	externalCircuitRecordSuccess(key)
	if err := externalCircuitBeforeAttempt(key, time.Now()); err != nil {
		t.Fatalf("circuit should be closed after success reset, got: %v", err)
	}
}

func TestExternalQuoteProvider_GetQuotes_UsesProviderPath(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(SwapRateResponse{
			Quotes: []Quote{{Provider: "ext", AmountTo: "10", AmountFrom: "1"}},
		})
	}))
	defer srv.Close()

	p := &ExternalQuoteProvider{
		client:  &http.Client{},
		baseURL: srv.URL,
		path:    "/v2/quote",
	}

	quotes, _, err := p.GetQuotes(context.Background(), SwapRateRequest{TickerFrom: "BTC", TickerTo: "USDT", AmountFrom: "1"})
	if err != nil {
		t.Fatalf("GetQuotes unexpected error: %v", err)
	}
	if gotPath != "/v2/quote" {
		t.Fatalf("request path=%q want=%q", gotPath, "/v2/quote")
	}
	if len(quotes) != 1 {
		t.Fatalf("quotes len=%d want=1", len(quotes))
	}
}

func TestParaSwapGetQuotes_Success(t *testing.T) {
	t.Setenv("SWAP_PARASWAP_CHAIN_ID", "1")

	var seenRawQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenRawQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"priceRoute":{"srcAmount":"100000000","destAmount":"300000000","gasCostUSD":"0.13"}}`))
	}))
	defer srv.Close()

	p := &ParaSwapQuoteProvider{
		client:  &http.Client{},
		baseURL: srv.URL,
	}

	quotes, _, err := p.GetQuotes(context.Background(), SwapRateRequest{
		TickerFrom: "BTC",
		TickerTo:   "USDT",
		AmountFrom: "1",
		ChainID:    "1",
	})
	if err != nil {
		t.Fatalf("GetQuotes: unexpected error: %v", err)
	}
	if len(quotes) != 1 {
		t.Fatalf("GetQuotes returned %d quotes, want 1", len(quotes))
	}
	if quotes[0].Provider != "paraswap" {
		t.Fatalf("provider=%q want paraswap", quotes[0].Provider)
	}
	if quotes[0].AmountTo != "300000000" {
		t.Fatalf("amount_to=%q want 300000000", quotes[0].AmountTo)
	}
	if quotes[0].AmountToUSD == "0" || quotes[0].AmountToUSD == "0.00" {
		t.Fatalf("amount_to_usd should be non-zero, got %q", quotes[0].AmountToUSD)
	}
	if !contains(seenRawQuery, "network=1") {
		t.Fatalf("expected network=1 in query, got %q", seenRawQuery)
	}
}

func TestParaSwapGetQuotes_UnsupportedToken(t *testing.T) {
	p := &ParaSwapQuoteProvider{client: &http.Client{}, baseURL: "http://unused"}
	_, _, err := p.GetQuotes(context.Background(), SwapRateRequest{
		TickerFrom: "DOGE",
		TickerTo:   "USDT",
		AmountFrom: "1",
		ChainID:    "1",
	})
	if err == nil {
		t.Fatal("expected unsupported token error, got nil")
	}
}

func TestGetUSDPriceMap_UsesStaleCacheOnFetchFailure(t *testing.T) {
	origClient := usdPriceClient
	origPrices := usdPriceCache.prices
	origUpdatedAt := usdPriceCache.updatedAt
	origExpiry := usdPriceCache.expiresAt
	defer func() {
		usdPriceClient = origClient
		usdPriceCache.prices = origPrices
		usdPriceCache.updatedAt = origUpdatedAt
		usdPriceCache.expiresAt = origExpiry
	}()

	usdPriceCache.prices = map[string]float64{"bitcoin": 100000}
	usdPriceCache.updatedAt = time.Now().Add(-8 * time.Minute)
	usdPriceCache.expiresAt = time.Now().Add(-time.Minute)
	usdPriceClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("network down")
	})}

	prices, stale, staleAge, err := getUSDPriceMap(context.Background())
	if err != nil {
		t.Fatalf("getUSDPriceMap unexpected error: %v", err)
	}
	if !stale {
		t.Fatal("expected stale=true when fetch fails and cache exists")
	}
	if staleAge < 1 {
		t.Fatalf("expected stale age >=1 minute, got %d", staleAge)
	}
	if prices["bitcoin"] != 100000 {
		t.Fatalf("expected stale bitcoin price, got %v", prices["bitcoin"])
	}
}

func TestComputeQuoteUSDAmounts_StaleWarning(t *testing.T) {
	origClient := usdPriceClient
	origPrices := usdPriceCache.prices
	origUpdatedAt := usdPriceCache.updatedAt
	origExpiry := usdPriceCache.expiresAt
	defer func() {
		usdPriceClient = origClient
		usdPriceCache.prices = origPrices
		usdPriceCache.updatedAt = origUpdatedAt
		usdPriceCache.expiresAt = origExpiry
	}()

	usdPriceCache.prices = map[string]float64{"bitcoin": 100000}
	usdPriceCache.updatedAt = time.Now().Add(-12 * time.Minute)
	usdPriceCache.expiresAt = time.Now().Add(-time.Minute)
	usdPriceClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("network down")
	})}

	toUSD, fromUSD, warnings := computeQuoteUSDAmounts(context.Background(), "BTC", "USDT", "100000000", "100000000000")
	if toUSD == "0.00" || fromUSD == "0.00" {
		t.Fatalf("expected non-zero usd values, got to=%s from=%s", toUSD, fromUSD)
	}
	found := false
	for _, w := range warnings {
		if strings.HasPrefix(w, usdPriceStaleWarning) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected stale warning prefix %q, got %v", usdPriceStaleWarning, warnings)
	}
}

func TestBuildProviderByName_Odos(t *testing.T) {
	t.Setenv("SWAP_ODOS_URL", "https://odos.example")
	t.Setenv("SWAP_ODOS_CHAIN_ID", "137")
	t.Setenv("SWAP_ODOS_TIMEOUT_MS", "3500")

	p, ok := buildProviderByName("odos")
	if !ok {
		t.Fatal("buildProviderByName(odos) returned ok=false")
	}
	od, ok := p.(*OdosQuoteProvider)
	if !ok {
		t.Fatalf("buildProviderByName(odos) = %T, want *OdosQuoteProvider", p)
	}
	if od.baseURL != "https://odos.example" {
		t.Fatalf("odos baseURL=%q want=%q", od.baseURL, "https://odos.example")
	}
	if od.chainID != "137" {
		t.Fatalf("odos chainID=%q want=%q", od.chainID, "137")
	}
	if od.client == nil || od.client.Timeout != 3500*time.Millisecond {
		t.Fatalf("odos timeout=%v want=%v", od.client.Timeout, 3500*time.Millisecond)
	}
}

func TestOdosChainID(t *testing.T) {
	cases := []struct {
		chainID string
		want    int
		wantErr bool
	}{
		{chainID: "1", want: 1, wantErr: false},
		{chainID: "137", want: 137, wantErr: false},
		{chainID: "42161", want: 42161, wantErr: false},
		{chainID: "invalid", want: 0, wantErr: true},
		{chainID: "-1", want: 0, wantErr: true},
	}
	for _, tc := range cases {
		got, err := odosChainID(tc.chainID)
		if tc.wantErr {
			if err == nil {
				t.Fatalf("odosChainID(%q) expected error, got %d", tc.chainID, got)
			}
		} else {
			if err != nil {
				t.Fatalf("odosChainID(%q) unexpected error: %v", tc.chainID, err)
			}
			if got != tc.want {
				t.Fatalf("odosChainID(%q)=%d want=%d", tc.chainID, got, tc.want)
			}
		}
	}
}

func TestOdosGetQuotes_Success(t *testing.T) {
	var seenBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("odos expected POST, got %s", r.Method)
		}
		var reqPayload map[string]any
		_ = json.NewDecoder(r.Body).Decode(&reqPayload)
		seenBody = fmt.Sprintf("%v", reqPayload)

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"statusCode":  200,
			"description": "ok",
			"quote": map[string]any{
				"inAmounts":   []string{"1000000000000000000"},
				"outAmounts":  []string{"2500000000"},
				"gasEstimate": "150000",
				"outTokens":   []string{"0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"},
			},
		})
	}))
	defer srv.Close()

	p := &OdosQuoteProvider{client: &http.Client{}, baseURL: srv.URL, chainID: "1"}
	quotes, warnings, err := p.GetQuotes(context.Background(), SwapRateRequest{
		TickerFrom: "ETH",
		TickerTo:   "USDT",
		AmountFrom: "1",
	})
	if err != nil {
		t.Fatalf("GetQuotes unexpected error: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("expected no warnings, got %v", warnings)
	}
	if len(quotes) != 1 {
		t.Fatalf("quotes len=%d want=1", len(quotes))
	}
	if quotes[0].Provider != "odos" {
		t.Fatalf("provider=%q want=odos", quotes[0].Provider)
	}
	if quotes[0].AmountTo != "2500000000" {
		t.Fatalf("amount_to=%q want=2500000000", quotes[0].AmountTo)
	}
	if !contains(seenBody, "chainId") {
		t.Fatalf("request body missing chainId: %q", seenBody)
	}
}

func TestOdosGetQuotes_Non200Status(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"statusCode":502,"description":"upstream error"}`))
	}))
	defer srv.Close()

	p := &OdosQuoteProvider{client: &http.Client{}, baseURL: srv.URL, chainID: "1"}
	_, _, err := p.GetQuotes(context.Background(), SwapRateRequest{
		TickerFrom: "ETH",
		TickerTo:   "USDC",
		AmountFrom: "1",
	})
	if err == nil {
		t.Fatal("expected error for non-200 HTTP status, got nil")
	}
}

func TestOdosGetQuotes_NonJSONResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("service unavailable"))
	}))
	defer srv.Close()

	p := &OdosQuoteProvider{client: &http.Client{}, baseURL: srv.URL, chainID: "1"}
	_, _, err := p.GetQuotes(context.Background(), SwapRateRequest{
		TickerFrom: "ETH",
		TickerTo:   "USDC",
		AmountFrom: "1",
	})
	if err == nil {
		t.Fatal("expected error for non-json response, got nil")
	}
	if !strings.Contains(err.Error(), "non-json response") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestOdosGetQuotes_ErrorStatusCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"statusCode":  400,
			"description": "invalid request",
		})
	}))
	defer srv.Close()

	p := &OdosQuoteProvider{client: &http.Client{}, baseURL: srv.URL, chainID: "1"}
	_, _, err := p.GetQuotes(context.Background(), SwapRateRequest{
		TickerFrom: "ETH",
		TickerTo:   "USDC",
		AmountFrom: "1",
	})
	if err == nil {
		t.Fatal("expected error for statusCode=400, got nil")
	}
	if !strings.Contains(err.Error(), "odos quote error code=400: invalid request") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestOdosGetQuotes_MissingOutAmounts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"statusCode": 200,
			"quote": map[string]any{
				"inAmounts":        []string{"1000000000000000000"},
				"outAmounts":       []string{},
				"gasEstimate":      "123456",
				"gasEstimateValue": 0.123,
			},
		})
	}))
	defer srv.Close()

	p := &OdosQuoteProvider{client: &http.Client{}, baseURL: srv.URL, chainID: "1"}
	_, _, err := p.GetQuotes(context.Background(), SwapRateRequest{
		TickerFrom: "ETH",
		TickerTo:   "USDC",
		AmountFrom: "1",
	})
	if err == nil {
		t.Fatal("expected error for missing outAmount, got nil")
	}
	if !strings.Contains(err.Error(), "missing outAmount") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestOdosGetQuotes_EmptyInAmounts(t *testing.T) {
	// When InAmounts is empty, it should fallback to req.AmountFrom
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"statusCode": 200,
			"quote": map[string]any{
				"inAmounts":        []string{},
				"outAmounts":       []string{"500000000"},
				"gasEstimate":      "123456",
				"gasEstimateValue": 0.123,
			},
		})
	}))
	defer srv.Close()

	p := &OdosQuoteProvider{client: &http.Client{}, baseURL: srv.URL, chainID: "1"}
	quotes, _, err := p.GetQuotes(context.Background(), SwapRateRequest{
		TickerFrom: "ETH",
		TickerTo:   "USDC",
		AmountFrom: "2",
	})
	if err != nil {
		t.Fatalf("GetQuotes unexpected error: %v", err)
	}
	if len(quotes) != 1 {
		t.Fatalf("quotes len=%d want=1", len(quotes))
	}
	if quotes[0].AmountFrom != "2" {
		t.Fatalf("amount_from=%q want=2 (fallback to req.AmountFrom)", quotes[0].AmountFrom)
	}
}

func TestOdosGetQuotes_WasteField(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"statusCode": 200,
			"quote": map[string]any{
				"inAmounts":        []string{"1000000000000000000"},
				"outAmounts":       []string{"2500000000"},
				"gasEstimate":      "987654",
				"gasEstimateValue": 0.987,
			},
		})
	}))
	defer srv.Close()

	p := &OdosQuoteProvider{client: &http.Client{}, baseURL: srv.URL, chainID: "1"}
	quotes, _, err := p.GetQuotes(context.Background(), SwapRateRequest{
		TickerFrom: "ETH",
		TickerTo:   "USDC",
		AmountFrom: "1",
	})
	if err != nil {
		t.Fatalf("GetQuotes unexpected error: %v", err)
	}
	if len(quotes) != 1 {
		t.Fatalf("quotes len=%d want=1", len(quotes))
	}
	if quotes[0].Waste != "987654" {
		t.Fatalf("waste=%q want=987654", quotes[0].Waste)
	}
}

func TestOdosGetQuotes_UnsupportedChain(t *testing.T) {
	p := &OdosQuoteProvider{client: &http.Client{}, baseURL: "http://unused", chainID: "99999"}
	_, _, err := p.GetQuotes(context.Background(), SwapRateRequest{
		TickerFrom: "ETH",
		TickerTo:   "USDC",
		AmountFrom: "1",
	})
	if err == nil {
		t.Fatal("expected error for unsupported chain, got nil")
	}
	if !strings.Contains(err.Error(), "unsupported chain id") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestOdosGetQuotes_UnsupportedFromToken(t *testing.T) {
	p := &OdosQuoteProvider{client: &http.Client{}, baseURL: "http://unused", chainID: "1"}
	_, _, err := p.GetQuotes(context.Background(), SwapRateRequest{
		TickerFrom: "UNKNOWN",
		TickerTo:   "USDC",
		AmountFrom: "1",
	})
	if err == nil {
		t.Fatal("expected error for unsupported from token, got nil")
	}
}

func TestOdosGetQuotes_MissingAmountFrom(t *testing.T) {
	p := &OdosQuoteProvider{client: &http.Client{}, baseURL: "http://unused", chainID: "1"}
	_, _, err := p.GetQuotes(context.Background(), SwapRateRequest{
		TickerFrom: "ETH",
		TickerTo:   "USDC",
		AmountFrom: "",
	})
	if err == nil {
		t.Fatal("expected error for missing amount_from, got nil")
	}
}

func TestOdosGetQuotes_SendsAPIKeyHeader(t *testing.T) {
	var seenAuthHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenAuthHeader = r.Header.Get("X-API-Key")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"statusCode": 200,
			"quote": map[string]any{
				"inAmounts":        []string{"1000000000000000000"},
				"outAmounts":       []string{"500000000"},
				"gasEstimate":      "123456",
				"gasEstimateValue": 0.123,
			},
		})
	}))
	defer srv.Close()

	p := &OdosQuoteProvider{client: &http.Client{}, baseURL: srv.URL, chainID: "1", apiKey: "test-odos-key"}
	_, _, err := p.GetQuotes(context.Background(), SwapRateRequest{
		TickerFrom: "ETH",
		TickerTo:   "USDC",
		AmountFrom: "1",
	})
	if err != nil {
		t.Fatalf("GetQuotes unexpected error: %v", err)
	}
	if seenAuthHeader != "test-odos-key" {
		t.Fatalf("Authorization header=%q want=%q", seenAuthHeader, "test-odos-key")
	}
}

func TestOdosGetQuotes_ChainIDVariants(t *testing.T) {
	chains := map[string]int{
		"1":     1,
		"10":    10,
		"137":   137,
		"42161": 42161,
		"56":    56,
	}
	for chainID, expectedNum := range chains {
		var seenChainID int
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if cn, ok := body["chainId"]; ok {
				if f, ok := cn.(float64); ok {
					seenChainID = int(f)
				}
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"statusCode": 200,
				"quote": map[string]any{
					"inAmounts":        []string{"1000000000000000000"},
					"outAmounts":       []string{"300000000"},
					"gasEstimate":      "123456",
					"gasEstimateValue": 0.123,
				},
			})
		}))
		p := &OdosQuoteProvider{client: &http.Client{}, baseURL: srv.URL, chainID: chainID}
		quotes, _, err := p.GetQuotes(context.Background(), SwapRateRequest{
			TickerFrom: "ETH",
			TickerTo:   "USDT",
			AmountFrom: "1",
		})
		if err != nil {
			t.Fatalf("GetQuotes chain=%q unexpected error: %v", chainID, err)
		}
		if seenChainID != expectedNum {
			t.Fatalf("chain=%q: request chainId=%d want=%d", chainID, seenChainID, expectedNum)
		}
		if len(quotes) != 1 || quotes[0].Provider != "odos" {
			t.Fatalf("chain=%q: unexpected quotes=%+v", chainID, quotes)
		}
		srv.Close()
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
