---
name: ledger
description: Record trades, query positions, and inspect portfolio state using the `ledger` CLI. Use this skill when a trading bot needs to persist executed trades, check open positions before sizing a new trade, query realized P&L, or import historic trade history.
allowed-tools: Bash
---

# ledger — Spot Canvas Trading Ledger CLI

`ledger` is the command-line interface for the Spot Canvas ledger service. Trading bots use it to record executed trades, query live portfolio state, and inspect trade history.

## Prerequisites

### Install

```bash
# Go toolchain
go install github.com/Spot-Canvas/ledger/cmd/ledger@latest

# Homebrew (macOS)
brew install --cask Spot-Canvas/ledger/ledger
```

### Authenticate

The `ledger` CLI reads your API key from `~/.config/sn/config.yaml` (written by `sn auth login`):

```bash
sn auth login        # one-time browser login
ledger accounts list # verify it works
```

For bots and CI, set the API key directly via environment variable — no config file needed:

```bash
export LEDGER_API_KEY=your-api-key
```

The tenant ID is resolved automatically on first use and cached in `~/.config/ledger/config.yaml`. Override with `LEDGER_TENANT_ID`.

---

## Accounts

```bash
ledger accounts list                        # list all accounts for this tenant
ledger accounts list --json                 # JSON array
ledger accounts show <account-id>           # aggregate stats: trades, win rate, P&L, balance
ledger accounts show <account-id> --json    # raw JSON
```

**Response fields:** `id`, `name`, `type` (`live`/`paper`), `created_at`

Common account IDs: `live` (real trading), `paper` (simulated).

`accounts show` includes a `Balance (USD)` row when a balance has been set, or displays `not set`.

---

## Account Balance

The ledger tracks a cash balance per account. Set an initial balance once, then query it before sizing positions. **Balance is automatically adjusted by trade ingestion** — opening a position deducts the cost and closing a position credits the realised P&L.

```bash
ledger accounts balance set live 50000                    # set USD balance (overwrites any existing value)
ledger accounts balance set live 40000 --currency EUR     # set a non-USD balance
ledger accounts balance get live                          # query current USD balance
ledger accounts balance get live --currency EUR           # query non-USD balance
ledger accounts balance get live --json                   # raw JSON
```

**Automatic balance adjustments during ingestion:**

| Trade event | Balance change |
|---|---|
| Spot buy (open / add to position) | − `quantity × price + fee` |
| Spot sell (partial or full close) | + realised P&L |
| Futures open | − margin (`margin` field; or `cost_basis / leverage`; skipped if neither available) |
| Futures close (partial or full) | + realised P&L (leverage- and fee-adjusted) |

Adjustments are a **no-op** when no balance row exists — the row is never auto-created. Position rebuild does not touch the balance.

`PUT /balance` always overwrites — use it to set an initial balance or to correct it after broker reconciliation.

---

## Portfolio

```bash
ledger portfolio live          # open positions + total realized P&L + balance (when set)
ledger portfolio paper
ledger portfolio live --json
```

**Response fields:**
- `positions[]` — open positions (see Positions below)
- `total_realized_pnl` — sum of realized P&L across all positions (open + closed)
- `balance` — current USD balance (omitted when no balance has been set)

Use this before placing a new trade to check current exposure.

---

## Positions

```bash
ledger positions live                    # open positions (default)
ledger positions live --status closed    # closed positions
ledger positions live --status all       # all positions
ledger positions live --json
```

**Position fields:**

| Field | Description |
|-------|-------------|
| `id` | Position ID |
| `symbol` | e.g. `BTC-USD` |
| `market_type` | `spot` or `futures` |
| `side` | `long` or `short` |
| `quantity` | Current size |
| `avg_entry_price` | Volume-weighted average entry |
| `cost_basis` | Total cost of the position |
| `realized_pnl` | Realized P&L so far |
| `status` | `open` or `closed` |
| `opened_at` | When the position was opened |
| `closed_at` | When it was closed (if closed) |
| `stop_loss` | Stop-loss price (if set) |
| `take_profit` | Take-profit price (if set) |

**Pattern — check if already in a position before trading:**

```bash
# Is there an open BTC-USD position?
ledger positions live --json | jq '.[] | select(.symbol == "BTC-USD" and .status == "open")'
```

