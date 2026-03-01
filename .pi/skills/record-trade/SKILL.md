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

## Account Balance

The ledger tracks a cash balance per account. The trading agent should set an initial balance before trading and query it before sizing positions. **Balance is automatically adjusted by trade ingestion** — opening a position deducts the cost and closing a position credits the realised P&L.

### Set (or reset) account balance

Use `PUT /api/v1/accounts/{accountId}/balance` to set an initial balance or to manually correct it after broker reconciliation. This **overwrites** the current value unconditionally.

```bash
curl -s -X PUT "${LEDGER_URL:-https://spot-canvas-ledger-staging-uumkospiua-ey.a.run.app}/api/v1/accounts/live/balance" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer ${LEDGER_API_KEY}" \
  -d '{"amount": 50000, "currency": "USD"}'
```

Response:
```json
{ "account_id": "live", "currency": "USD", "amount": 50000 }
```

`currency` defaults to `"USD"` if omitted.

CLI equivalent:

```bash
ledger accounts balance set live 50000               # set USD balance
ledger accounts balance set live 40000 --currency EUR  # set EUR balance
ledger accounts balance set live 50000 --json        # raw JSON response
```

### Query account balance

```bash
curl -s "${LEDGER_URL:-https://spot-canvas-ledger-staging-uumkospiua-ey.a.run.app}/api/v1/accounts/live/balance" \
  -H "Authorization: Bearer ${LEDGER_API_KEY}"
```

Response when set:
```json
{ "account_id": "live", "currency": "USD", "amount": 47250.00 }
```

Returns HTTP 404 when no balance has been set for the account.

Optional `?currency=EUR` query parameter selects a non-default currency.

CLI equivalent:

```bash
ledger accounts balance get live               # show balance table
ledger accounts balance get live --currency EUR  # EUR balance
ledger accounts balance get live --json        # raw JSON
```

### How automatic balance adjustment works

When a trade is ingested the ledger adjusts the USD balance **within the same transaction** as the position update. The adjustment is a **no-op** if no balance row exists for the account — no balance row is ever auto-created.

| Event | Balance change |
|---|---|
| Spot buy (open / add to position) | − `quantity × price + fee` |
| Spot sell (partial or full close) | + realised P&L |
| Futures open | − margin (uses `margin` field; falls back to `cost_basis / leverage`; skipped if neither available) |
| Futures close (partial or full) | + realised P&L (leverage- and fee-adjusted) |

Position **rebuild** does not touch the balance — it only reconstructs position state.

### Balance in portfolio and stats responses

When a balance has been set, it is included in the portfolio summary and account stats responses as an optional `"balance"` field. It is omitted entirely when no balance row exists.

```bash
curl -s "${LEDGER_URL:-...}/api/v1/accounts/live/portfolio" | python3 -m json.tool
# → { "positions": [...], "total_realized_pnl": 1234.5, "balance": 47250.00 }

curl -s "${LEDGER_URL:-...}/api/v1/accounts/live/stats" | python3 -m json.tool
# → { ..., "total_realized_pnl": 1234.5, "balance": 47250.00 }
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
  "open_positions": 7,
  "balance": 47250.00
}
```

`total_trades` = closed + open positions (round-trips, not raw buy/sell records). `balance` is omitted when no balance has been set.

CLI equivalent:

```bash
ledger accounts show live            # human-readable table (includes balance row when set)
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
ledger accounts list                                    # list all accounts for the tenant
ledger accounts show <account-id>                       # show aggregate stats (win rate, P&L, trade count, balance)
ledger accounts show <account-id> --json                # raw JSON stats

ledger accounts balance set <account-id> <amount>       # set/overwrite account balance (USD)
ledger accounts balance set <account-id> <amount> --currency EUR  # set non-USD balance
ledger accounts balance get <account-id>                # query current balance
ledger accounts balance get <account-id> --currency EUR # query non-USD balance

ledger trades list <account-id>             # round-trip view: one row per position (default)
ledger trades list <account-id> --raw       # raw trade view: one row per individual trade leg
ledger trades list <account-id> --limit 20  # show last 20 positions (default: 50, 0 = all)
ledger trades list <account-id> --long      # show all columns: ID, full timestamps, exit reason
                                            # default (--short): no ID, compact times (no year)
```

