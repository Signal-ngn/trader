## ADDED Requirements

### Requirement: Exit price on closed positions
The system SHALL store the explicit exit price on a position when it is fully closed. The exit price SHALL be set to the trade price of the closing trade.

#### Scenario: Spot position fully closed
- **WHEN** a spot sell trade fully closes an open position at price 55000
- **THEN** the system SHALL set the position's exit_price to 55000

#### Scenario: Futures position fully closed
- **WHEN** a futures trade fully offsets an open position at price 42000
- **THEN** the system SHALL set the position's exit_price to 42000

#### Scenario: Partial close does not set exit price
- **WHEN** a sell trade partially reduces an open position
- **THEN** the system SHALL NOT set exit_price on the position (it remains NULL)

### Requirement: Exit reason on closed positions
The system SHALL store the exit reason on a position when it is fully closed. The exit reason SHALL be copied from the closing trade's exit_reason field.

#### Scenario: Position closed with exit reason
- **WHEN** a closing trade has exit_reason "take profit reached"
- **THEN** the system SHALL set the position's exit_reason to "take profit reached"

#### Scenario: Position closed without exit reason
- **WHEN** a closing trade has no exit_reason (NULL)
- **THEN** the system SHALL leave the position's exit_reason as NULL

### Requirement: Stop loss and take profit on positions
The system SHALL store stop_loss and take_profit prices on positions. These fields SHALL be set from the opening trade and updated by subsequent trades that increase the position, using the new trade's values when provided.

#### Scenario: Position opened with stop loss and take profit
- **WHEN** a buy trade opens a new position with stop_loss 48000 and take_profit 55000
- **THEN** the system SHALL set the position's stop_loss to 48000 and take_profit to 55000

#### Scenario: Position opened without stop loss and take profit
- **WHEN** a buy trade opens a new position without stop_loss or take_profit
- **THEN** the system SHALL leave the position's stop_loss and take_profit as NULL

#### Scenario: Position increased with updated stop loss
- **WHEN** a subsequent buy trade increases an existing position and includes stop_loss 49000 but no take_profit
- **THEN** the system SHALL update stop_loss to 49000 and keep the existing take_profit unchanged

#### Scenario: Position increased without stop loss or take profit
- **WHEN** a subsequent buy trade increases an existing position without stop_loss or take_profit
- **THEN** the system SHALL keep the existing stop_loss and take_profit unchanged

### Requirement: Confidence score on positions
The system SHALL store the confidence score from the opening trade on the position. The confidence SHALL be set when the position is created and SHALL NOT be updated by subsequent trades.

#### Scenario: Position opened with confidence
- **WHEN** a buy trade opens a new position with confidence 0.85
- **THEN** the system SHALL set the position's confidence to 0.85

#### Scenario: Position opened without confidence
- **WHEN** a buy trade opens a new position without confidence
- **THEN** the system SHALL leave the position's confidence as NULL

#### Scenario: Subsequent trade does not change confidence
- **WHEN** a buy trade with confidence 0.92 increases a position that was opened with confidence 0.85
- **THEN** the position's confidence SHALL remain 0.85

### Requirement: Position rebuild preserves metadata
The system SHALL reconstruct position metadata (exit_price, exit_reason, stop_loss, take_profit, confidence) during position rebuild from trade history. The rebuilt positions MUST have the same metadata values as positions created through incremental trade processing.

#### Scenario: Rebuild closed position with metadata
- **WHEN** positions are rebuilt for an account that has a closed position with exit_price, exit_reason, stop_loss, take_profit, and confidence
- **THEN** the rebuilt position SHALL have the same metadata values as before the rebuild
