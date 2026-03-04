package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// validConfigKeys are the keys writable via `trader config set`.
var validConfigKeys = []string{
	"trader_url", "tenant_id", "api_key",
	"api_url", "web_url", "ingestion_url", "nats_url", "nats_creds_file",
}

// configDefaults holds built-in defaults.
var configDefaults = map[string]string{
	"trader_url":    "https://signalngn-trader-potbdcvufa-ew.a.run.app",
	"api_url":       "https://signalngn-api-potbdcvufa-ew.a.run.app",
	"web_url":       "https://signalngn.com",
	"ingestion_url": "https://signalngn-ingestion-potbdcvufa-ew.a.run.app",
	"nats_url":      "tls://connect.ngs.global",
}

// snViper is a read-only viper instance pointing at ~/.config/sn/config.yaml.
var snViper = viper.New()

// traderConfigPath returns the path to the trader config file.
func traderConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "trader", "config.yaml")
}

// snConfigPath returns the path to the sn config file.
func snConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "sn", "config.yaml")
}

// loadConfig initialises viper with config file, env vars, and defaults.
func loadConfig() {
	// Set defaults
	for k, v := range configDefaults {
		viper.SetDefault(k, v)
	}

	// Trader config file (read-write)
	cfgPath := traderConfigPath()
	if cfgPath != "" {
		viper.SetConfigFile(cfgPath)
		_ = viper.ReadInConfig()
	}

	// Environment variables (TRADER_ prefix)
	viper.SetEnvPrefix("TRADER")
	viper.AutomaticEnv()
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	// sn config file (read-only, for api_key fallback)
	snPath := snConfigPath()
	if snPath != "" {
		snViper.SetConfigFile(snPath)
		_ = snViper.ReadInConfig()
	}
}

// resolveAPIKey returns the API key using priority order:
//  1. TRADER_API_KEY env var
//  2. api_key in ~/.config/trader/config.yaml
//  3. api_key in ~/.config/sn/config.yaml
func resolveAPIKey() (string, string, error) {
	// 1. Env var (already bound via AutomaticEnv with TRADER_ prefix)
	if key := os.Getenv("TRADER_API_KEY"); key != "" {
		return key, "[env]", nil
	}

	// 2. Trader config
	if key := viper.GetString("api_key"); key != "" {
		return key, "[trader]", nil
	}

	// 3. sn config — copy to trader config for future use
	if key := snViper.GetString("api_key"); key != "" {
		_ = writeConfigValue("api_key", key)
		return key, "[sn]", nil
	}

	return "", "", fmt.Errorf("no API key found — run `trader auth login` or set TRADER_API_KEY")
}

// resolveTenantID returns the tenant ID using priority order:
//  1. TRADER_TENANT_ID env var
//  2. tenant_id in ~/.config/trader/config.yaml
//  3. Call GET /auth/resolve and cache the result
func resolveTenantID(apiKey, traderURL string) (string, error) {
	// 1. Env var
	if tid := os.Getenv("TRADER_TENANT_ID"); tid != "" {
		return tid, nil
	}

	// 2. Cached in trader config
	if tid := viper.GetString("tenant_id"); tid != "" {
		return tid, nil
	}

	// 3. Resolve via /auth/resolve
	tid, err := fetchTenantID(apiKey, traderURL)
	if err != nil {
		return "", err
	}

	// Cache for future calls
	_ = writeConfigValue("tenant_id", tid)
	return tid, nil
}

// fetchTenantID calls GET /auth/resolve and returns the tenant_id.
func fetchTenantID(apiKey, traderURL string) (string, error) {
	req, err := http.NewRequest("GET", traderURL+"/auth/resolve", nil)
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("auth/resolve: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusUnauthorized {
		return "", fmt.Errorf("authentication failed — check your API key")
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("auth/resolve returned %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		TenantID string `json:"tenant_id"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("decode auth/resolve response: %w", err)
	}
	return result.TenantID, nil
}

// writeConfigValue writes a key=value to the trader config file.
func writeConfigValue(key, value string) error {
	cfgPath := traderConfigPath()
	dir := filepath.Dir(cfgPath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	viper.Set(key, value)
	if err := viper.WriteConfigAs(cfgPath); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	// Fix permissions
	_ = os.Chmod(cfgPath, 0600)
	return nil
}

// deleteConfigValue removes a key from the trader config file.
func deleteConfigValue(key string) error {
	cfgPath := traderConfigPath()
	settings := viper.AllSettings()
	delete(settings, key)
	viper.Reset()
	loadConfig()
	for k, v := range settings {
		if k != key {
			viper.Set(k, v)
		}
	}
	return viper.WriteConfigAs(cfgPath)
}

// maskValue shows the first 8 characters followed by "..." for long strings.
func maskValue(val string) string {
	if val == "" {
		return ""
	}
	if len(val) <= 8 {
		return val
	}
	return val[:8] + "..."
}

// isValidKey checks whether a key is in validConfigKeys.
func isValidKey(key string) bool {
	for _, k := range validConfigKeys {
		if k == key {
			return true
		}
	}
	return false
}

// configSource returns where the value for a key comes from.
func configSource(key string) string {
	envKey := "TRADER_" + strings.ToUpper(key)
	if os.Getenv(envKey) != "" {
		return "[env]"
	}
	// For api_key, check env without prefix too
	if key == "api_key" {
		if os.Getenv("TRADER_API_KEY") != "" {
			return "[env]"
		}
		if viper.GetString(key) != "" {
			return "[trader]"
		}
		if snViper.GetString(key) != "" {
			return "[sn]"
		}
		return "[not set]"
	}
	if viper.IsSet(key) && viper.ConfigFileUsed() != "" {
		return "[trader]"
	}
	_, hasDefault := configDefaults[key]
	if hasDefault {
		return "[default]"
	}
	return "[not set]"
}
