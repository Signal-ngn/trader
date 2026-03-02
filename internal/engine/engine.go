// Package engine implements the trading engine goroutine for the trader service.
// It subscribes to Synadia NGS signals, filters them, and executes paper or live
// trades by writing directly to the store layer — no HTTP round-trip required.
package engine

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	nats "github.com/nats-io/nats.go"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/Signal-ngn/trader/internal/api/middleware"
	"github.com/Signal-ngn/trader/internal/config"
	"github.com/Signal-ngn/trader/internal/ingest"
	"github.com/Signal-ngn/trader/internal/store"
)

// PositionState holds the in-memory risk metadata for a single open position.
type PositionState struct {
	ID           int64
	AccountID    string
	Symbol       string
	MarketType   string
	Side         string  // "long" or "short"
	EntryPrice   float64
	StopLoss     float64
	TakeProfit   float64
	Leverage     int
	Strategy     string
	OpenedAt     time.Time
	PeakPrice    float64
	TrailingStop float64
}

// posKey returns the map key for a (accountID, symbol) pair.
func posKey(accountID, symbol string) string { return accountID + ":" + symbol }

// cooldownKey identifies a (accountID, product, action) tuple for the cooldown map.
type cooldownKey struct {
	accountID string
	symbol    string
	action    string // "BUY" or "SHORT"
}

// Engine is the trading engine. It connects to Synadia NGS, subscribes to signals,
// and manages positions based on those signals and risk rules.
type Engine struct {
	cfg      *config.Config
	repo     *store.Repository
	exchange Exchange

	// tenantUUID is resolved once at Start from the SN API key.
	// Falls back to the middleware default tenant if the key is not found.
	tenantUUID uuid.UUID

	// accounts is the resolved list of account IDs this engine instance trades.
	// Populated at Start time (from cfg.TraderAccounts or all tenant accounts).
	accounts []string

	// publisher fans out trade events to SSE subscribers after each fill.
	publisher ingest.TradePublisher

	// NGS NATS connection (separate from the ledger NATS connection)
	ngsConn *nats.Conn

	// In-memory risk state cache — keyed by posKey(accountID, symbol)
	posStateMu sync.RWMutex
	posState   map[string]*PositionState

	// Per-(accountID, product, action) cooldown map
	cooldownMu sync.Mutex
	cooldown   map[cooldownKey]time.Time

	// Direction conflict guard — keyed by posKey(accountID, symbol), value is "long" or "short"
	conflictMu sync.Mutex
	conflict   map[string]string

	// Signal allowlist — rebuilt every 5 minutes from the SN API
	allowlistMu sync.RWMutex
	allowlist   signalAllowlist

	// Last observed signal price per symbol — used as current price in risk loop.
	// Updated on every signal received from NGS.
	lastPriceMu sync.RWMutex
	lastPrice   map[string]float64 // symbol → last signal price

	// (No in-memory daily loss counter — queried from DB on each check so it
	// survives restarts and reflects trades from all sources, not just the engine.)

	logger zerolog.Logger
}

// New creates a new Engine. The Exchange is selected based on cfg.TradingMode.
// publisher may be nil; when set, every filled trade is fanned out to SSE subscribers.
func New(cfg *config.Config, repo *store.Repository, publisher ingest.TradePublisher) *Engine {
	var ex Exchange
	if cfg.TradingMode == "live" {
		ex = NewBinanceFuturesExchange(cfg)
	} else {
		ex = NewNoopExchange(cfg)
	}

	return &Engine{
		cfg:       cfg,
		repo:      repo,
		exchange:  ex,
		publisher: publisher,
		posState:  make(map[string]*PositionState),
		cooldown:  make(map[cooldownKey]time.Time),
		conflict:  make(map[string]string),
		lastPrice: make(map[string]float64),
		logger:    log.With().Str("component", "engine").Logger(),
	}
}

