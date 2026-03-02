---
name: trader
description: >
  Query and manage the Signal Ngn trading ledger using the `trader` CLI.
  Use this skill when you need to record trades, check open positions,
  inspect portfolio state, stream live trade events, or query trade history.
allowed-tools: Bash
---

# trader CLI

`trader` is the command-line interface for the Signal Ngn trader service. It lets you record executed trades, query live portfolio state, stream real-time trade events, and inspect trade history — from a terminal or a trading bot script.

**Production service URL:** `https://signalngn-trader-potbdcvufa-ew.a.run.app`

---

## Installation

```bash
# Homebrew (macOS)
brew install --cask Signal-ngn/trader/trader

# Go toolchain
go install github.com/Signal-ngn/trader/cmd/trader@latest
```

---

## Authentication

The CLI reads your API key from three places, in order:

1. `TRADER_API_KEY` env var
2. `api_key` in `~/.config/trader/config.yaml`
3. `api_key` in `~/.config/sn/config.yaml` (written by `sn auth login`)

```bash
# One-time browser login via the sn CLI
sn auth login
trader accounts list   # picks up key automatically

# For bots / CI — no sn login needed
export TRADER_API_KEY=your-api-key
trader accounts list
```

The tenant ID is resolved automatically on first use (via `GET /auth/resolve`) and cached in `~/.config/trader/config.yaml`. Override with `TRADER_TENANT_ID`.

---

## Global flags

These work on every command:

| Flag | Description |
|---|---|
| `--trader-url <url>` | Override service URL for this invocation |
| `--json` | Output raw JSON instead of the default table |
| `--version` | Print CLI version |

```bash
trader --trader-url http://localhost:8080 accounts list
trader --json portfolio live
```

---

## Commands

### accounts

```bash
trader accounts list                          # list all accounts for the tenant
trader accounts list --json

trader accounts show <account-id>             # aggregate stats: trades, win rate, P&L, balance
trader accounts show live --json

trader accounts balance set <account-id> <amount>          # set USD cash balance
trader accounts balance set live 50000
trader accounts balance set live 40000 --currency EUR

trader accounts balance get <account-id>                   # query current balance
trader accounts balance get live
trader accounts balance get live --currency EUR
trader accounts balance get live --json
```

`accounts show` response fields: `total_trades`, `closed_trades`, `win_count`, `loss_count`, `win_rate`, `total_realized_pnl`, `open_positions`, `balance` (omitted when not set).

The balance is adjusted automatically by trade ingestion: buys deduct cost, sells credit realised P&L. `balance set` always overwrites — use it to set an initial value or correct after broker reconciliation.

---

### portfolio

```bash
trader portfolio <account-id>       # open positions + total realized P&L
trader portfolio live
trader portfolio paper --json
```

---

### positions

```bash
trader positions <account-id>                     # open positions (default)
trader positions live
trader positions live --status closed             # closed positions
trader positions live --status all                # all positions
trader positions live --json
```

`--status` values: `open` (default), `closed`, `all`.

Position fields: `symbol`, `side` (`long`/`short`), `market_type` (`spot`/`futures`), `quantity`, `avg_entry_price`, `cost_basis`, `realized_pnl`, `status`.

**Check if already in a position before trading:**

```bash
trader positions live --json | jq '.[] | select(.symbol == "BTC-USD")'
```

---

### trades list

`trades list` has two modes:

**Round-trip view (default):** shows one row per complete trade cycle (entry + exit), with win/loss result, P&L, and P&L%. Best for reviewing performance.

**Raw view (`--raw`):** shows individual buy/sell legs. Required when filtering by symbol, side, or date range.

```bash
# Round-trip view (default)
trader trades list live
trader trades list live --limit 20
trader trades list live --long               # adds position ID, full timestamps, exit reason
trader trades list live --json               # JSON array of position objects

# Raw individual trades
trader trades list live --raw
trader trades list live --raw --symbol BTC-USD
trader trades list live --raw --side buy
trader trades list live --raw --market-type futures
trader trades list live --raw --start 2025-01-01T00:00:00Z --end 2025-02-01T00:00:00Z
trader trades list live --raw --limit 200
trader trades list live --raw --limit 0      # all pages
trader trades list live --raw --long         # adds trade ID column, full timestamps
trader trades list live --raw --json
```

Filters (`--symbol`, `--side`, `--market-type`, `--start`, `--end`) are **only applied in `--raw` mode**. A warning is printed if you use them without `--raw`.

`--limit 0` fetches all pages. Default is 50.

---

### trades add

Record a single trade immediately after execution.

```bash
trader trades add <account-id> [flags]
```

**Required flags:** `--symbol`, `--side`, `--quantity`, `--price`

```bash
# Minimal spot buy
trader trades add live --symbol BTC-USD --side buy --quantity 0.1 --price 95000

# With fee and strategy metadata
trader trades add live \
  --symbol BTC-USD --side buy --quantity 0.1 --price 95000 \
  --fee 9.50 --strategy macd_momentum --confidence 0.78 \
  --stop-loss 93000 --take-profit 99000

# Spot sell (exit)
trader trades add live \
  --symbol BTC-USD --side sell --quantity 0.1 --price 98000 \
  --fee 9.80 --exit-reason "take-profit hit"

# Futures long with leverage
trader trades add live \
  --symbol BTC-USD --side buy --quantity 0.5 --price 95000 \
  --market-type futures --leverage 10 --margin 4750

# Explicit trade ID and timestamp
trader trades add paper \
  --trade-id "bot-20250201-042" \
  --symbol ETH-USD --side buy --quantity 1.0 --price 3200 \
  --timestamp 2025-02-01T10:30:00Z

trader trades add live --symbol BTC-USD --side buy --quantity 0.1 --price 95000 --json
```

