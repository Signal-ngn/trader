## ADDED Requirements

### Requirement: Exchange interface
The engine SHALL interact with exchanges exclusively through an `Exchange` interface with three methods: `OpenPosition`, `ClosePosition`, and `GetBalance`. The engine SHALL NOT contain any exchange-specific code outside of adapter implementations.

#### Scenario: Paper mode uses noop adapter
- **WHEN** `TRADING_MODE=paper`
- **THEN** the engine SHALL use a `NoopExchange` that returns a synthetic fill at signal price with zero fees

#### Scenario: Live mode uses Binance adapter
- **WHEN** `TRADING_MODE=live`
- **THEN** the engine SHALL use `BinanceFuturesExchange` which calls the Binance Futures API

### Requirement: Binance Futures adapter — open position
The Binance adapter SHALL open a futures position using a market order. It SHALL use `BINANCE_API_KEY` and `BINANCE_API_SECRET` from environment variables. The adapter SHALL return the actual fill price and quantity from the order response.

#### Scenario: Long position opened successfully
- **WHEN** `OpenPosition` is called with side=long, symbol=BTCUSDT, sizeUSD=1500, leverage=2
- **THEN** the adapter SHALL place a market buy order on Binance Futures and return the fill price and quantity

#### Scenario: Short position opened successfully
- **WHEN** `OpenPosition` is called with side=short
- **THEN** the adapter SHALL place a market sell order on Binance Futures

#### Scenario: Binance returns an error
- **WHEN** Binance returns a non-2xx response
- **THEN** the adapter SHALL return an error and the engine SHALL not record a trade

#### Scenario: Binance rate limit hit
- **WHEN** Binance returns HTTP 429
- **THEN** the adapter SHALL retry once after 1 second, then return an error if still failing

### Requirement: Binance Futures adapter — close position
The Binance adapter SHALL close an open futures position using a market order in the opposite direction. It SHALL use the full position quantity from the exchange (not the ledger) to avoid partial close mismatches.

#### Scenario: Long position closed
- **WHEN** `ClosePosition` is called for a long position
- **THEN** the adapter SHALL place a market sell order for the full open quantity on Binance Futures

#### Scenario: Short position closed
- **WHEN** `ClosePosition` is called for a short position
- **THEN** the adapter SHALL place a market buy order for the full open quantity on Binance Futures

### Requirement: Binance Futures adapter — get balance
The Binance adapter SHALL return the available USDT balance from the Binance Futures wallet.

#### Scenario: Balance fetched successfully
- **WHEN** `GetBalance` is called
- **THEN** the adapter SHALL return the available USDT balance from Binance Futures account info

#### Scenario: Missing API credentials
- **WHEN** `BINANCE_API_KEY` or `BINANCE_API_SECRET` is not set and `TRADING_MODE=live`
- **THEN** the engine SHALL log a fatal error and abort engine startup, leaving the HTTP server running