// Start initialises the engine and runs the signal and risk loops.
// It blocks until ctx is cancelled.
func (e *Engine) Start(ctx context.Context) error {
	// Resolve tenant from the SN API key (same key used to authenticate to the
	// trader service). Falls back to the middleware default tenant.
	if e.cfg.SNAPIKey != "" {
		apiKey, err := uuid.Parse(e.cfg.SNAPIKey)
		if err == nil {
			userRepo := store.NewUserRepository(e.repo.Pool())
			user, err := userRepo.GetByAPIKey(ctx, apiKey)
			if err != nil {
				e.logger.Warn().Err(err).Msg("could not resolve tenant from SN_API_KEY, using default")
			} else if user != nil {
				e.tenantUUID = user.TenantID
				e.logger.Info().Str("tenant_id", e.tenantUUID.String()).Msg("resolved tenant from SN_API_KEY")
			}
		}
	}
	if e.tenantUUID == uuid.Nil {
		e.tenantUUID = middleware.DefaultTenantID
		e.logger.Warn().Str("tenant_id", e.tenantUUID.String()).Msg("using default tenant — set SN_API_KEY to resolve the real tenant")
	}

	// Resolve the account list: use cfg.TraderAccounts if set, otherwise load
	// all accounts for the tenant from the DB.
	if len(e.cfg.TraderAccounts) > 0 {
		e.accounts = e.cfg.TraderAccounts
	} else {
		accts, err := e.repo.ListAccounts(ctx, e.tenantUUID)
		if err != nil {
			e.logger.Error().Err(err).Msg("failed to list tenant accounts — engine aborted")
			return nil
		}
		for _, a := range accts {
			e.accounts = append(e.accounts, a.ID)
		}
	}
	if len(e.accounts) == 0 {
		e.logger.Error().Msg("no accounts to trade — engine aborted (create an account first or set TRADER_ACCOUNTS)")
		return nil
	}

	e.logger.Info().
		Strs("accounts", e.accounts).
		Str("mode", e.cfg.TradingMode).
		Msg("starting trading engine")

	// Validate live-mode credentials before doing anything else.
	if e.cfg.TradingMode == "live" {
		if e.cfg.BinanceAPIKey == "" || e.cfg.BinanceAPISecret == "" {
			e.logger.Error().Msg("BINANCE_API_KEY and BINANCE_API_SECRET are required in live mode — engine aborted")
			return nil
		}
		if _, err := e.exchange.GetBalance(ctx); err != nil {
			e.logger.Error().Err(err).Msg("Binance credential validation failed — engine aborted")
			return nil
		}
		e.logger.Info().Msg("Binance credentials validated")
	}

	// Require SN API key.
	if e.cfg.SNAPIKey == "" {
		e.logger.Error().Msg("SN_API_KEY is required when TRADING_ENABLED=true — engine aborted")
		return nil
	}

	// Fetch initial allowlist.
	al, err := fetchAllowlist(ctx, e.cfg)
	if err != nil {
		e.logger.Error().Err(err).Msg("failed to fetch signal allowlist — engine aborted")
		return nil
	}
	e.allowlistMu.Lock()
	e.allowlist = al
	e.allowlistMu.Unlock()
	e.logger.Info().Int("slots", len(al)).Msg("loaded signal allowlist")

	// Load startup state from DB.
	if err := e.loadStartupState(ctx); err != nil {
		e.logger.Error().Err(err).Msg("failed to load startup state — engine aborted")
		return nil
	}

	// Start allowlist refresh goroutine.
	go e.startAllowlistRefresher(ctx)

	// Start risk loop goroutine.
	go e.startRiskLoop(ctx)

	// Connect to NGS and run signal loop (blocks until ctx cancelled).
	e.runSignalLoop(ctx)

	e.logger.Info().Msg("trading engine stopped")
	return nil
}

// loadStartupState seeds the conflict guard and position state cache for all accounts.
func (e *Engine) loadStartupState(ctx context.Context) error {
	totalPositions, totalStates := 0, 0

	for _, accountID := range e.accounts {
		// Seed conflict guard from open ledger positions.
		openPositions, err := e.repo.ListOpenPositionsForAccount(ctx, accountID)
		if err != nil {
			return fmt.Errorf("list open positions for %s: %w", accountID, err)
		}
		e.conflictMu.Lock()
		for _, p := range openPositions {
			e.conflict[posKey(accountID, p.Symbol)] = string(p.Side)
		}
		e.conflictMu.Unlock()
		totalPositions += len(openPositions)

		// Load engine_position_state rows.
		dbStates, err := e.repo.LoadPositionStates(ctx, accountID)
		if err != nil {
			return fmt.Errorf("load position states for %s: %w", accountID, err)
		}
		e.posStateMu.Lock()
		for _, s := range dbStates {
			ps := &PositionState{
				ID:           s.ID,
				AccountID:    s.AccountID,
				Symbol:       s.Symbol,
				MarketType:   s.MarketType,
				Side:         s.Side,
				EntryPrice:   s.EntryPrice,
				StopLoss:     s.StopLoss,
				TakeProfit:   s.TakeProfit,
				Leverage:     s.Leverage,
				Strategy:     s.Strategy,
				OpenedAt:     s.OpenedAt,
				PeakPrice:    s.PeakPrice,
				TrailingStop: s.TrailingStop,
			}
			e.posState[posKey(accountID, s.Symbol)] = ps
		}
		e.posStateMu.Unlock()
		totalStates += len(dbStates)
	}

	e.logger.Info().
		Int("open_positions", totalPositions).
		Int("position_states", totalStates).
		Msg("loaded startup state")
	return nil
}
