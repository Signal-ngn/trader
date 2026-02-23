package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"

	"github.com/rs/zerolog/log"

	"github.com/Spot-Canvas/ledger/internal/api/middleware"
	"github.com/Spot-Canvas/ledger/internal/domain"
	"github.com/Spot-Canvas/ledger/internal/ingest"
	"github.com/Spot-Canvas/ledger/internal/store"
)

// ImportRequest is the request body for POST /api/v1/import.
type ImportRequest struct {
	Trades []ingest.TradeEvent `json:"trades"`
}

// ImportResult holds the result of a single trade import.
type ImportResult struct {
	TradeID string `json:"trade_id"`
	Status  string `json:"status"` // "inserted", "duplicate", "error"
	Error   string `json:"error,omitempty"`
}

// ImportResponse is the response body for POST /api/v1/import.
type ImportResponse struct {
	Total      int            `json:"total"`
	Inserted   int            `json:"inserted"`
	Duplicates int            `json:"duplicates"`
	Errors     int            `json:"errors"`
	Results    []ImportResult `json:"results"`
}

func (s *Server) handleImportTrades(w http.ResponseWriter, r *http.Request) {
	tenantID := middleware.TenantIDFromContext(r.Context())

	var req ImportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid JSON: %v", err))
		return
	}

	if len(req.Trades) == 0 {
		writeError(w, http.StatusBadRequest, "trades array is empty")
		return
	}

	if len(req.Trades) > 1000 {
		writeError(w, http.StatusBadRequest, "too many trades: max 1000 per request")
		return
	}

	// Validate all trades up front before inserting any.
	// For the import endpoint the tenant_id comes from the auth context,
	// so we don't require it in the event payload — we inject it below.
	for i, event := range req.Trades {
		// Temporarily inject tenantID so Validate() passes the tenant_id check.
		req.Trades[i].TenantID = tenantID.String()
		if err := req.Trades[i].Validate(); err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("trade[%d] (%s): %v", i, event.TradeID, err))
			return
		}
	}

	// Sort by timestamp ascending for correct position calculation
	sort.Slice(req.Trades, func(i, j int) bool {
		return req.Trades[i].Timestamp < req.Trades[j].Timestamp
	})

	ctx := r.Context()
	resp := ImportResponse{
		Total:   len(req.Trades),
		Results: make([]ImportResult, 0, len(req.Trades)),
	}

	// Collect accounts that need position rebuilds
	affectedAccounts := make(map[string]bool)

	for _, event := range req.Trades {
		result := ImportResult{TradeID: event.TradeID}

		trade, err := event.ToDomain()
		if err != nil {
			result.Status = "error"
			result.Error = err.Error()
			resp.Errors++
			resp.Results = append(resp.Results, result)
			continue
		}

		// Override TenantID from context (import always uses the caller's tenant)
		trade.TenantID = tenantID

		// Ensure account exists (scoped to tenant)
		accountType := domain.InferAccountType(event.AccountID)
		if _, err := s.repo.GetOrCreateAccount(ctx, tenantID, trade.AccountID, accountType); err != nil {
			result.Status = "error"
			result.Error = fmt.Sprintf("account setup failed: %v", err)
			resp.Errors++
			resp.Results = append(resp.Results, result)
			continue
		}

		// Calculate cost basis for sells
		if trade.Side == domain.SideSell {
			avgPrice, err := s.repo.GetAvgEntryPrice(ctx, tenantID, trade.AccountID, trade.Symbol, trade.MarketType)
			if err != nil {
				result.Status = "error"
				result.Error = fmt.Sprintf("cost basis lookup failed: %v", err)
				resp.Errors++
				resp.Results = append(resp.Results, result)
				continue
			}
			store.CostBasisForTrade(trade, avgPrice)
		}

		inserted, err := s.repo.InsertTradeAndUpdatePosition(ctx, tenantID, trade)
		if err != nil {
			result.Status = "error"
			result.Error = err.Error()
			resp.Errors++
			resp.Results = append(resp.Results, result)
			continue
		}

		if inserted {
			result.Status = "inserted"
			resp.Inserted++
			affectedAccounts[trade.AccountID] = true
		} else {
			result.Status = "duplicate"
			resp.Duplicates++
		}
		resp.Results = append(resp.Results, result)
	}

	// Rebuild positions for affected accounts to ensure consistency
	// (historic imports may arrive out of order relative to existing trades)
	for accountID := range affectedAccounts {
		if err := s.repo.RebuildPositions(ctx, tenantID, accountID); err != nil {
			log.Error().Err(err).
				Str("tenant_id", tenantID.String()).
				Str("account_id", accountID).
				Msg("failed to rebuild positions after import")
		}
	}

	status := http.StatusOK
	if resp.Errors > 0 && resp.Inserted == 0 {
		status = http.StatusUnprocessableEntity
	}
	writeJSON(w, status, resp)
}
