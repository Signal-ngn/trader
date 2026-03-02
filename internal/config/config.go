package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

// Config holds all configuration for the ledger service.
type Config struct {
	// HTTP server port
	HTTPPort string

	// Database settings
	DatabaseURL string

	// Cloud SQL settings (for GCP deployment)
	CloudSQLInstance string
	DBName           string
	DBUser           string
	DBPassword       string

	// NATS settings
	NATSURLs      string
	NATSCredsFile string
	NATSCreds     string

	// Logging
	LogLevel    string
	Environment string

	// Auth
	EnforceAuth bool

	// Trading engine settings
	TradingEnabled   bool
	TradingMode      string  // "paper" or "live"
	TraderAccount    string  // account ID to trade under
	StrategyFilter   string  // optional prefix filter for signal strategies
	PortfolioSize    float64 // total portfolio size in USD
	PositionSizePct  float64 // default position size as % of portfolio (0–100)
	MaxPositionSize  float64 // max position size in USD (0 = no limit)
	MinPositionSize  float64 // min position size in USD (0 = no minimum)
	MaxPositions     int     // max concurrent open positions (0 = no limit)
	DailyLossLimit   float64 // max daily loss in USD before halting opens (0 = no limit)
	KillSwitchFile   string  // path to kill switch file (default: /tmp/trader.kill)
	SNAPIKey         string  // SignalNGN API key
	SNAPIURL         string  // SignalNGN API base URL
	SNNATSCredsFile  string  // path to NGS NATS credentials file (optional)
	BinanceAPIKey    string  // Binance API key (live mode only)
	BinanceAPISecret string  // Binance API secret (live mode only)
}

// Load reads configuration from environment variables with .env support.
func Load() (*Config, error) {
	// Load .env file if it exists (ignore error if not found)
	_ = godotenv.Load()

	cfg := &Config{
		HTTPPort:         getEnv("HTTP_PORT", "8080"),
		DatabaseURL:      getEnv("DATABASE_URL", "postgres://spot:spot@localhost:5432/spot_canvas?sslmode=disable"),
		CloudSQLInstance: os.Getenv("CLOUDSQL_INSTANCE"),
		DBName:           getEnv("DB_NAME", "spot_canvas"),
		DBUser:           getEnv("DB_USER", "spot"),
		DBPassword:       os.Getenv("DB_PASSWORD"),
		NATSURLs:         getEnv("NATS_URLS", "nats://localhost:4222"),
		NATSCredsFile:    os.Getenv("NATS_CREDS_FILE"),
		NATSCreds:        os.Getenv("NATS_CREDS"),
		LogLevel:         getEnv("LOG_LEVEL", "info"),
		Environment:      getEnv("ENVIRONMENT", "development"),
		EnforceAuth:      os.Getenv("ENFORCE_AUTH") != "false",

		// Trading engine
		TradingEnabled:   os.Getenv("TRADING_ENABLED") == "true",
		TradingMode:      getEnv("TRADING_MODE", "paper"),
		TraderAccount:    getEnv("TRADER_ACCOUNT", "paper"),
		StrategyFilter:   os.Getenv("STRATEGY_FILTER"),
		PortfolioSize:    parseFloat(os.Getenv("PORTFOLIO_SIZE"), 10000),
		PositionSizePct:  parseFloat(os.Getenv("POSITION_SIZE_PCT"), 10),
		MaxPositionSize:  parseFloat(os.Getenv("MAX_POSITION_SIZE"), 0),
		MinPositionSize:  parseFloat(os.Getenv("MIN_POSITION_SIZE"), 0),
		MaxPositions:     parseInt(os.Getenv("MAX_POSITIONS"), 0),
		DailyLossLimit:   parseFloat(os.Getenv("DAILY_LOSS_LIMIT"), 0),
		KillSwitchFile:   getEnv("KILL_SWITCH_FILE", "/tmp/trader.kill"),
		SNAPIKey:         os.Getenv("SN_API_KEY"),
		SNAPIURL:         getEnv("SN_API_URL", "https://api.signal-ngn.com"),
		SNNATSCredsFile:  os.Getenv("SN_NATS_CREDS_FILE"),
		BinanceAPIKey:    os.Getenv("BINANCE_API_KEY"),
		BinanceAPISecret: os.Getenv("BINANCE_API_SECRET"),
	}

	// Build Cloud SQL connection string if instance is specified
	if cfg.CloudSQLInstance != "" {
		cfg.DatabaseURL = cfg.buildCloudSQLURL()
	}

	return cfg, nil
}

// buildCloudSQLURL constructs a PostgreSQL connection string for Cloud SQL
// using Unix socket connection (required for Cloud Run).
func (c *Config) buildCloudSQLURL() string {
	socketDir := "/cloudsql"
	socketPath := fmt.Sprintf("%s/%s", socketDir, c.CloudSQLInstance)

	var sb strings.Builder
	sb.WriteString("postgres://")
	sb.WriteString(c.DBUser)
	if c.DBPassword != "" {
		sb.WriteString(":")
		sb.WriteString(c.DBPassword)
	}
	sb.WriteString("@/")
	sb.WriteString(c.DBName)
	sb.WriteString("?host=")
	sb.WriteString(socketPath)

	return sb.String()
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func parseFloat(s string, defaultValue float64) float64 {
	if s == "" {
		return defaultValue
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return defaultValue
	}
	return v
}

func parseInt(s string, defaultValue int) int {
	if s == "" {
		return defaultValue
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return defaultValue
	}
	return v
}
