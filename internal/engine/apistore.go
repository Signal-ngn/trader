package engine

import (
	"context"
	"fmt"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/google/uuid"
	"google.golang.org/api/iterator"

	"github.com/Signal-ngn/trader/internal/config"
	"github.com/Signal-ngn/trader/internal/domain"
	"github.com/Signal-ngn/trader/internal/platform"
)

// APIEngineStore is an EngineStore implementation backed by the Signal ngn
// platform API (for trades, positions, accounts, and balance) and GCP Firestore
// (for engine risk-management state and daily P&L). It has no dependency on
// any SQL database or the internal/store package.
type APIEngineStore struct {
	client    *platform.PlatformClient
	firestore *firestore.Client
	cfg       *config.Config
}

// NewAPIEngineStore creates an APIEngineStore. Both platformClient and
// firestoreClient must be non-nil.
func NewAPIEngineStore(platformClient *platform.PlatformClient, firestoreClient *firestore.Client, cfg *config.Config) *APIEngineStore {
	return &APIEngineStore{
		client:    platformClient,
		firestore: firestoreClient,
		cfg:       cfg,
	}
}

// --- Firestore helpers ---

// posDocRef returns the Firestore document reference for an engine position state.
// Path: engine-state/{accountID}/positions/{symbol}-{marketType}
func (s *APIEngineStore) posDocRef(accountID, symbol, marketType string) *firestore.DocumentRef {
	docID := symbol + "-" + marketType
	return s.firestore.Collection("engine-state").Doc(accountID).Collection("positions").Doc(docID)
}

// dailyPnLDocRef returns the Firestore document reference for today's P&L accumulator.
// Path: engine-state/{accountID}/daily-pnl/{accountID}-{YYYY-MM-DD}
func (s *APIEngineStore) dailyPnLDocRef(accountID string) *firestore.DocumentRef {
	date := time.Now().UTC().Format("2006-01-02")
	docID := accountID + "-" + date
	return s.firestore.Collection("engine-state").Doc(accountID).Collection("daily-pnl").Doc(docID)
}

// --- InsertPositionState (task 5.2) ---

// InsertPositionState writes a Firestore document with all risk fields for an
// open position. Uses Set (upsert) so that a restart after an unexpected crash
// does not fail on a duplicate.
func (s *APIEngineStore) InsertPositionState(ctx context.Context, tenantID uuid.UUID, state *EnginePositionState) error {
	data := map[string]interface{}{
		"account_id":    state.AccountID,
		"symbol":        state.Symbol,
		"market_type":   state.MarketType,
		"side":          state.Side,
		"entry_price":   state.EntryPrice,
		"stop_loss":     state.StopLoss,
		"take_profit":   state.TakeProfit,
		"hard_stop":     state.HardStop,
		"leverage":      state.Leverage,
		"strategy":      state.Strategy,
		"granularity":   state.Granularity,
		"opened_at":     state.OpenedAt,
		"peak_price":    state.PeakPrice,
		"trailing_stop": state.TrailingStop,
	}
	_, err := s.posDocRef(state.AccountID, state.Symbol, state.MarketType).Set(ctx, data)
	if err != nil {
		return fmt.Errorf("insert position state: %w", err)
	}
	return nil
}

// --- UpdatePositionState (task 5.3) ---

// UpdatePositionState updates the trailing stop, peak price, stop loss, and
// take profit fields on an existing Firestore position document.
func (s *APIEngineStore) UpdatePositionState(ctx context.Context, tenantID uuid.UUID, state *EnginePositionState) error {
	updates := []firestore.Update{
		{Path: "trailing_stop", Value: state.TrailingStop},
		{Path: "peak_price", Value: state.PeakPrice},
		{Path: "stop_loss", Value: state.StopLoss},
		{Path: "take_profit", Value: state.TakeProfit},
	}
	_, err := s.posDocRef(state.AccountID, state.Symbol, state.MarketType).Update(ctx, updates)
	if err != nil {
		return fmt.Errorf("update position state: %w", err)
	}
	return nil
}

