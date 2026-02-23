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

// validConfigKeys are the keys writable via `ledger config set`.
var validConfigKeys = []string{"ledger_url", "tenant_id", "api_key"}

// configDefaults holds built-in defaults.
var configDefaults = map[string]string{
	"ledger_url": "https://signalngn-ledger-potbdcvufa-ew.a.run.app",
}

// snViper is a read-only viper instance pointing at ~/.config/sn/config.yaml.
var snViper = viper.New()

// ledgerConfigPath returns the path to the ledger config file.
func ledgerConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "ledger", "config.yaml")
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

	// Ledger config file (read-write)
	cfgPath := ledgerConfigPath()
	if cfgPath != "" {
		viper.SetConfigFile(cfgPath)
		_ = viper.ReadInConfig()
	}

	// Environment variables (LEDGER_ prefix)
	viper.SetEnvPrefix("LEDGER")
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
//  1. LEDGER_API_KEY env var
//  2. api_key in ~/.config/ledger/config.yaml
//  3. api_key in ~/.config/sn/config.yaml
func resolveAPIKey() (string, string, error) {
	// 1. Env var (already bound via AutomaticEnv with LEDGER_ prefix)
	if key := os.Getenv("LEDGER_API_KEY"); key != "" {
		return key, "[env]", nil
	}

	// 2. Ledger config
	if key := viper.GetString("api_key"); key != "" {
		return key, "[ledger]", nil
	}

	// 3. sn config
	if key := snViper.GetString("api_key"); key != "" {
		return key, "[sn]", nil
	}

	return "", "", fmt.Errorf("no API key found — run `sn auth login` or set LEDGER_API_KEY")
}

// resolveTenantID returns the tenant ID using priority order:
//  1. LEDGER_TENANT_ID env var
//  2. tenant_id in ~/.config/ledger/config.yaml
//  3. Call GET /auth/resolve and cache the result
func resolveTenantID(apiKey, ledgerURL string) (string, error) {
	// 1. Env var
	if tid := os.Getenv("LEDGER_TENANT_ID"); tid != "" {
		return tid, nil
	}

	// 2. Cached in ledger config
	if tid := viper.GetString("tenant_id"); tid != "" {
		return tid, nil
	}

	// 3. Resolve via /auth/resolve
	tid, err := fetchTenantID(apiKey, ledgerURL)
	if err != nil {
		return "", err
	}

	// Cache for future calls
	_ = writeConfigValue("tenant_id", tid)
	return tid, nil
}

// fetchTenantID calls GET /auth/resolve and returns the tenant_id.
func fetchTenantID(apiKey, ledgerURL string) (string, error) {
	req, err := http.NewRequest("GET", ledgerURL+"/auth/resolve", nil)
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

// writeConfigValue writes a key=value to the ledger config file.
func writeConfigValue(key, value string) error {
	cfgPath := ledgerConfigPath()
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

// deleteConfigValue removes a key from the ledger config file.
func deleteConfigValue(key string) error {
	cfgPath := ledgerConfigPath()
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
	envKey := "LEDGER_" + strings.ToUpper(key)
	if os.Getenv(envKey) != "" {
		return "[env]"
	}
	// For api_key, check env without prefix too
	if key == "api_key" {
		if os.Getenv("LEDGER_API_KEY") != "" {
			return "[env]"
		}
		if viper.GetString(key) != "" {
			return "[ledger]"
		}
		if snViper.GetString(key) != "" {
			return "[sn]"
		}
		return "[not set]"
	}
	if viper.IsSet(key) && viper.ConfigFileUsed() != "" {
		return "[ledger]"
	}
	_, hasDefault := configDefaults[key]
	if hasDefault {
		return "[default]"
	}
	return "[not set]"
}
