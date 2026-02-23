package ingest

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/Spot-Canvas/ledger/internal/domain"
	"github.com/Spot-Canvas/ledger/internal/store"
)

const (
	// StreamName is the JetStream stream name for ledger trades.
	StreamName = "LEDGER_TRADES"
	// SubjectPrefix is the NATS subject prefix for trade events.
	SubjectPrefix = "ledger.trades."
	// SubjectWildcard subscribes to all trade subjects.
	SubjectWildcard = "ledger.trades.>"
	// ConsumerName is the durable consumer name.
	ConsumerName = "ledger-trade-consumer"
)

// Consumer subscribes to trade events via NATS JetStream.
type Consumer struct {
	nc     *nats.Conn
	repo   *store.Repository
	logger zerolog.Logger
}

// NewConsumer creates a new NATS trade consumer.
func NewConsumer(nc *nats.Conn, repo *store.Repository) *Consumer {
	return &Consumer{
		nc:     nc,
		repo:   repo,
		logger: log.With().Str("component", "ingest").Logger(),
	}
}

// Start begins consuming trade events. Blocks until context is cancelled.
func (c *Consumer) Start(ctx context.Context) error {
	js, err := jetstream.New(c.nc)
	if err != nil {
		return fmt.Errorf("create jetstream context: %w", err)
	}

	// Create or update the stream
	_, err = js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:     StreamName,
		Subjects: []string{SubjectWildcard},
		Storage:  jetstream.FileStorage,
		MaxBytes: 100 * 1024 * 1024, // 100MB
	})
	if err != nil {
		return fmt.Errorf("create stream: %w", err)
	}

	// Create durable consumer
	cons, err := js.CreateOrUpdateConsumer(ctx, StreamName, jetstream.ConsumerConfig{
		Durable:       ConsumerName,
		AckPolicy:     jetstream.AckExplicitPolicy,
		DeliverPolicy: jetstream.DeliverAllPolicy,
		AckWait:       30 * time.Second,
		MaxDeliver:    5,
	})
	if err != nil {
		return fmt.Errorf("create consumer: %w", err)
	}

	c.logger.Info().Msg("started consuming trade events from NATS JetStream")

	// Consume messages
	cc, err := cons.Consume(func(msg jetstream.Msg) {
		if err := c.handleMessage(ctx, msg); err != nil {
			c.logger.Error().Err(err).
				Str("subject", msg.Subject()).
				Msg("failed to handle trade message")
			// NAK for redelivery on DB errors
			msg.Nak()
			return
		}
		msg.Ack()
	})
	if err != nil {
		return fmt.Errorf("consume: %w", err)
	}

	// Wait for context cancellation
	<-ctx.Done()
	cc.Stop()
	c.logger.Info().Msg("stopped consuming trade events")
	return nil
}

func (c *Consumer) handleMessage(ctx context.Context, msg jetstream.Msg) error {
	var event TradeEvent
	if err := json.Unmarshal(msg.Data(), &event); err != nil {
		c.logger.Warn().Err(err).
			Str("subject", msg.Subject()).
			Msg("failed to unmarshal trade event, rejecting")
		// Terminate — malformed messages should not be redelivered
		msg.Term()
		return nil
	}

	// Validate (includes tenant_id presence and UUID format check)
	if err := event.Validate(); err != nil {
		c.logger.Warn().Err(err).
			Str("trade_id", event.TradeID).
			Str("subject", msg.Subject()).
			Msg("invalid trade event, rejecting")
		msg.Term()
		return nil
	}

	// Convert to domain trade (sets trade.TenantID)
	trade, err := event.ToDomain()
	if err != nil {
		c.logger.Warn().Err(err).
			Str("trade_id", event.TradeID).
			Msg("failed to convert trade event, rejecting")
		msg.Term()
		return nil
	}

	tenantID := trade.TenantID

	// Infer account type from account ID
	accountType := domain.InferAccountType(event.AccountID)

	// Ensure account exists (scoped to tenant)
	_, err = c.repo.GetOrCreateAccount(ctx, tenantID, trade.AccountID, accountType)
	if err != nil {
		return fmt.Errorf("get or create account: %w", err)
	}

	// Get avg entry price for cost basis calculation on sells
	if trade.Side == domain.SideSell {
		avgPrice, err := c.repo.GetAvgEntryPrice(ctx, tenantID, trade.AccountID, trade.Symbol, trade.MarketType)
		if err != nil {
			return fmt.Errorf("get avg entry price: %w", err)
		}
		store.CostBasisForTrade(trade, avgPrice)
	}

	// Insert trade and update position atomically (scoped to tenant)
	inserted, err := c.repo.InsertTradeAndUpdatePosition(ctx, tenantID, trade)
	if err != nil {
		return fmt.Errorf("insert trade and update position: %w", err)
	}

	if inserted {
		c.logger.Info().
			Str("tenant_id", tenantID.String()).
			Str("trade_id", trade.TradeID).
			Str("account_id", trade.AccountID).
			Str("symbol", trade.Symbol).
			Str("side", string(trade.Side)).
			Float64("quantity", trade.Quantity).
			Float64("price", trade.Price).
			Msg("ingested trade")
	} else {
		c.logger.Debug().
			Str("tenant_id", tenantID.String()).
			Str("trade_id", trade.TradeID).
			Msg("duplicate trade, skipped")
	}

	return nil
}

// ConnectNATS connects to NATS with retry logic, matching spot-canvas-app patterns.
func ConnectNATS(urls string, credsFile, creds string) (*nats.Conn, error) {
	opts := []nats.Option{
		nats.Name("ledger"),
		nats.ReconnectWait(time.Second),
		nats.MaxReconnects(-1),
		nats.ReconnectHandler(func(nc *nats.Conn) {
			log.Info().Str("url", nc.ConnectedUrl()).Msg("reconnected to NATS")
		}),
		nats.DisconnectErrHandler(func(nc *nats.Conn, err error) {
			if err != nil {
				log.Warn().Err(err).Msg("disconnected from NATS")
			}
		}),
		nats.ErrorHandler(func(nc *nats.Conn, sub *nats.Subscription, err error) {
			log.Error().Err(err).Msg("NATS error")
		}),
	}

	// Add credentials if configured
	if creds != "" {
		tmpFile, err := os.CreateTemp("", "nats-creds-*.creds")
		if err != nil {
			return nil, fmt.Errorf("create temp credentials file: %w", err)
		}
		if _, err := tmpFile.WriteString(creds); err != nil {
			tmpFile.Close()
			os.Remove(tmpFile.Name())
			return nil, fmt.Errorf("write credentials: %w", err)
		}
		tmpFile.Close()
		opts = append(opts, nats.UserCredentials(tmpFile.Name()))
	} else if credsFile != "" {
		opts = append(opts, nats.UserCredentials(credsFile))
	}

	// Retry connection
	var nc *nats.Conn
	var err error
	backoff := 100 * time.Millisecond
	maxBackoff := 30 * time.Second

	for attempt := 1; ; attempt++ {
		nc, err = nats.Connect(urls, opts...)
		if err == nil {
			log.Info().Str("url", nc.ConnectedUrl()).Int("attempt", attempt).Msg("connected to NATS")
			return nc, nil
		}

		log.Warn().Err(err).Int("attempt", attempt).Dur("backoff", backoff).
			Msg("failed to connect to NATS, retrying...")
		time.Sleep(backoff)

		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}