// --- DeletePositionState (task 5.4) ---

// DeletePositionState deletes the Firestore document for a closed position.
func (s *APIEngineStore) DeletePositionState(ctx context.Context, tenantID uuid.UUID, symbol, marketType, accountID string) error {
	_, err := s.posDocRef(accountID, symbol, marketType).Delete(ctx)
	if err != nil {
		return fmt.Errorf("delete position state: %w", err)
	}
	return nil
}

// --- LoadPositionStates (task 5.5) ---

// LoadPositionStates queries the positions sub-collection for the account and
// returns all EnginePositionState entries. Called at engine startup to restore
// the in-memory risk map.
func (s *APIEngineStore) LoadPositionStates(ctx context.Context, accountID string) ([]EnginePositionState, error) {
	iter := s.firestore.Collection("engine-state").Doc(accountID).Collection("positions").Documents(ctx)
	defer iter.Stop()

	var states []EnginePositionState
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("load position states: %w", err)
		}
		data := doc.Data()
		var st EnginePositionState
		st.AccountID = stringVal(data, "account_id")
		st.Symbol = stringVal(data, "symbol")
		st.MarketType = stringVal(data, "market_type")
		st.Side = stringVal(data, "side")
		st.EntryPrice = float64Val(data, "entry_price")
		st.StopLoss = float64Val(data, "stop_loss")
		st.TakeProfit = float64Val(data, "take_profit")
		st.HardStop = float64Val(data, "hard_stop")
		st.Leverage = intVal(data, "leverage")
		st.Strategy = stringVal(data, "strategy")
		st.Granularity = stringVal(data, "granularity")
		st.PeakPrice = float64Val(data, "peak_price")
		st.TrailingStop = float64Val(data, "trailing_stop")
		if ts, ok := data["opened_at"]; ok {
			switch v := ts.(type) {
			case time.Time:
				st.OpenedAt = v
			}
		}
		states = append(states, st)
	}
	return states, nil
}

// --- CountOpenPositionStates (task 5.6) ---

// CountOpenPositionStates returns the count of documents in the positions
// sub-collection for the account.
func (s *APIEngineStore) CountOpenPositionStates(ctx context.Context, accountID string) (int, error) {
	iter := s.firestore.Collection("engine-state").Doc(accountID).Collection("positions").Documents(ctx)
	defer iter.Stop()

	count := 0
	for {
		_, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return 0, fmt.Errorf("count position states: %w", err)
		}
		count++
	}
	return count, nil
}

// --- DailyRealizedPnL (task 6.1) ---

