## ADDED Requirements

### Requirement: Strategy metadata fields in trade events
The system SHALL accept optional strategy metadata fields in trade events: `strategy` (string), `entry_reason` (string), `exit_reason` (string), `confidence` (float64, 0–1), `stop_loss` (float64), and `take_profit` (float64). All fields SHALL be nullable and omitting them SHALL NOT affect validation.

#### Scenario: Trade event with all metadata fields
- **WHEN** a trade event includes strategy, entry_reason, confidence, stop_loss, and take_profit fields
- **THEN** the system SHALL persist all metadata fields alongside the base trade fields

#### Scenario: Trade event without metadata fields
- **WHEN** a trade event is received without any of the new metadata fields
- **THEN** the system SHALL persist the trade with all metadata fields set to NULL

#### Scenario: Trade event with partial metadata
- **WHEN** a trade event includes strategy and confidence but omits entry_reason, stop_loss, and take_profit
- **THEN** the system SHALL persist the provided fields and set omitted fields to NULL

#### Scenario: Exit trade with exit_reason
- **WHEN** a sell trade event includes an exit_reason of "stop loss hit"
- **THEN** the system SHALL persist the exit_reason on the trade record

#### Scenario: Confidence value validation
- **WHEN** a trade event includes a confidence value
- **THEN** the system SHALL accept any float64 value (validation of 0–1 range is the publisher's responsibility)
