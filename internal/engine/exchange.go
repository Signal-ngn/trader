package engine

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/Signal-ngn/trader/internal/config"
	"github.com/Signal-ngn/trader/internal/domain"
)

// OpenPositionRequest contains the parameters for opening a position.
type OpenPositionRequest struct {
	Symbol   string
	Side     domain.PositionSide // "long" or "short"
	SizeUSD  float64
	Leverage int
	Price    float64 // signal price (used by NoopExchange)
}

// ClosePositionRequest contains the parameters for closing a position.
type ClosePositionRequest struct {
	Symbol     string
	Side       domain.PositionSide // position side to close
	MarketType domain.MarketType
}

// OrderResult contains the fill details from an exchange order.
type OrderResult struct {
	FillPrice float64
	Quantity  float64
	Fee       float64
	Margin    float64
}

// Exchange is the interface over exchange APIs.
// Paper mode uses NoopExchange; live mode uses BinanceFuturesExchange.
type Exchange interface {
	OpenPosition(ctx context.Context, req OpenPositionRequest) (*OrderResult, error)
	ClosePosition(ctx context.Context, req ClosePositionRequest) (*OrderResult, error)
	GetBalance(ctx context.Context) (float64, error)
}

// ──────────────────────────────────────────────────────────────────────────────
// NoopExchange — paper mode
// ──────────────────────────────────────────────────────────────────────────────

// NoopExchange returns synthetic fills at signal price with zero fees.
type NoopExchange struct {
	cfg *config.Config
}

// NewNoopExchange creates a new NoopExchange.
func NewNoopExchange(cfg *config.Config) *NoopExchange {
	return &NoopExchange{cfg: cfg}
}

func (n *NoopExchange) OpenPosition(_ context.Context, req OpenPositionRequest) (*OrderResult, error) {
	qty := 0.0
	if req.Price > 0 {
		qty = req.SizeUSD / req.Price
	}
	margin := req.SizeUSD
	if req.Leverage > 1 {
		margin = req.SizeUSD / float64(req.Leverage)
	}
	return &OrderResult{
		FillPrice: req.Price,
		Quantity:  qty,
		Fee:       0,
		Margin:    margin,
	}, nil
}

func (n *NoopExchange) ClosePosition(_ context.Context, req ClosePositionRequest) (*OrderResult, error) {
	// For NoopExchange, the price comes from the position evaluation — return 0 to use caller's price.
	return &OrderResult{
		FillPrice: 0,
		Quantity:  0,
		Fee:       0,
	}, nil
}

func (n *NoopExchange) GetBalance(_ context.Context) (float64, error) {
	return n.cfg.PortfolioSize, nil
}

// ──────────────────────────────────────────────────────────────────────────────
// BinanceFuturesExchange — live mode
// ──────────────────────────────────────────────────────────────────────────────

// BinanceFuturesExchange calls the Binance Futures API.
// Uses the go-binance/v2 SDK.
type BinanceFuturesExchange struct {
	cfg    *config.Config
	client binanceFuturesClient
}

// binanceFuturesClient is the internal interface over the Binance Futures HTTP API.
// Its methods are unexported because they're implementation details — callers use
// the Exchange interface. Tests within package engine can implement this directly.
// Tests outside the package should use the Exchange interface with a mock Exchange.
type binanceFuturesClient interface {
	newOrder(ctx context.Context, symbol, side, positionSide, orderType, quantity string) (*binanceOrderResult, error)
	setLeverage(ctx context.Context, symbol string, leverage int) error
	getBalance(ctx context.Context) (float64, error)
	getPositionQty(ctx context.Context, symbol string) (float64, error)
}

type binanceOrderResult struct {
	AvgPrice float64
	Quantity float64
	Fee      float64
}

// NewBinanceFuturesExchange creates a new Binance Futures adapter using the
// default HTTP client. The client can be swapped for testing via WithClient.
func NewBinanceFuturesExchange(cfg *config.Config) *BinanceFuturesExchange {
	return &BinanceFuturesExchange{
		cfg:    cfg,
		client: newBinanceHTTPClient(cfg),
	}
}

// WithClient returns a shallow copy of BinanceFuturesExchange with the given
// client substituted. Intended for intra-package tests (package engine) that
// need to inject a mock binanceFuturesClient without hitting the real API.
//
// Example (inside package engine):
//
//	ex := NewBinanceFuturesExchange(cfg).WithClient(&mockBinanceClient{...})
func (b *BinanceFuturesExchange) WithClient(c binanceFuturesClient) *BinanceFuturesExchange {
	cp := *b
	cp.client = c
	return &cp
}

