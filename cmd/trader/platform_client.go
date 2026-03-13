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

	"github.com/Signal-ngn/trader/internal/platform"
)

// PlatformClient wraps the internal platform client and adds CLI-local URL
// helper methods and raw HTTP call helpers so command files can call the
// platform API with the same patterns previously used against the ledger Client.
type PlatformClient struct {
	*platform.PlatformClient
	http *http.Client
}

// newPlatformClient resolves the API key and returns a ready PlatformClient.
// Exits the process with a helpful message if the API key is missing.
func newPlatformClient() *PlatformClient {
	apiKey, _, err := resolveAPIKey()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
	return &PlatformClient{
		PlatformClient: platform.NewWithIngestion(
			viper.GetString("api_url"),
			viper.GetString("ingestion_url"),
			apiKey,
		),
		http: &http.Client{Timeout: 30 * time.Second},
	}
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

// doRaw performs a raw HTTP call with Bearer auth and returns (statusCode, body, error).
func (c *PlatformClient) doRaw(method, rawURL string, body []byte) (int, []byte, error) {
	var reqBody io.Reader
	if body != nil {
		reqBody = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, rawURL, reqBody)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	if body != nil {
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

// doRawReader performs a raw HTTP call with a reader body and returns (statusCode, body, error).
func (c *PlatformClient) doRawReader(method, rawURL string, body io.Reader) (int, []byte, error) {
	req, err := http.NewRequest(method, rawURL, body)
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

// GetRaw shadows platform.PlatformClient.GetRaw to return (int, []byte, error).
func (c *PlatformClient) GetRaw(rawURL string) (int, []byte, error) {
	return c.doRaw(http.MethodGet, rawURL, nil)
}

// PostRaw performs a POST with raw bytes body and returns (statusCode, body, error).
func (c *PlatformClient) PostRaw(rawURL string, body []byte) (int, []byte, error) {
	return c.doRaw(http.MethodPost, rawURL, body)
}

// PutRaw performs a PUT with a reader body and returns (statusCode, body, error).
func (c *PlatformClient) PutRaw(rawURL string, body io.Reader) (int, []byte, error) {
	return c.doRawReader(http.MethodPut, rawURL, body)
}

// DeleteWithResult performs a DELETE and unmarshals the response into out.
func (c *PlatformClient) DeleteWithResult(rawURL string, out any) error {
	statusCode, b, err := c.doRaw(http.MethodDelete, rawURL, nil)
	if err != nil {
		return err
	}
	if statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden {
		return fmt.Errorf("authentication failed — run `trader auth login` to refresh your API key")
	}
	if statusCode < 200 || statusCode >= 300 {
		return &platformAPIError{StatusCode: statusCode, Body: string(b)}
	}
	if out != nil && len(b) > 0 {
		if err := json.Unmarshal(b, out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}

// platformAPIError wraps a non-2xx response from the platform API (CLI-local type).
type platformAPIError struct {
	StatusCode int
	Body       string
}

func (e *platformAPIError) Error() string {
	return fmt.Sprintf("API error %d: %s", e.StatusCode, e.Body)
}

// isPlatformNotFound returns true if the error is a 404 from the platform API.
func isPlatformNotFound(err error) bool {
	if e, ok := err.(*platform.APIError); ok {
		return e.StatusCode == http.StatusNotFound
	}
	if e, ok := err.(*platformAPIError); ok {
		return e.StatusCode == http.StatusNotFound
	}
	return false
}

// isPlatformConflict returns true if the error is a 409 from the platform API.
func isPlatformConflict(err error) bool {
	if e, ok := err.(*platform.APIError); ok {
		return e.StatusCode == http.StatusConflict
	}
	if e, ok := err.(*platformAPIError); ok {
		return e.StatusCode == http.StatusConflict
	}
	return false
}

// platformErrorBody extracts the error body string from a platform API error.
func platformErrorBody(err error) string {
	if e, ok := err.(*platformAPIError); ok {
		return e.Body
	}
	if e, ok := err.(*platform.APIError); ok {
		return e.Body
	}
	return ""
}

// resolveAPIKey is re-declared here to avoid import cycle — it's defined in config.go.
// (No redeclaration needed — it's in the same package.)

// fmtTime and friends are in output.go — same package.

// Keep compile happy: ensure time is imported via usage above.
var _ = time.Second
