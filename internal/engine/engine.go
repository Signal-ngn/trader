// Package engine implements the trading engine goroutine for the trader service.
// It subscribes to Synadia NGS signals, filters them, and executes paper or live
// trades by writing directly to the store layer — no HTTP round-trip required.
package engine

import (
	"context"
	"sync"
	"time"

	nats "github.com/nats-io/nats.go"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/Signal-ngn/trader/internal/config"
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

// cooldownKey identifies a (product, action) pair for the cooldown map.
type cooldownKey struct {
	symbol string
	action string // "BUY" or "SHORT"
}

// Engine is the trading engine. It connects to Synadia NGS, subscribes to signals,
// and manages positions based on those signals and risk rules.
type Engine struct {
	cfg      *config.Config
	repo     *store.Repository
	exchange Exchange

	// NGS NATS connection (separate from the ledger NATS connection)
	ngsConn *nats.Conn

	// In-memory risk state cache — keyed by symbol
	posStateMu sync.RWMutex
	posState   map[string]*PositionState // symbol → state

	// Per-(product,action) cooldown map
	cooldownMu sync.Mutex
	cooldown   map[cooldownKey]time.Time

	// Direction conflict guard — keyed by symbol, value is "long" or "short"
	conflictMu sync.Mutex
	conflict   map[string]string // symbol → open side

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
func New(cfg *config.Config, repo *store.Repository) *Engine {
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
	e.logger.Info().
		Str("account", e.cfg.TraderAccount).
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

// loadStartupState seeds the conflict guard from open ledger positions
// and loads engine_position_state rows into the in-memory cache.
func (e *Engine) loadStartupState(ctx context.Context) error {
	// Seed conflict guard from open ledger positions.
	// We use the default tenant — the engine is single-tenant.
	openPositions, err := e.repo.ListOpenPositionsForAccount(ctx, e.cfg.TraderAccount)
	if err != nil {
		return err
	}
	e.conflictMu.Lock()
	for _, p := range openPositions {
		e.conflict[p.Symbol] = string(p.Side)
	}
	e.conflictMu.Unlock()
	e.logger.Info().Int("open_positions", len(openPositions)).Msg("seeded conflict guard from ledger")

	// Load engine_position_state rows.
	dbStates, err := e.repo.LoadPositionStates(ctx, e.cfg.TraderAccount)
	if err != nil {
		return err
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
		e.posState[s.Symbol] = ps
	}
	e.posStateMu.Unlock()
	e.logger.Info().Int("position_states", len(dbStates)).Msg("loaded engine position states")

	return nil
}