func (b *BinanceFuturesExchange) OpenPosition(ctx context.Context, req OpenPositionRequest) (*OrderResult, error) {
	// Set leverage first.
	if req.Leverage > 0 {
		if err := b.withRetry(ctx, func(ctx context.Context) error {
			return b.client.setLeverage(ctx, binanceSymbol(req.Symbol), req.Leverage)
		}); err != nil {
			return nil, fmt.Errorf("set leverage: %w", err)
		}
	}

	side := "BUY"
	positionSide := "LONG"
	if req.Side == domain.PositionSideShort {
		side = "SELL"
		positionSide = "SHORT"
	}

	qty := "0"
	if req.Price > 0 && req.SizeUSD > 0 {
		qty = fmt.Sprintf("%.6f", req.SizeUSD/req.Price)
	}

	var result *binanceOrderResult
	if err := b.withRetry(ctx, func(ctx context.Context) error {
		var err error
		result, err = b.client.newOrder(ctx, binanceSymbol(req.Symbol), side, positionSide, "MARKET", qty)
		return err
	}); err != nil {
		return nil, fmt.Errorf("binance open position: %w", err)
	}

	margin := result.AvgPrice * result.Quantity
	if req.Leverage > 1 {
		margin /= float64(req.Leverage)
	}

	return &OrderResult{
		FillPrice: result.AvgPrice,
		Quantity:  result.Quantity,
		Fee:       result.Fee,
		Margin:    margin,
	}, nil
}

func (b *BinanceFuturesExchange) ClosePosition(ctx context.Context, req ClosePositionRequest) (*OrderResult, error) {
	// Fetch actual open quantity from Binance to avoid partial-close mismatches.
	var openQty float64
	if err := b.withRetry(ctx, func(ctx context.Context) error {
		var err error
		openQty, err = b.client.getPositionQty(ctx, binanceSymbol(req.Symbol))
		return err
	}); err != nil {
		return nil, fmt.Errorf("get open position qty: %w", err)
	}
	if openQty <= 0 {
		return nil, fmt.Errorf("no open position for %s on Binance", req.Symbol)
	}

	// Close side is opposite of open side.
	closeSide := "SELL"
	closePosSide := "LONG"
	if req.Side == domain.PositionSideShort {
		closeSide = "BUY"
		closePosSide = "SHORT"
	}

	qtyStr := fmt.Sprintf("%.6f", openQty)
	var result *binanceOrderResult
	if err := b.withRetry(ctx, func(ctx context.Context) error {
		var err error
		result, err = b.client.newOrder(ctx, binanceSymbol(req.Symbol), closeSide, closePosSide, "MARKET", qtyStr)
		return err
	}); err != nil {
		return nil, fmt.Errorf("binance close position: %w", err)
	}

	return &OrderResult{
		FillPrice: result.AvgPrice,
		Quantity:  result.Quantity,
		Fee:       result.Fee,
	}, nil
}

func (b *BinanceFuturesExchange) GetBalance(ctx context.Context) (float64, error) {
	var balance float64
	if err := b.withRetry(ctx, func(ctx context.Context) error {
		var err error
		balance, err = b.client.getBalance(ctx)
		return err
	}); err != nil {
		return 0, err
	}
	return balance, nil
}

// withRetry retries the function once after 1 second on a 429 rate-limit error.
type binanceRateLimitError struct{}

func (e *binanceRateLimitError) Error() string { return "binance rate limit (429)" }

func (b *BinanceFuturesExchange) withRetry(ctx context.Context, fn func(context.Context) error) error {
	err := fn(ctx)
	if err == nil {
		return nil
	}
	// Retry once on rate limit.
	if _, ok := err.(*binanceRateLimitError); ok {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
		return fn(ctx)
	}
	return err
}

// binanceSymbol converts a product ID like "BTC-USD" to Binance symbol "BTCUSDT".
func binanceSymbol(product string) string {
	// Simple heuristic: remove hyphens and replace trailing USD with USDT.
	for i := len(product) - 1; i >= 0; i-- {
		if product[i] == '-' {
			base := product[:i]
			quote := product[i+1:]
			if quote == "USD" {
				quote = "USDT"
			}
			return base + quote
		}
	}
	return product
}

// ──────────────────────────────────────────────────────────────────────────────
// binanceHTTPClient — raw HTTP implementation
// ──────────────────────────────────────────────────────────────────────────────

type binanceHTTPClient struct {
	cfg        *config.Config
	httpClient *http.Client
}

