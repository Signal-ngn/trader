// Package platform provides the Signal ngn platform API client used by the
// trader engine and the CLI.
package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// APIError wraps a non-2xx API response.
type APIError struct {
	StatusCode int
	Body       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("platform API error %d: %s", e.StatusCode, e.Body)
}

// PlatformClient wraps the Signal ngn platform HTTP API.
// It does not require a tenant_id — auth is via Bearer SN_API_KEY.
type PlatformClient struct {
	APIURL       string
	IngestionURL string
	APIKey       string
	http         *http.Client
}

// New creates a PlatformClient with the given API URL and key.
func New(apiURL, apiKey string) *PlatformClient {
	return &PlatformClient{
		APIURL:  apiURL,
		APIKey:  apiKey,
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

// NewWithIngestion creates a PlatformClient with both API and ingestion URLs.
// Used by the CLI which talks to both surfaces.
func NewWithIngestion(apiURL, ingestionURL, apiKey string) *PlatformClient {
	return &PlatformClient{
		APIURL:       apiURL,
		IngestionURL: ingestionURL,
		APIKey:       apiKey,
		http:         &http.Client{Timeout: 30 * time.Second},
	}
}

// platformDo performs an HTTP request against the platform API surfaces.
// API server requests receive Bearer api_key auth.
// Ingestion server mutating requests receive no Bearer token.
func (c *PlatformClient) platformDo(method, rawURL string, body any, out any) error {
	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}
		reqBody = bytes.NewReader(b)
	}

	req, err := http.NewRequest(method, rawURL, reqBody)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")

	isIngestion := c.IngestionURL != "" && len(rawURL) >= len(c.IngestionURL) &&
		rawURL[:len(c.IngestionURL)] == c.IngestionURL
	if !isIngestion && c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("request: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &APIError{StatusCode: resp.StatusCode, Body: string(respBytes)}
	}

	if out != nil && len(respBytes) > 0 {
		if err := json.Unmarshal(respBytes, out); err != nil {
			return fmt.Errorf("decode response: %w\nbody: %s", err, string(respBytes))
		}
	}
	return nil
}

// apiURL builds a URL against the API server.
func (c *PlatformClient) apiURL(path string, params ...url.Values) string {
	u := c.APIURL + path
	if len(params) > 0 && params[0] != nil {
		q := params[0].Encode()
		if q != "" {
			u += "?" + q
		}
	}
	return u
}

// ingestionURL builds a URL against the ingestion server.
func (c *PlatformClient) ingestionURL(path string, params ...url.Values) string {
	u := c.IngestionURL + path
	if len(params) > 0 && params[0] != nil {
		q := params[0].Encode()
		if q != "" {
			u += "?" + q
		}
	}
	return u
}

// Get performs a GET request and unmarshals the response.
func (c *PlatformClient) Get(rawURL string, out any) error {
	return c.platformDo(http.MethodGet, rawURL, nil, out)
}

// GetRaw performs a GET and returns the raw bytes.
func (c *PlatformClient) GetRaw(rawURL string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	isIngestion := c.IngestionURL != "" && len(rawURL) >= len(c.IngestionURL) &&
		rawURL[:len(c.IngestionURL)] == c.IngestionURL
	if !isIngestion && c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &APIError{StatusCode: resp.StatusCode, Body: string(b)}
	}
	return b, nil
}

// Post performs a POST request and unmarshals the response.
func (c *PlatformClient) Post(rawURL string, body, out any) error {
	return c.platformDo(http.MethodPost, rawURL, body, out)
}

// Put performs a PUT request and unmarshals the response.
func (c *PlatformClient) Put(rawURL string, body, out any) error {
	return c.platformDo(http.MethodPut, rawURL, body, out)
}

// Patch performs a PATCH request and unmarshals the response.
func (c *PlatformClient) Patch(rawURL string, body, out any) error {
	return c.platformDo(http.MethodPatch, rawURL, body, out)
}

// Delete performs a DELETE request.
func (c *PlatformClient) Delete(rawURL string) error {
	return c.platformDo(http.MethodDelete, rawURL, nil, nil)
}

// --- Engine-specific methods (Tasks 3.1–3.6) ---

// authResolveResponse is the response from GET /auth/resolve.
type authResolveResponse struct {
	TenantID string `json:"tenant_id"`
}

// ResolveAuth calls GET /auth/resolve and returns the tenant_id for the
// authenticated API key.
func (c *PlatformClient) ResolveAuth(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.apiURL("/auth/resolve"), nil)
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("request: %w", err)
	}
	defer resp.Body.Close()

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", &APIError{StatusCode: resp.StatusCode, Body: string(b)}
	}

	var out authResolveResponse
	if err := json.Unmarshal(b, &out); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}
	return out.TenantID, nil
}

// Account represents a trading account on the platform.
type Account struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Type    string   `json:"type"`
	Balance *float64 `json:"balance,omitempty"`
}

// ListAccounts calls GET /api/v1/accounts and returns all accounts for the
// authenticated tenant, including the current balance for each.
func (c *PlatformClient) ListAccounts(ctx context.Context) ([]Account, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.apiURL("/api/v1/accounts"), nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request: %w", err)
	}
	defer resp.Body.Close()

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &APIError{StatusCode: resp.StatusCode, Body: string(b)}
	}

	var accounts []Account
	if err := json.Unmarshal(b, &accounts); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return accounts, nil
}

