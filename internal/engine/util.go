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

// costBasisForTrade calculates the appropriate cost_basis and realized_pnl for
// a trade. Moved from internal/store so the engine has no store dependency.
func costBasisForTrade(trade *domain.Trade, avgEntryPrice float64) {
	if trade.Side == domain.SideBuy {
		trade.CostBasis = trade.Quantity*trade.Price + trade.Fee
		trade.RealizedPnL = 0
	} else {
		trade.CostBasis = avgEntryPrice * trade.Quantity
		trade.RealizedPnL = (trade.Price-avgEntryPrice)*trade.Quantity - trade.Fee
	}
}
