---
name: record-trade
description: Record trades to the Spot Canvas trading ledger. Use when an AI trading agent/bot executes a trade and needs to persist it — covers entries, exits, spot, and leveraged futures with full strategy metadata.
allowed-tools: Bash
---

# Record Trade

Record executed trades to the Spot Canvas trading ledger via its REST import API.

**Endpoint:** `POST /api/v1/import`

The ledger URL depends on the environment:
- **Staging:** `https://spot-canvas-ledger-staging-uumkospiua-ey.a.run.app`
- **Production:** Use the `LEDGER_URL` environment variable if set, otherwise ask the user.

## Recording a Trade

Use `curl` to POST a JSON body with one or more trades.

```bash
curl -s -X POST "${LEDGER_URL:-https://spot-canvas-ledger-staging-uumkospiua-ey.a.run.app}/api/v1/import" \
  -H "Content-Type: application/json" \
  -d '{
    "trades": [{
      "trade_id": "<unique-id>",
      "account_id": "<account>",
      "symbol": "<pair>",
      "side": "<buy|sell>",
      "quantity": <number>,
      "price": <number>,
      "fee": <number>,
      "fee_currency": "<currency>",
      "market_type": "<spot|futures>",
      "timestamp": "<RFC3339>",
      "strategy": "<strategy-name>",
      "entry_reason": "<signal description>",
      "exit_reason": "<exit description>",
      "confidence": <0-1>,
      "stop_loss": <price>,
      "take_profit": <price>,
      "leverage": <integer>,
      "margin": <number>,
      "liquidation_price": <number>,
      "funding_fee": <number>
    }]
  }'
```

## Field Reference

### Required Fields

| Field | Type | Description | Example |
|-------|------|-------------|---------|
| `trade_id` | string | Unique trade identifier. Use exchange trade ID or generate a UUID. Must be unique — duplicates are silently skipped. | `"binance-12345"` |
| `account_id` | string | Account identifier. Accounts are auto-created on first use. Use `"live"` or `"paper"` convention. | `"live"` |
| `symbol` | string | Trading pair. | `"BTC-USD"` |
| `side` | string | `"buy"` or `"sell"` | `"buy"` |
| `quantity` | number | Trade quantity (must be > 0). | `0.5` |
| `price` | number | Execution price (must be > 0). | `50000` |
| `fee` | number | Fee amount. Use `0` if no fee. | `25.00` |
| `fee_currency` | string | Currency the fee is denominated in. | `"USD"` |
| `market_type` | string | `"spot"` or `"futures"` | `"spot"` |
| `timestamp` | string | Trade execution time in RFC3339 format. | `"2025-06-15T10:30:00Z"` |

### Strategy Metadata (optional)

| Field | Type | Description | Example |
|-------|------|-------------|---------|
| `strategy` | string | Strategy name that generated the signal. | `"macd-rsi-v2"` |
| `entry_reason` | string | Signal reason text at entry. Use on buy trades. | `"MACD bullish crossover, RSI 42"` |
| `exit_reason` | string | Reason for closing. Use on sell trades. | `"stop loss hit"`, `"take profit reached"` |
| `confidence` | number | Signal confidence score, 0–1. | `0.85` |
| `stop_loss` | number | Stop loss price level. | `48000` |
| `take_profit` | number | Take profit price level. | `55000` |

### Futures Fields (optional, for `market_type: "futures"` only)

| Field | Type | Description | Example |
|-------|------|-------------|---------|
| `leverage` | integer | Leverage multiplier. | `10` |
| `margin` | number | Margin amount. | `5000` |
| `liquidation_price` | number | Liquidation price. | `45000` |
| `funding_fee` | number | Funding fee amount. | `12.50` |

## Response Format

```json
{
  "total": 1,
  "inserted": 1,
  "duplicates": 0,
  "errors": 0,
  "results": [
    { "trade_id": "binance-12345", "status": "inserted" }
  ]
}
```

Status per trade is one of: `"inserted"`, `"duplicate"`, `"error"`.

## Examples

### Spot buy with strategy metadata

```bash
curl -s -X POST "${LEDGER_URL:-https://spot-canvas-ledger-staging-uumkospiua-ey.a.run.app}/api/v1/import" \
  -H "Content-Type: application/json" \
  -d '{
    "trades": [{
      "trade_id": "bot-'$(date +%s)'",
      "account_id": "live",
      "symbol": "BTC-USD",
      "side": "buy",
      "quantity": 0.5,
      "price": 50000,
      "fee": 25,
      "fee_currency": "USD",
      "market_type": "spot",
      "timestamp": "'$(date -u +%Y-%m-%dT%H:%M:%SZ)'",
      "strategy": "macd-rsi-v2",
      "entry_reason": "MACD bullish crossover, RSI 42",
      "confidence": 0.85,
      "stop_loss": 48000,
      "take_profit": 55000
    }]
  }'
```

### Spot sell (closing position)

```bash
curl -s -X POST "${LEDGER_URL:-https://spot-canvas-ledger-staging-uumkospiua-ey.a.run.app}/api/v1/import" \
  -H "Content-Type: application/json" \
  -d '{
    "trades": [{
      "trade_id": "bot-'$(date +%s)'",
      "account_id": "live",
      "symbol": "BTC-USD",
      "side": "sell",
      "quantity": 0.5,
      "price": 55000,
      "fee": 27.50,
      "fee_currency": "USD",
      "market_type": "spot",
      "timestamp": "'$(date -u +%Y-%m-%dT%H:%M:%SZ)'",
      "exit_reason": "take profit reached"
    }]
  }'
```