// PortfolioPosition represents a single open position in the platform portfolio.
type PortfolioPosition struct {
	Symbol        string   `json:"symbol"`
	MarketType    string   `json:"market_type"`
	Side          string   `json:"side"`
	Quantity      float64  `json:"quantity"`
	AvgEntryPrice float64  `json:"avg_entry_price"`
	StopLoss      *float64 `json:"stop_loss,omitempty"`
	TakeProfit    *float64 `json:"take_profit,omitempty"`
	Leverage      *int     `json:"leverage,omitempty"`
	OpenedAt      string   `json:"opened_at"`
}

// Portfolio represents the portfolio response from the platform.
type Portfolio struct {
	AccountID string              `json:"account_id"`
	Positions []PortfolioPosition `json:"positions"`
}

// GetPortfolio calls GET /api/v1/accounts/{id}/portfolio and returns the open
// positions for the account.
func (c *PlatformClient) GetPortfolio(ctx context.Context, accountID string) (*Portfolio, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.apiURL("/api/v1/accounts/"+accountID+"/portfolio"), nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request: %w", err)
	}
	defer resp.Body.Close()

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &APIError{StatusCode: resp.StatusCode, Body: string(b)}
	}

	var portfolio Portfolio
	if err := json.Unmarshal(b, &portfolio); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &portfolio, nil
}

// tradePayload is the request body for POST /api/v1/trades.
type tradePayload struct {
	TenantID    string   `json:"tenant_id"`
	TradeID     string   `json:"trade_id"`
	AccountID   string   `json:"account_id"`
	Symbol      string   `json:"symbol"`
	Side        string   `json:"side"`
	Quantity    float64  `json:"quantity"`
	Price       float64  `json:"price"`
	Fee         float64  `json:"fee"`
	FeeCurrency string   `json:"fee_currency"`
	MarketType  string   `json:"market_type"`
	Timestamp   string   `json:"timestamp"`
	CostBasis   float64  `json:"cost_basis"`
	RealizedPnL float64  `json:"realized_pnl"`
	Leverage    *int     `json:"leverage,omitempty"`
	Margin      *float64 `json:"margin,omitempty"`
	Strategy    *string  `json:"strategy,omitempty"`
	EntryReason *string  `json:"entry_reason,omitempty"`
	ExitReason  *string  `json:"exit_reason,omitempty"`
	Confidence  *float64 `json:"confidence,omitempty"`
	StopLoss    *float64 `json:"stop_loss,omitempty"`
	TakeProfit  *float64 `json:"take_profit,omitempty"`
}

// TradeSubmission holds the data to submit for a single trade.
// Matches domain.Trade fields needed by the platform API.
type TradeSubmission struct {
	TenantID    string
	TradeID     string
	AccountID   string
	Symbol      string
	Side        string
	Quantity    float64
	Price       float64
	Fee         float64
	FeeCurrency string
	MarketType  string
	Timestamp   string // RFC3339
	CostBasis   float64
	RealizedPnL float64
	Leverage    *int
	Margin      *float64
	Strategy    *string
	EntryReason *string
	ExitReason  *string
	Confidence  *float64
	StopLoss    *float64
	TakeProfit  *float64
}

// SubmitTrade calls POST /api/v1/trades. Returns nil on 2xx or 409 (idempotent).
func (c *PlatformClient) SubmitTrade(ctx context.Context, trade TradeSubmission) error {
	payload := tradePayload{
		TenantID:    trade.TenantID,
		TradeID:     trade.TradeID,
		AccountID:   trade.AccountID,
		Symbol:      trade.Symbol,
		Side:        trade.Side,
		Quantity:    trade.Quantity,
		Price:       trade.Price,
		Fee:         trade.Fee,
		FeeCurrency: trade.FeeCurrency,
		MarketType:  trade.MarketType,
		Timestamp:   trade.Timestamp,
		CostBasis:   trade.CostBasis,
		RealizedPnL: trade.RealizedPnL,
		Leverage:    trade.Leverage,
		Margin:      trade.Margin,
		Strategy:    trade.Strategy,
		EntryReason: trade.EntryReason,
		ExitReason:  trade.ExitReason,
		Confidence:  trade.Confidence,
		StopLoss:    trade.StopLoss,
		TakeProfit:  trade.TakeProfit,
	}

	b, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal trade: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiURL("/api/v1/trades"), bytes.NewReader(b))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("request: %w", err)
	}
	defer resp.Body.Close()
	rb, _ := io.ReadAll(resp.Body)

	if resp.StatusCode == http.StatusConflict {
		return nil // idempotent — trade already recorded
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &APIError{StatusCode: resp.StatusCode, Body: string(rb)}
	}
	return nil
}

// SetBalance calls PUT /api/v1/accounts/{id}/balance with {"balance": amount}.
func (c *PlatformClient) SetBalance(ctx context.Context, accountID string, balance float64) error {
	payload := map[string]float64{"balance": balance}
	b, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.apiURL("/api/v1/accounts/"+accountID+"/balance"), bytes.NewReader(b))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("request: %w", err)
	}
	defer resp.Body.Close()
	rb, _ := io.ReadAll(resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &APIError{StatusCode: resp.StatusCode, Body: string(rb)}
	}
	return nil
}
