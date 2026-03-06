package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/spf13/viper"
)

// Client wraps the ledger HTTP API.
type Client struct {
	LedgerURL string
	APIKey    string
	TenantID  string
	http      *http.Client
}

// newClient resolves credentials and returns a ready Client.
// Exits the process with a helpful message if credentials are missing.
func newClient() *Client {
	ledgerURL := viper.GetString("trader_url")

	apiKey, _, err := resolveAPIKey()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}

	tenantID, err := resolveTenantID(apiKey, ledgerURL)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}

	return &Client{
		LedgerURL: ledgerURL,
		APIKey:    apiKey,
		TenantID:  tenantID,
		http:      &http.Client{Timeout: 30 * time.Second},
	}
}

// apiError wraps a non-2xx API response.
type apiError struct {
	StatusCode int
	Body       string
}

func (e *apiError) Error() string {
	return fmt.Sprintf("API error %d: %s", e.StatusCode, e.Body)
}

// do performs an HTTP request with Bearer auth.
func (c *Client) do(method, rawURL string, body any, out any) error {
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
	req.Header.Set("Authorization", "Bearer "+c.APIKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("request: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("authentication failed — run `trader auth login` to refresh your API key")
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &apiError{StatusCode: resp.StatusCode, Body: string(respBytes)}
	}

	if out != nil && len(respBytes) > 0 {
		if err := json.Unmarshal(respBytes, out); err != nil {
			return fmt.Errorf("decode response: %w\nbody: %s", err, string(respBytes))
		}
	}
	return nil
}

// doRaw performs a GET and returns raw bytes (used for --json passthrough).
func (c *Client) doRaw(method, rawURL string, rawBody []byte) (int, []byte, error) {
	var reqBody io.Reader
	if rawBody != nil {
		reqBody = bytes.NewReader(rawBody)
	}
	req, err := http.NewRequest(method, rawURL, reqBody)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	if rawBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b, nil
}

// traderURL builds a URL against the trader service.
func (c *Client) traderURL(path string, params ...url.Values) string {
	u := c.LedgerURL + path
	if len(params) > 0 && params[0] != nil {
		q := params[0].Encode()
		if q != "" {
			u += "?" + q
		}
	}
	return u
}

// Get performs a GET request and unmarshals the response.
func (c *Client) Get(rawURL string, out any) error {
	return c.do(http.MethodGet, rawURL, nil, out)
}

// GetRaw performs a GET and returns raw bytes.
func (c *Client) GetRaw(rawURL string) (int, []byte, error) {
	return c.doRaw(http.MethodGet, rawURL, nil)
}

// Post performs a POST request.
func (c *Client) Post(rawURL string, body, out any) error {
	return c.do(http.MethodPost, rawURL, body, out)
}

// PostRaw performs a POST with raw bytes body and returns raw bytes.
func (c *Client) PostRaw(rawURL string, body []byte) (int, []byte, error) {
	return c.doRaw(http.MethodPost, rawURL, body)
}

// PutRaw performs a PUT with a body reader and returns raw bytes.
func (c *Client) PutRaw(rawURL string, body io.Reader) (int, []byte, error) {
	req, err := http.NewRequest(http.MethodPut, rawURL, body)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.APIKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b, nil
}

// Delete performs a DELETE request and unmarshals the response.
func (c *Client) Delete(rawURL string, out any) error {
	return c.do(http.MethodDelete, rawURL, nil, out)
}

// isNotFound returns true if the error is an HTTP 404.
func isNotFound(err error) bool {
	if e, ok := err.(*apiError); ok {
		return e.StatusCode == http.StatusNotFound
	}
	return false
}

// isConflict returns true if the error is an HTTP 409.
func isConflict(err error) bool {
	if e, ok := err.(*apiError); ok {
		return e.StatusCode == http.StatusConflict
	}
	return false
}