---

## Trades

### List trades

```bash
ledger trades list <account-id>                              # 50 most recent trades (default)
ledger trades list live --symbol BTC-USD                     # filter by symbol
ledger trades list live --side buy                           # filter by side: buy or sell
ledger trades list live --market-type futures                # filter by market type
ledger trades list live --start 2025-01-01T00:00:00Z --end 2025-02-01T00:00:00Z
ledger trades list live --limit 200                          # up to 200 results
ledger trades list live --limit 0                            # all trades (follows all cursor pages)
ledger trades list live --json
```

### Record a single trade

```bash
ledger trades add <account-id> --symbol BTC-USD --side buy --quantity 0.1 --price 95000
```

**Required flags:** `--symbol`, `--side`, `--quantity`, `--price`

**Optional flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--trade-id` | auto-generated UUID | Unique trade identifier |
| `--fee` | `0` | Fee paid |
| `--fee-currency` | `USD` | Fee currency |
| `--market-type` | `spot` | `spot` or `futures` |
| `--timestamp` | now | Execution time (RFC3339) |
| `--strategy` | | Strategy name |
| `--entry-reason` | | Why the position was entered |
| `--exit-reason` | | Why the position was exited |
| `--confidence` | | Signal confidence (0–1) |
| `--stop-loss` | | Stop-loss price |
| `--take-profit` | | Take-profit price |
| `--leverage` | | Leverage multiplier (futures) |
| `--margin` | | Margin used (futures) |
| `--liquidation-price` | | Liquidation price (futures) |
| `--funding-fee` | | Funding fee (futures) |

**Examples:**

```bash
# Spot buy with strategy metadata
ledger trades add live \
  --symbol BTC-USD --side buy --quantity 0.1 --price 95000 \
  --fee 9.50 --strategy macd_momentum --confidence 0.78 \
  --stop-loss 93000 --take-profit 99000

# Spot sell (exit)
ledger trades add live \
  --symbol BTC-USD --side sell --quantity 0.1 --price 98000 \
  --fee 9.80 --exit-reason "take-profit hit"

# Futures long with leverage
ledger trades add live \
  --symbol BTC-USD --side buy --quantity 0.5 --price 95000 \
  --market-type futures --leverage 10 --margin 4750

# With explicit trade ID and timestamp
ledger trades add paper \
  --trade-id "bot-20250201-042" \
  --symbol ETH-USD --side buy --quantity 1.0 --price 3200 \
  --timestamp 2025-02-01T10:30:00Z

