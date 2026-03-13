## REMOVED Requirements

### Requirement: NATS JetStream subscription for trade events
**Reason:** The NATS-based trade ingestion consumer (`internal/ingest`) is removed. Trade ingestion is now handled exclusively by the platform API (`POST /api/v1/trades`). The traderd binary no longer connects to NATS for ingestion purposes.
**Migration:** Trade events are submitted directly to `POST {api_url}/api/v1/trades` by the engine or by the CLI `trader import` command.

### Requirement: Trade event validation
**Reason:** Removed with the NATS consumer. Validation of incoming trades is now the responsibility of the platform API.
**Migration:** The platform API validates trade payloads on `POST /api/v1/trades`.

### Requirement: Idempotent trade ingestion
**Reason:** Removed with the NATS consumer. Idempotency is enforced by the platform API using `trade_id` as the deduplication key (ON CONFLICT DO NOTHING).
**Migration:** The platform API provides idempotent trade submission via `POST /api/v1/trades`.

### Requirement: Multi-account trade routing
**Reason:** Removed with the NATS consumer. Account routing is determined by the `account_id` field in the trade payload sent to the platform API.
**Migration:** Include `account_id` in the trade payload for `POST /api/v1/trades`.

### Requirement: Futures trade fields
**Reason:** Removed with the NATS consumer. Futures-specific fields are passed directly in the trade payload to the platform API.
**Migration:** Include futures fields (`leverage`, `margin`, `liquidation_price`, `funding_fee`) in the trade payload for `POST /api/v1/trades`.

### Requirement: Transactional trade and position update
**Reason:** Removed with the NATS consumer. The platform API handles the trade insert and position update transactionally on its side.
**Migration:** The platform API guarantees atomic trade + position update on `POST /api/v1/trades`.

### Requirement: Strategy metadata fields in trade events
**Reason:** Removed with the NATS consumer. Strategy metadata fields are included directly in the trade payload submitted to the platform API.
**Migration:** Include strategy metadata fields (`strategy`, `entry_reason`, `exit_reason`, `confidence`, `stop_loss`, `take_profit`) in the trade payload for `POST /api/v1/trades`.