### Leveraged futures trade

```bash
curl -s -X POST "${LEDGER_URL:-https://spot-canvas-ledger-staging-uumkospiua-ey.a.run.app}/api/v1/import" \
  -H "Content-Type: application/json" \
  -d '{
    "trades": [{
      "trade_id": "bot-futures-'$(date +%s)'",
      "account_id": "live",
      "symbol": "ETH-USD",
      "side": "buy",
      "quantity": 10,
      "price": 3000,
      "fee": 6,
      "fee_currency": "USD",
      "market_type": "futures",
      "timestamp": "'$(date -u +%Y-%m-%dT%H:%M:%SZ)'",
      "strategy": "funding-arb",
      "confidence": 0.72,
      "leverage": 5,
      "margin": 6000,
      "liquidation_price": 2500,
      "stop_loss": 2800,
      "take_profit": 3300
    }]
  }'
```

### Batch import (multiple trades)

Up to 1000 trades per request. Trades are automatically sorted by timestamp for correct position calculation.

```bash
curl -s -X POST "${LEDGER_URL:-https://spot-canvas-ledger-staging-uumkospiua-ey.a.run.app}/api/v1/import" \
  -H "Content-Type: application/json" \
  -d '{
    "trades": [
      { "trade_id": "t1", "account_id": "paper", "symbol": "BTC-USD", "side": "buy", "quantity": 1.0, "price": 40000, "fee": 20, "fee_currency": "USD", "market_type": "spot", "timestamp": "2025-06-01T10:00:00Z" },
      { "trade_id": "t2", "account_id": "paper", "symbol": "BTC-USD", "side": "sell", "quantity": 1.0, "price": 42000, "fee": 21, "fee_currency": "USD", "market_type": "spot", "timestamp": "2025-06-15T10:00:00Z", "exit_reason": "target hit" }
    ]
  }'
```

## Querying the Ledger

After recording trades, you can query positions and trade history.

### Get account stats (win rate, realized P&L, trade count)

REST endpoint — returns aggregate stats computed from round-trips (closed positions):

```bash
curl -s "${LEDGER_URL:-https://spot-canvas-ledger-staging-uumkospiua-ey.a.run.app}/api/v1/accounts/live/stats" | python3 -m json.tool
```

Response:
```json
{
  "total_trades": 116,
  "closed_trades": 109,
  "win_count": 79,
  "loss_count": 30,
  "win_rate": 0.7248,
  "total_realized_pnl": 2555.28,
  "open_positions": 7
}
```

`total_trades` = closed + open positions (round-trips, not raw buy/sell records).

CLI equivalent:

```bash
ledger accounts show live            # human-readable table
ledger accounts show live --json     # raw JSON
```

### Check open positions

```bash
curl -s "${LEDGER_URL:-https://spot-canvas-ledger-staging-uumkospiua-ey.a.run.app}/api/v1/accounts/live/positions?status=open" | python3 -m json.tool
```

### Check closed positions (with exit_price, exit_reason)

```bash
curl -s "${LEDGER_URL:-https://spot-canvas-ledger-staging-uumkospiua-ey.a.run.app}/api/v1/accounts/live/positions?status=closed" | python3 -m json.tool
```

### Get portfolio summary

```bash
curl -s "${LEDGER_URL:-https://spot-canvas-ledger-staging-uumkospiua-ey.a.run.app}/api/v1/accounts/live/portfolio" | python3 -m json.tool
```

### List recent trades

```bash
curl -s "${LEDGER_URL:-https://spot-canvas-ledger-staging-uumkospiua-ey.a.run.app}/api/v1/accounts/live/trades?limit=10" | python3 -m json.tool
```

### Filter trades by symbol

```bash
curl -s "${LEDGER_URL:-https://spot-canvas-ledger-staging-uumkospiua-ey.a.run.app}/api/v1/accounts/live/trades?symbol=BTC-USD&limit=20" | python3 -m json.tool
```

## `ledger` CLI Reference

The `ledger` CLI wraps the REST API for human use. Key commands:

```bash
ledger accounts list                 # list all accounts for the tenant
ledger accounts show <account-id>    # show aggregate stats (win rate, P&L, trade count)
ledger accounts show <account-id> --json  # raw JSON stats

ledger trades list --account live    # list recent trades
ledger positions list --account live # list positions

ledger import --file trades.csv      # bulk import trades from CSV
```

## Important Notes

- **Idempotent:** Submitting the same `trade_id` twice results in a `"duplicate"` — no error, no double-counting.
- **Auto-creates accounts:** If the `account_id` doesn't exist, it's created automatically (`"live"` → live type, `"paper"` → paper type).
- **Position tracking is automatic:** The ledger maintains positions from trade history. Buy trades open/increase positions, sell trades reduce/close them.
- **Metadata on positions:** `stop_loss`, `take_profit`, and `confidence` are copied from the opening trade to the position. `exit_price` and `exit_reason` are set when the position closes.
- **Batch max:** 1000 trades per request.
- **Timestamps:** Always use RFC3339 format (e.g., `2025-06-15T10:30:00Z`).