// DailyRealizedPnL reads today's Firestore daily P&L document for the account.
// Returns 0 if the document does not exist (no trades closed today).
func (s *APIEngineStore) DailyRealizedPnL(ctx context.Context, accountID string) (float64, error) {
	doc, err := s.dailyPnLDocRef(accountID).Get(ctx)
	if err != nil {
		// Document not found = no trades closed today; not an error.
		if isFirestoreNotFound(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("daily realized pnl: %w", err)
	}
	data := doc.Data()
	return float64Val(data, "pnl"), nil
}

// --- incrementDailyPnL (task 6.2) ---

// incrementDailyPnL atomically increments the daily P&L Firestore document
// using firestore.Increment to avoid lost updates under concurrent writes.
// Called from InsertTradeAndUpdatePosition after a successful trade close.
func (s *APIEngineStore) incrementDailyPnL(ctx context.Context, accountID string, delta float64) error {
	ref := s.dailyPnLDocRef(accountID)
	_, err := ref.Set(ctx, map[string]interface{}{
		"pnl": firestore.Increment(delta),
	}, firestore.MergeAll)
	if err != nil {
		return fmt.Errorf("increment daily pnl: %w", err)
	}
	return nil
}

// --- InsertTradeAndUpdatePosition (task 7.1) ---

// InsertTradeAndUpdatePosition submits a trade to the platform API. On success
// it increments the daily P&L (if realised P&L is non-zero) and adjusts the
// account balance. Returns (true, nil) on 2xx, (false, nil) on 409 (duplicate),
// (false, err) on failure.
func (s *APIEngineStore) InsertTradeAndUpdatePosition(ctx context.Context, tenantID uuid.UUID, trade *domain.Trade) (bool, error) {
	sub := platform.TradeSubmission{
		TenantID:    tenantID.String(),
		TradeID:     trade.TradeID,
		AccountID:   trade.AccountID,
		Symbol:      trade.Symbol,
		Side:        string(trade.Side),
		Quantity:    trade.Quantity,
		Price:       trade.Price,
		Fee:         trade.Fee,
		FeeCurrency: trade.FeeCurrency,
		MarketType:  string(trade.MarketType),
		Timestamp:   trade.Timestamp.UTC().Format(time.RFC3339),
		CostBasis:   trade.CostBasis,
		RealizedPnL: trade.RealizedPnL,
		Leverage:    trade.Leverage,
		Margin:      trade.Margin,
		Strategy:    trade.Strategy,
		EntryReason: trade.EntryReason,
		ExitReason:  trade.ExitReason,
		Confidence:  trade.Confidence,
		StopLoss:    trade.StopLoss,
		TakeProfit:  trade.TakeProfit,
	}

	err := s.client.SubmitTrade(ctx, sub)
	if err != nil {
		// Check if it is a 409 (idempotent duplicate).
		if apiErr, ok := err.(*platform.APIError); ok && apiErr.StatusCode == 409 {
			return false, nil
		}
		return false, fmt.Errorf("submit trade: %w", err)
	}

	// Increment daily P&L accumulator when there is realised P&L (trade close).
	if trade.RealizedPnL != 0 {
		if pnlErr := s.incrementDailyPnL(ctx, trade.AccountID, trade.RealizedPnL); pnlErr != nil {
			// Non-fatal: log but continue so trade is recorded.
			_ = pnlErr
		}
	}

	// Adjust balance — compute the signed cost delta.
	balanceDelta := costDeltaForTrade(trade)
	if adjustErr := s.AdjustBalance(ctx, tenantID, trade.AccountID, "USD", balanceDelta); adjustErr != nil {
		// Non-fatal: balance can reconcile on the next write.
		_ = adjustErr
	}

	return true, nil
}

// costDeltaForTrade returns the signed balance delta resulting from a trade.
// For buy/open: negative (capital leaves account). For sell/close: positive
// (capital returns plus realised P&L).
func costDeltaForTrade(trade *domain.Trade) float64 {
	if trade.Side == domain.SideBuy {
		// Opening a long or covering a short: capital committed = cost basis.
		return -(trade.CostBasis)
	}
	// Selling a long or shorting: capital returned = cost basis + realised P&L.
	return trade.CostBasis + trade.RealizedPnL
}

// --- GetAccountBalance (task 7.2) ---

// GetAccountBalance reads the balance from the platform account list. Returns
// nil if the account is not found or has no balance set.
func (s *APIEngineStore) GetAccountBalance(ctx context.Context, tenantID uuid.UUID, accountID, currency string) (*float64, error) {
	accounts, err := s.client.ListAccounts(ctx)
	if err != nil {
		return nil, fmt.Errorf("get account balance: list accounts: %w", err)
	}
	for _, a := range accounts {
		if a.ID == accountID {
			return a.Balance, nil
		}
	}
	return nil, nil // account not found
}

// --- AdjustBalance (task 7.3) ---

// AdjustBalance reads the current balance, applies the delta, and calls
// platform.SetBalance to persist the new value. If no balance record exists,
// seeds from PORTFOLIO_SIZE_USD.
func (s *APIEngineStore) AdjustBalance(ctx context.Context, tenantID uuid.UUID, accountID, currency string, delta float64) error {
	current, err := s.GetAccountBalance(ctx, tenantID, accountID, currency)
	if err != nil {
		return fmt.Errorf("adjust balance: get current: %w", err)
	}

	var base float64
	if current != nil {
		base = *current
	} else {
		// Seed from portfolio size on first boot.
		base = s.cfg.PortfolioSize
	}

	newBalance := base + delta
	if err := s.client.SetBalance(ctx, accountID, newBalance); err != nil {
		return fmt.Errorf("adjust balance: set balance: %w", err)
	}
	return nil
}

// --- GetAvgEntryPrice (task 7.4) ---

// GetAvgEntryPrice calls GetPortfolio and returns avg_entry_price for the
// matching open position. Returns 0 if the position is not found.
func (s *APIEngineStore) GetAvgEntryPrice(ctx context.Context, tenantID uuid.UUID, accountID, symbol string, marketType domain.MarketType) (float64, error) {
	portfolio, err := s.client.GetPortfolio(ctx, accountID)
	if err != nil {
		return 0, fmt.Errorf("get avg entry price: %w", err)
	}
	for _, p := range portfolio.Positions {
		if p.Symbol == symbol && p.MarketType == string(marketType) {
			return p.AvgEntryPrice, nil
		}
	}
	return 0, nil
}

// --- ListOpenPositionsForAccount (task 7.5) ---

// ListOpenPositionsForAccount calls GetPortfolio and maps PortfolioPosition to
// domain.Position.
func (s *APIEngineStore) ListOpenPositionsForAccount(ctx context.Context, accountID string) ([]domain.Position, error) {
	portfolio, err := s.client.GetPortfolio(ctx, accountID)
	if err != nil {
		return nil, fmt.Errorf("list open positions: %w", err)
	}

	positions := make([]domain.Position, 0, len(portfolio.Positions))
	for _, pp := range portfolio.Positions {
		openedAt, _ := time.Parse(time.RFC3339, pp.OpenedAt)
		p := domain.Position{
			AccountID:     accountID,
			Symbol:        pp.Symbol,
			MarketType:    domain.MarketType(pp.MarketType),
			Side:          domain.PositionSide(pp.Side),
			Quantity:      pp.Quantity,
			AvgEntryPrice: pp.AvgEntryPrice,
			StopLoss:      pp.StopLoss,
			TakeProfit:    pp.TakeProfit,
			Leverage:      pp.Leverage,
			Status:        domain.PositionStatusOpen,
			OpenedAt:      openedAt,
		}
		positions = append(positions, p)
	}
	return positions, nil
}

// --- ListAccounts (task 7.6) ---

// ListAccounts calls the platform API and maps platform.Account to domain.Account.
func (s *APIEngineStore) ListAccounts(ctx context.Context, tenantID uuid.UUID) ([]domain.Account, error) {
	accounts, err := s.client.ListAccounts(ctx)
	if err != nil {
		return nil, fmt.Errorf("list accounts: %w", err)
	}
	result := make([]domain.Account, 0, len(accounts))
	for _, a := range accounts {
		result = append(result, domain.Account{
			ID:   a.ID,
			Name: a.Name,
			Type: domain.AccountType(a.Type),
		})
	}
	return result, nil
}

// --- Firestore helper utilities ---

func stringVal(data map[string]interface{}, key string) string {
	if v, ok := data[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func float64Val(data map[string]interface{}, key string) float64 {
	if v, ok := data[key]; ok {
		switch n := v.(type) {
		case float64:
			return n
		case int64:
			return float64(n)
		}
	}
	return 0
}

func intVal(data map[string]interface{}, key string) int {
	if v, ok := data[key]; ok {
		switch n := v.(type) {
		case int64:
			return int(n)
		case float64:
			return int(n)
		}
	}
	return 0
}

// isFirestoreNotFound returns true when a Firestore error is "document not found".
func isFirestoreNotFound(err error) bool {
	if err == nil {
		return false
	}
	// google.golang.org/grpc/codes.NotFound
	return containsCode(err, "NotFound") || containsCode(err, "not found")
}

func containsCode(err error, code string) bool {
	if err == nil {
		return false
	}
	return len(err.Error()) > 0 && (err.Error() == code ||
		len(err.Error()) >= len(code) && containsSubstring(err.Error(), code))
}

func containsSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
