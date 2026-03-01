## ADDED Requirements

### Requirement: Cost basis tracking per trade
The system SHALL record the cost basis for every trade. For buy trades, the cost basis SHALL be quantity × price + fees. For sell trades, the cost basis SHALL reference the average entry price at the time of the sell.

#### Scenario: Buy trade cost basis
- **WHEN** a spot buy trade of 0.5 BTC at price 50000 with fee 25 is ingested
- **THEN** the system SHALL record cost basis as (0.5 × 50000) + 25 = 25025

#### Scenario: Sell trade cost basis reference
- **WHEN** a spot sell trade is ingested and the current average entry price is 48000
- **THEN** the system SHALL record the cost basis per unit as 48000 for P&L calculation

### Requirement: Realized gains and losses per trade
The system SHALL calculate and store the realized gain or loss for every sell trade. Realized P&L SHALL be (sell price − average entry price) × quantity − sell fees.

#### Scenario: Profitable sell
- **WHEN** a spot sell of 0.5 BTC at price 55000 (avg entry 50000, fee 27.50) is ingested
- **THEN** the realized P&L SHALL be (55000 − 50000) × 0.5 − 27.50 = 2472.50

#### Scenario: Loss-making sell
- **WHEN** a spot sell of 1.0 ETH at price 3000 (avg entry 3500, fee 3.00) is ingested
- **THEN** the realized P&L SHALL be (3000 − 3500) × 1.0 − 3.00 = −503.00

### Requirement: Fee tracking
The system SHALL store the fee amount and fee currency for every trade. Fees paid in the traded asset (e.g., BTC) SHALL be stored separately from fees paid in the quote currency (e.g., USD).

#### Scenario: Fee in quote currency
- **WHEN** a trade has a fee of 25.00 USD
- **THEN** the system SHALL store fee amount 25.00 and fee currency "USD"

#### Scenario: Fee in base currency
- **WHEN** a trade has a fee of 0.0001 BTC
- **THEN** the system SHALL store fee amount 0.0001 and fee currency "BTC"

### Requirement: Holding period tracking
The system SHALL store timestamps with sufficient precision to calculate holding periods for each position. The system SHALL track when a position was opened (first buy) and when it was closed (final sell), enabling calculation of short-term vs long-term holding periods.

#### Scenario: Position opened and closed
- **WHEN** a position is opened by a buy on 2025-01-15T10:00:00Z and closed by a sell on 2025-07-20T14:30:00Z
- **THEN** the system SHALL have both timestamps stored, enabling holding period calculation of approximately 186 days

#### Scenario: Position still open
- **WHEN** a position was opened by a buy on 2025-03-01T08:00:00Z and has not been closed
- **THEN** the system SHALL have the open timestamp stored with no close timestamp, indicating the position is still held

### Requirement: Trade currency pair preservation
The system SHALL store the full symbol/pair for each trade (e.g., "BTC-USD", "ETH-EUR") to support multi-currency tax calculations where the reporting currency may differ from the trading quote currency.

#### Scenario: Trade in USD pair
- **WHEN** a trade for BTC-USD is ingested
- **THEN** the system SHALL store symbol "BTC-USD", preserving both base and quote currency information

#### Scenario: Trade in EUR pair
- **WHEN** a trade for ETH-EUR is ingested
- **THEN** the system SHALL store symbol "ETH-EUR", enabling future EUR-denominated tax reporting

### Requirement: Futures P&L data for tax purposes
The system SHALL store sufficient data for futures trades to calculate taxable gains: entry price, exit price, leverage, direction (long/short), margin, and funding fees. Futures gains SHALL be tracked separately from spot gains.

#### Scenario: Leveraged futures long closed at profit
- **WHEN** a 10x leveraged long futures position on BTC is opened at 50000 and closed at 52000
- **THEN** the system SHALL store both trades with leverage 10, enabling P&L calculation of (52000 − 50000) × quantity × leverage

#### Scenario: Futures funding fees
- **WHEN** a futures trade includes a funding fee of 12.50
- **THEN** the system SHALL store the funding fee amount, which is deductible for tax purposes