func newBinanceHTTPClient(cfg *config.Config) *binanceHTTPClient {
	return &binanceHTTPClient{
		cfg:        cfg,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

// The Binance Futures REST API requires HMAC-SHA256 signing.
// We implement a minimal client using net/http rather than pulling in the full SDK.

const binanceFuturesBaseURL = "https://fapi.binance.com"

func (c *binanceHTTPClient) newOrder(ctx context.Context, symbol, side, positionSide, orderType, quantity string) (*binanceOrderResult, error) {
	params := fmt.Sprintf("symbol=%s&side=%s&positionSide=%s&type=%s&quantity=%s&timestamp=%d",
		symbol, side, positionSide, orderType, quantity, time.Now().UnixMilli())
	sig := hmacSHA256(c.cfg.BinanceAPISecret, params)
	url := fmt.Sprintf("%s/fapi/v1/order?%s&signature=%s", binanceFuturesBaseURL, params, sig)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-MBX-APIKEY", c.cfg.BinanceAPIKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, &binanceRateLimitError{}
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("binance order API returned %d", resp.StatusCode)
	}

	var body struct {
		AvgPrice string `json:"avgPrice"`
		OrigQty  string `json:"origQty"`
		Status   string `json:"status"`
	}
	if err := decodeJSON(resp.Body, &body); err != nil {
		return nil, err
	}

	result := &binanceOrderResult{}
	fmt.Sscanf(body.AvgPrice, "%f", &result.AvgPrice)
	fmt.Sscanf(body.OrigQty, "%f", &result.Quantity)
	return result, nil
}

func (c *binanceHTTPClient) setLeverage(ctx context.Context, symbol string, leverage int) error {
	params := fmt.Sprintf("symbol=%s&leverage=%d&timestamp=%d", symbol, leverage, time.Now().UnixMilli())
	sig := hmacSHA256(c.cfg.BinanceAPISecret, params)
	url := fmt.Sprintf("%s/fapi/v1/leverage?%s&signature=%s", binanceFuturesBaseURL, params, sig)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-MBX-APIKEY", c.cfg.BinanceAPIKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		return &binanceRateLimitError{}
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("binance leverage API returned %d", resp.StatusCode)
	}
	return nil
}

func (c *binanceHTTPClient) getBalance(ctx context.Context) (float64, error) {
	params := fmt.Sprintf("timestamp=%d", time.Now().UnixMilli())
	sig := hmacSHA256(c.cfg.BinanceAPISecret, params)
	url := fmt.Sprintf("%s/fapi/v2/balance?%s&signature=%s", binanceFuturesBaseURL, params, sig)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("X-MBX-APIKEY", c.cfg.BinanceAPIKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		return 0, &binanceRateLimitError{}
	}
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("binance balance API returned %d", resp.StatusCode)
	}

	var accounts []struct {
		Asset           string `json:"asset"`
		AvailableBalance string `json:"availableBalance"`
	}
	if err := decodeJSON(resp.Body, &accounts); err != nil {
		return 0, err
	}
	for _, a := range accounts {
		if a.Asset == "USDT" {
			var balance float64
			fmt.Sscanf(a.AvailableBalance, "%f", &balance)
			return balance, nil
		}
	}
	return 0, fmt.Errorf("USDT balance not found in Binance response")
}

func (c *binanceHTTPClient) getPositionQty(ctx context.Context, symbol string) (float64, error) {
	params := fmt.Sprintf("symbol=%s&timestamp=%d", symbol, time.Now().UnixMilli())
	sig := hmacSHA256(c.cfg.BinanceAPISecret, params)
	url := fmt.Sprintf("%s/fapi/v2/positionRisk?%s&signature=%s", binanceFuturesBaseURL, params, sig)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("X-MBX-APIKEY", c.cfg.BinanceAPIKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		return 0, &binanceRateLimitError{}
	}
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("binance position risk API returned %d", resp.StatusCode)
	}

	var positions []struct {
		Symbol           string `json:"symbol"`
		PositionAmt      string `json:"positionAmt"`
		PositionSide     string `json:"positionSide"`
	}
	if err := decodeJSON(resp.Body, &positions); err != nil {
		return 0, err
	}
	for _, p := range positions {
		if p.Symbol == symbol {
			var qty float64
			fmt.Sscanf(p.PositionAmt, "%f", &qty)
			if qty < 0 {
				qty = -qty // absolute value
			}
			if qty > 0 {
				return qty, nil
			}
		}
	}
	return 0, nil
}
