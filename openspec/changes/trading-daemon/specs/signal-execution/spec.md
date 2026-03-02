## ADDED Requirements

### Requirement: Connect to Synadia NGS for signal subscription
The engine SHALL establish a NATS connection to Synadia NGS (`tls://connect.ngs.global`) on startup using the embedded subscribe-only credentials, overridable via `SN_NATS_CREDS_FILE` env var. The connection SHALL use exponential backoff retry (starting at 10s, doubling, capped at 5m) and reconnect automatically on disconnect.

#### Scenario: Engine starts with valid credentials
- **WHEN** `TRADING_ENABLED=true` and valid NGS credentials are available
- **THEN** the engine SHALL connect to Synadia NGS and log the connected URL

#### Scenario: NGS connection fails on startup
- **WHEN** the NGS connection fails on the first attempt
- **THEN** the engine SHALL retry with exponential backoff without affecting the HTTP server or ledger ingest consumer

#### Scenario: NGS connection drops while running
- **WHEN** the NGS connection drops after successful startup
- **THEN** the engine SHALL reconnect automatically with exponential backoff and resume signal processing

### Requirement: Subscribe to signal subject
The engine SHALL subscribe to `signals.<exchange>.<product>.<granularity>.<strategy>` using a wildcard subject (`signals.>`) and filter messages in the handler. The subject structure SHALL match exactly what the `sn signals` command uses.

#### Scenario: Signal arrives on matching subject
- **WHEN** a signal is published on `signals.coinbase.BTC-USD.ONE_HOUR.ml_xgboost`
- **THEN** the engine SHALL receive and process the message

#### Scenario: Signal subject is parsed correctly
- **WHEN** a message arrives on `signals.coinbase.BTC-USD.ONE_HOUR.ml_xgboost+trend`
- **THEN** the engine SHALL parse exchange=`coinbase`, product=`BTC-USD`, granularity=`ONE_HOUR`, strategy=`ml_xgboost+trend`

### Requirement: Build signal allowlist from trading config API
On startup and every 5 minutes, the engine SHALL fetch enabled trading configs from the SignalNGN API (`SN_API_URL/config/trading`) using `SN_API_KEY`. It SHALL build an allowlist of (exchange, product, granularity, strategy) tuples from enabled configs, expanding `strategies_long`, `strategies_short`, and `strategies_spot` for each config.

#### Scenario: Allowlist fetch succeeds
- **WHEN** the trading config API returns enabled configs
- **THEN** the engine SHALL build an allowlist and use it to filter incoming signals

#### Scenario: Allowlist fetch fails on startup
- **WHEN** `SN_API_KEY` is not set or the API call fails
- **THEN** the engine SHALL log a fatal error and abort engine startup, leaving the HTTP server running

#### Scenario: Allowlist refreshed every 5 minutes
- **WHEN** 5 minutes have elapsed since the last fetch
- **THEN** the engine SHALL re-fetch trading configs and rebuild the allowlist in the background without interrupting signal processing

### Requirement: Filter signals against allowlist with prefix matching
The engine SHALL drop signals whose (exchange, product, granularity) do not exactly match an allowlist entry. Strategy matching SHALL use prefix matching: a signal strategy of `ml_xgboost+trend` matches an allowlist entry of `ml_xgboost` (suffix separated by `_` or `+` is stripped).

#### Scenario: Exact strategy match
- **WHEN** a signal arrives with strategy `ml_xgboost` and the allowlist contains `ml_xgboost`
- **THEN** the engine SHALL allow the signal

#### Scenario: Strategy suffix match
- **WHEN** a signal arrives with strategy `ml_xgboost+trend` and the allowlist contains `ml_xgboost`
- **THEN** the engine SHALL allow the signal

#### Scenario: Signal not in allowlist
- **WHEN** a signal arrives for a product not in any enabled trading config
- **THEN** the engine SHALL silently drop the signal

### Requirement: Filter signals by optional strategy prefix flag
When `STRATEGY_FILTER` is set, the engine SHALL additionally require that the signal's strategy starts with the configured prefix. Signals not matching the prefix SHALL be dropped silently.

#### Scenario: Strategy filter matches
- **WHEN** `STRATEGY_FILTER=ml_transformer` and a signal arrives with strategy `ml_transformer+trend`
- **THEN** the engine SHALL allow the signal through to the position engine

#### Scenario: Strategy filter does not match
- **WHEN** `STRATEGY_FILTER=ml_transformer` and a signal arrives with strategy `ml_xgboost`
- **THEN** the engine SHALL drop the signal

### Requirement: Reject stale signals
The engine SHALL reject signals whose `timestamp` field is older than 2 minutes at the time of processing.

#### Scenario: Fresh signal accepted
- **WHEN** a signal arrives with a timestamp within the last 2 minutes
- **THEN** the engine SHALL process the signal

#### Scenario: Stale signal dropped
- **WHEN** a signal arrives with a timestamp older than 2 minutes
- **THEN** the engine SHALL drop the signal and log a warning with the signal age

### Requirement: Reject low-confidence entry signals
The engine SHALL reject `BUY` and `SHORT` signals with confidence below 0.5. `SELL` and `COVER` signals SHALL be processed regardless of confidence.

#### Scenario: High-confidence BUY accepted
- **WHEN** a BUY signal arrives with confidence 0.78
- **THEN** the engine SHALL route it to the position engine

#### Scenario: Low-confidence BUY rejected
- **WHEN** a BUY signal arrives with confidence 0.35
- **THEN** the engine SHALL drop the signal and log the rejection reason

#### Scenario: SELL accepted regardless of confidence
- **WHEN** a SELL signal arrives with confidence 0.2
- **THEN** the engine SHALL route it to the position engine

### Requirement: Per-product signal cooldown
The engine SHALL enforce a 5-minute cooldown per (product, action) pair. After processing a `BUY` or `SHORT` signal for a product, further signals of the same action for that product SHALL be dropped until the cooldown expires. Cooldown state is held in memory and resets on engine restart.

#### Scenario: First signal processed
- **WHEN** a BUY signal for BTC-USD arrives and no cooldown is active
- **THEN** the engine SHALL process the signal and start the 5-minute cooldown

#### Scenario: Signal within cooldown dropped
- **WHEN** a BUY signal for BTC-USD arrives within 5 minutes of the previous BUY
- **THEN** the engine SHALL drop the signal and log the remaining cooldown time

#### Scenario: Signal after cooldown processed
- **WHEN** a BUY signal for BTC-USD arrives more than 5 minutes after the previous BUY
- **THEN** the engine SHALL process the signal normally