### Round-trip view (default)

Shows one row per position. Columns: RESULT, SYMBOL, DIR, SIZE, ENTRY, EXIT, P&L, P&L%, OPENED, CLOSED.

- `RESULT`: `✓ win`, `✗ loss`, or `open`
- `SIZE`: cost basis in USD
- `ENTRY` / `EXIT`: avg entry price and exit price
- `P&L` / `P&L%`: realized P&L amount and percentage
- `OPENED` / `CLOSED`: compact timestamps (`MM-DD HH:MM:SS`); `--long` shows full `YYYY-MM-DD HH:MM:SS`

A win/loss summary is printed below the table, calculated from the displayed rows:

```
5 wins  2 losses  71% win rate  (7 closed)
```

Open positions are excluded from the win/loss count.

### Raw trade view (`--raw`)

Shows one row per individual trade leg. Columns (short): SYMBOL, SIDE, QTY, PRICE, FEE, MARKET, TIME.  
With `--long`: adds the TRADE-ID column and shows full timestamps.

Number formatting:
- **QTY**: whole numbers for large quantities (e.g. `52447552` not `5.24e+07`)
- **PRICE**: up to 6 decimal places for normal prices; scientific notation (`2.860e-05`) for micro-prices
- **FEE**: max 4 decimal places, trailing zeros trimmed

## Deleting a Trade

Use this **only to remove test trades** that were recorded by mistake or during testing. Do not use it to delete real trading history.

A trade can only be deleted if its account/symbol has **no open position**. If the trade contributed to an open position, close the position first (record the offsetting sell/buy trade), then delete the test trades.

### Via CLI

```bash
ledger trades delete <trade-id> --confirm
```

The `--confirm` flag is required to prevent accidental deletion.

**Examples:**

```bash
# Delete a specific test trade
ledger trades delete bot-1234567890 --confirm

# Delete and get JSON response
ledger trades delete bot-1234567890 --confirm --json
```

**Exit codes and messages:**

| Situation | Output | Exit code |
|-----------|--------|-----------|
| Success | `deleted trade <id>` | 0 |
| Missing `--confirm` | `use --confirm to delete a trade` | non-zero |
| Trade not found | `trade not found` | non-zero |
| Trade has open position | server error message | non-zero |

### Via REST API

```bash
curl -s -X DELETE \
  "${LEDGER_URL:-https://spot-canvas-ledger-staging-uumkospiua-ey.a.run.app}/api/v1/trades/<trade-id>" \
  -H "Authorization: Bearer ${LEDGER_API_KEY}"
```

**Responses:**

| Status | Meaning |
|--------|---------|
| `200 {"deleted": "<id>"}` | Trade deleted successfully |
| `404 {"error": "trade not found"}` | Trade doesn't exist or belongs to another tenant |
| `409 {"error": "trade contributes to an open position and cannot be deleted"}` | Close the position first |
| `401` | Invalid or missing API key |

## Important Notes

- **Idempotent:** Submitting the same `trade_id` twice results in a `"duplicate"` — no error, no double-counting.
- **Auto-creates accounts:** If the `account_id` doesn't exist, it's created automatically (`"live"` → live type, `"paper"` → paper type).
- **Position tracking is automatic:** The ledger maintains positions from trade history. Buy trades open/increase positions, sell trades reduce/close them.
- **Metadata on positions:** `stop_loss`, `take_profit`, and `confidence` are copied from the opening trade to the position. `exit_price` and `exit_reason` are set when the position closes.
- **Batch max:** 1000 trades per request.
- **Timestamps:** Always use RFC3339 format (e.g., `2025-06-15T10:30:00Z`).
