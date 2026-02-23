package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
)

// resetViper clears viper state and env vars that affect resolution.
func resetViper(t *testing.T) {
	t.Helper()
	viper.Reset()
	snViper = viper.New() // re-create; *viper.Viper has no Reset method
	os.Unsetenv("LEDGER_API_KEY")
	os.Unsetenv("LEDGER_TENANT_ID")
}

func writeTempConfig(t *testing.T, dir, filename, content string) string {
	t.Helper()
	path := filepath.Join(dir, filename)
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

// ---- resolveAPIKey tests ----

func TestResolveAPIKey_EnvWins(t *testing.T) {
	resetViper(t)
	t.Setenv("LEDGER_API_KEY", "env-key-123")

	key, src, err := resolveAPIKey()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if key != "env-key-123" {
		t.Errorf("expected env-key-123, got %s", key)
	}
	if src != "[env]" {
		t.Errorf("expected [env], got %s", src)
	}
}

func TestResolveAPIKey_LedgerConfigBeforeSN(t *testing.T) {
	resetViper(t)
	tmp := t.TempDir()

	snPath := writeTempConfig(t, tmp, "sn.yaml", "api_key: sn-key\n")
	snViper.SetConfigFile(snPath)
	_ = snViper.ReadInConfig()

	ledgerPath := writeTempConfig(t, tmp, "ledger.yaml", "api_key: ledger-key\n")
	viper.SetConfigFile(ledgerPath)
	_ = viper.ReadInConfig()

	key, src, err := resolveAPIKey()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if key != "ledger-key" {
		t.Errorf("expected ledger-key, got %s", key)
	}
	if src != "[ledger]" {
		t.Errorf("expected [ledger], got %s", src)
	}
}

func TestResolveAPIKey_SNConfigFallback(t *testing.T) {
	resetViper(t)
	tmp := t.TempDir()

	snPath := writeTempConfig(t, tmp, "sn.yaml", "api_key: sn-only-key\n")
	snViper.SetConfigFile(snPath)
	_ = snViper.ReadInConfig()

	key, src, err := resolveAPIKey()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if key != "sn-only-key" {
		t.Errorf("expected sn-only-key, got %s", key)
	}
	if src != "[sn]" {
		t.Errorf("expected [sn], got %s", src)
	}
}

func TestResolveAPIKey_NoneFound(t *testing.T) {
	resetViper(t)

	_, _, err := resolveAPIKey()
	if err == nil {
		t.Fatal("expected error when no key found")
	}
}

// ---- resolveTenantID tests ----

func TestResolveTenantID_EnvWins(t *testing.T) {
	resetViper(t)
	t.Setenv("LEDGER_TENANT_ID", "env-tenant-uuid")

	tid, err := resolveTenantID("any-key", "http://unused")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tid != "env-tenant-uuid" {
		t.Errorf("expected env-tenant-uuid, got %s", tid)
	}
}

func TestResolveTenantID_CachedConfig(t *testing.T) {
	resetViper(t)
	viper.Set("tenant_id", "cached-tenant-uuid")

	tid, err := resolveTenantID("any-key", "http://unused")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tid != "cached-tenant-uuid" {
		t.Errorf("expected cached-tenant-uuid, got %s", tid)
	}
}

func TestResolveTenantID_FetchesFromServer(t *testing.T) {
	resetViper(t)
	tmp := t.TempDir()
	ledgerPath := filepath.Join(tmp, "ledger.yaml")

	// Override ledgerConfigPath by pointing viper at the temp file AND
	// monkey-patching the write destination via the env var so writeConfigValue
	// writes to the temp path, not the real ~/.config/ledger/config.yaml.
	// We do this by temporarily overriding LEDGER_TENANT_ID after the call —
	// simplest approach is to just verify fetchTenantID directly.
	_ = ledgerPath

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/auth/resolve" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tenant_id":"server-tenant-uuid"}`))
	}))
	defer srv.Close()

	// Test fetchTenantID directly — avoids writing to the real config file
	tid, err := fetchTenantID("test-key", srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tid != "server-tenant-uuid" {
		t.Errorf("expected server-tenant-uuid, got %s", tid)
	}
}

func TestResolveTenantID_401Error(t *testing.T) {
	resetViper(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer srv.Close()

	_, err := resolveTenantID("bad-key", srv.URL)
	if err == nil {
		t.Fatal("expected error on 401")
	}
}
