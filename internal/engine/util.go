package engine

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"

	"github.com/Signal-ngn/trader/internal/domain"
)

// hmacSHA256 signs the given message with the secret using HMAC-SHA256.
// Returns the hex-encoded signature.
func hmacSHA256(secret, message string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(message))
	b := mac.Sum(nil)
	return fmt.Sprintf("%x", b)
}

// decodeJSON decodes a JSON response body into v.
func decodeJSON(r io.Reader, v interface{}) error {
	return json.NewDecoder(r).Decode(v)
}

// costBasisForTrade calculates the cost_basis (margin) and realized_pnl for a
// close trade. Called only from executeCloseTrade.
//
// CostBasis is set to the margin that was originally committed for this
// position (= avgEntryPrice × quantity / leverage). For spot (leverage 1) this
// is the full notional, matching the old behaviour.
//
// RealizedPnL is computed with the correct sign for each position side:
//
//	long  close (sell+long):  P&L = (exitPrice − avgEntry) × qty − fee
//	short close (buy+short):  P&L = (avgEntry − exitPrice) × qty − fee
func costBasisForTrade(trade *domain.Trade, avgEntryPrice float64) {
	leverage := 1.0
	if trade.Leverage != nil && *trade.Leverage > 0 {
		leverage = float64(*trade.Leverage)
	}

	// Margin committed at open.
	margin := avgEntryPrice * trade.Quantity / leverage

	if trade.PositionSide == domain.PositionSideShort {
		// Close short (buy+short): profit when price fell below entry.
		trade.CostBasis = margin
		trade.RealizedPnL = (avgEntryPrice-trade.Price)*trade.Quantity - trade.Fee
	} else {
		// Close long (sell+long) or legacy spot sell: profit when price rose.
		trade.CostBasis = margin
		trade.RealizedPnL = (trade.Price-avgEntryPrice)*trade.Quantity - trade.Fee
	}
}
