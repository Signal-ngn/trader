package main

import (
	"fmt"
	"net/url"
	"os"

	"github.com/spf13/viper"

	"github.com/Signal-ngn/trader/internal/platform"
)

// PlatformClient wraps the internal platform client and adds CLI-local URL
// helper methods (apiURL, ingestionURL) so existing command code is unchanged.
type PlatformClient struct {
	*platform.PlatformClient
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