**All flags:**

| Flag | Default | Description |
|---|---|---|
| `--trade-id` | auto UUID | Unique trade identifier — resubmitting the same ID is safe (idempotent) |
| `--symbol` | *(required)* | Trading pair, e.g. `BTC-USD` |
| `--side` | *(required)* | `buy` or `sell` |
| `--quantity` | *(required)* | Trade size |
| `--price` | *(required)* | Fill price |
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

---

### trades watch

Stream live trade events for an account to stdout as JSONL. Reconnects automatically every 5 s on disconnect. Exit with `Ctrl-C`.

```bash
trader trades watch <account-id>
trader trades watch live
trader trades watch paper | jq .       # pretty-print with jq
```

Each event is a JSON object:

```json
{
  "trade_id": "engine-live-BTC-USD-1740912345678901234",
  "account_id": "live",
  "symbol": "BTC-USD",
  "side": "sell",
  "quantity": 0.021,
  "price": 96800.0,
  "fee": 0,
  "market_type": "futures",
  "timestamp": "2026-03-02T11:00:00Z",
  "strategy": "ml_xgboost",
  "confidence": 0.82,
  "stop_loss": 93200.0,
  "take_profit": 104000.0,
  "exit_reason": "take profit"
}
```

Optional fields (`strategy`, `confidence`, `stop_loss`, `take_profit`, `entry_reason`, `exit_reason`) are omitted when not set.

---

### trades delete

```bash
trader trades delete <trade-id> --confirm
trader trades delete abc-123 --confirm --json
```

Fails if the trade contributes to an open position. The `--confirm` flag is required.

---

### orders

```bash
trader orders <account-id>
trader orders live                           # 50 most recent orders
trader orders live --status open             # open orders only
trader orders live --status filled
trader orders live --symbol BTC-USD
trader orders live --limit 0 --json          # all orders as JSON
```

`--status` values: `open`, `filled`, `partially_filled`, `cancelled`.

Order fields: `order_id`, `symbol`, `side`, `order_type` (`market`/`limit`), `requested_qty`, `filled_qty`, `avg_fill_price`, `status`, `market_type`, `created_at`.

---

### import

Bulk-load historic trades from a JSON file. Validates all trades up front, inserts idempotently (duplicate IDs are skipped), rebuilds positions.

```bash
trader import trades.json
trader import trades.json --json    # full response JSON
```

Prints `Total: N  Inserted: N  Duplicates: N  Errors: N`. Exits non-zero if any errors occurred. Safe to re-run.

**File format** — JSON object with a `"trades"` array:

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
      "timestamp": "2025-01-01T10:00:00Z"
    }
  ]
}
```

Required fields: `tenant_id`, `trade_id`, `account_id`, `symbol`, `side`, `quantity`, `price`, `fee`, `fee_currency`, `market_type`, `timestamp`. Max 1000 trades per request.

---

### config

```bash
trader config show                        # all resolved values and their sources
trader config set trader_url https://...  # write to ~/.config/trader/config.yaml
trader config set api_key sk-...
trader config get trader_url
```

Config file: `~/.config/trader/config.yaml`

| Key | Default | Env override |
|---|---|---|
| `trader_url` | `https://signalngn-trader-potbdcvufa-ew.a.run.app` | `TRADER_URL` |
| `api_key` | *(from `~/.config/sn/config.yaml`)* | `TRADER_API_KEY` |
| `tenant_id` | *(resolved via `/auth/resolve` on first use)* | `TRADER_TENANT_ID` |

---

## Common bot patterns

### Check available balance before sizing a position

```bash
BALANCE=$(trader accounts balance get live --json | jq '.amount')
```

### Check current exposure before entering

```bash
# Is BTC-USD already open?
trader positions live --json | jq '.[] | select(.symbol == "BTC-USD" and .status == "open")'

# Total open position count
trader positions live --json | jq 'length'
```

### Record a trade immediately after execution

```bash
trader trades add live \
  --trade-id "$EXCHANGE_ORDER_ID" \
  --symbol BTC-USD --side buy --quantity 0.1 --price 95000 \
  --fee 9.50 --strategy macd_momentum --confidence 0.78 \
  --stop-loss 93000 --take-profit 99000
```

### Get realised P&L since midnight

```bash
TODAY=$(date -u +%Y-%m-%dT00:00:00Z)
trader trades list live --raw --start "$TODAY" --json | \
  jq '[.[] | select(.side == "sell")] | map(.realized_pnl) | add // 0'
```

### Verify a trade was recorded

```bash
trader trades list live --raw --json | jq '.[] | select(.trade_id == "my-order-id")'
```

### Point at a local instance for testing

```bash
TRADER_URL=http://localhost:8080 trader accounts list
# or permanently:
trader config set trader_url http://localhost:8080
```