# JSON output
ledger trades add live --symbol BTC-USD --side buy --quantity 0.1 --price 95000 --json
```

**Trade is idempotent** — re-submitting the same `--trade-id` is safe, returns duplicate status.

### Trade fields (list output)

| Field | Description |
|-------|-------------|
| `trade_id` | Unique trade identifier |
| `symbol` | Trading pair |
| `side` | `buy` or `sell` |
| `quantity` | Trade size |
| `price` | Fill price |
| `fee` | Fee paid |
| `fee_currency` | Fee currency (e.g. `USD`) |
| `market_type` | `spot` or `futures` |
| `timestamp` | Trade execution time (RFC3339) |
| `cost_basis` | Cost basis for this trade |
| `realized_pnl` | Realized P&L for this trade (sell side) |
| `strategy` | Strategy that generated the signal (if set) |
| `entry_reason` | Why the position was entered |
| `exit_reason` | Why the position was exited |
| `confidence` | Signal confidence at entry (0–1) |
| `stop_loss` | Stop-loss at time of trade |
| `take_profit` | Take-profit at time of trade |

**Pattern — get the last trade for a symbol:**

```bash
ledger trades list live --symbol BTC-USD --limit 1 --json | jq '.[0]'
```

---

## Orders

```bash
ledger orders live                       # 50 most recent orders
ledger orders live --status open         # open orders only
ledger orders live --status filled
ledger orders live --symbol BTC-USD
ledger orders live --limit 0 --json      # all orders as JSON
```

**Order fields:** `order_id`, `symbol`, `side`, `order_type` (`market`/`limit`), `requested_qty`, `filled_qty`, `avg_fill_price`, `status` (`open`/`filled`/`partially_filled`/`cancelled`), `market_type`, `created_at`

---

## Import Historic Trades

Use `ledger import` to bulk-load past trades from a JSON file. The service validates all trades up front, inserts them idempotently, and rebuilds positions from the full trade history.

```bash
ledger import trades.json          # import and print summary
ledger import trades.json --json   # full response JSON
```

**File format** — a JSON object with a `"trades"` array:

```json
{
  "trades": [
    {
      "tenant_id": "c2899e28-2bbe-47c1-8d29-84ee1a04fd37",
      "trade_id": "cb-20250101-001",
      "account_id": "live",
      "symbol": "BTC-USD",
      "side": "buy",
      "quantity": 0.1,
      "price": 95000,
      "fee": 9.50,
      "fee_currency": "USD",
      "market_type": "spot",
      "timestamp": "2025-01-01T10:00:00Z",
      "strategy": "macd_momentum",
      "entry_reason": "MACD crossover with RSI < 60",
      "confidence": 0.78,
      "stop_loss": 93000,
      "take_profit": 99000
    }
  ]
}
```

**Output:**
```
Total: 1  Inserted: 1  Duplicates: 0  Errors: 0
```

Exits non-zero if any errors occurred. Re-running the same file is safe (duplicates are skipped).

**Required fields:** `tenant_id`, `trade_id`, `account_id`, `symbol`, `side`, `quantity`, `price`, `fee`, `fee_currency`, `market_type`, `timestamp`

**Optional fields:** `leverage`, `margin`, `liquidation_price`, `funding_fee`, `strategy`, `entry_reason`, `exit_reason`, `confidence`, `stop_loss`, `take_profit`

---

## Config

```bash
ledger config show                              # all resolved values and sources
ledger config set ledger_url https://...        # override service URL
ledger config get ledger_url
```

| Key | Default | Env override |
|-----|---------|-------------|
| `ledger_url` | `https://signalngn-ledger-potbdcvufa-ew.a.run.app` | `LEDGER_URL` |
| `api_key` | _(from `~/.config/sn/config.yaml`)_ | `LEDGER_API_KEY` |
| `tenant_id` | _(resolved via `/auth/resolve` on first use)_ | `LEDGER_TENANT_ID` |

---

## Global Flags

```bash
ledger --ledger-url http://localhost:8080 accounts list   # one-off URL override
ledger --json <any-command>                               # JSON output
```

---

## Trading Bot Patterns

### Check available balance before sizing a position

```bash
# Get current USD balance
BALANCE=$(ledger accounts balance get live --json | jq '.amount')
echo "Available balance: $BALANCE"

# Set initial balance (e.g. after funding the account)
ledger accounts balance set live 50000
```

### Check exposure before entering a trade

```bash
# Get open position size for BTC-USD
SIZE=$(ledger positions live --json | jq '[.[] | select(.symbol == "BTC-USD" and .status == "open")] | map(.quantity) | add // 0')
echo "Current BTC exposure: $SIZE"
```

### Record a trade immediately after execution

```bash
ledger trades add live \
  --trade-id "$EXCHANGE_ORDER_ID" \
  --symbol BTC-USD --side buy --quantity 0.1 --price 95000 \
  --fee 9.50 --strategy macd_momentum --confidence 0.78 \
  --stop-loss 93000 --take-profit 99000
```

### Get realized P&L for the day

```bash
TODAY=$(date -u +%Y-%m-%dT00:00:00Z)
ledger trades list live --start "$TODAY" --json | \
  jq '[.[] | select(.side == "sell")] | map(.realized_pnl) | add // 0'
```

### Verify a trade was recorded after execution

```bash
# After placing a trade with trade_id "cb-20250201-042"
ledger trades list live --json | jq '.[] | select(.trade_id == "cb-20250201-042")'
```

### Import a day's trades from a file

```bash
ledger import /tmp/trades-2025-02-01.json
# Total: 12  Inserted: 12  Duplicates: 0  Errors: 0
```

### Point at a local ledger instance for testing

```bash
LEDGER_URL=http://localhost:8080 ledger accounts list
# or permanently:
ledger config set ledger_url http://localhost:8080
```
