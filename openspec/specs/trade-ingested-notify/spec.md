## ADDED Requirements

### Requirement: NATS notification on trade ingestion
After a trade is successfully stored (transaction committed), the ledger service SHALL publish a notification to the NATS core subject `ledger.trades.notify.<tenantID>` where `<tenantID>` is the UUID of the tenant who owns the trade (hyphens included, lower-case). The payload SHALL be a JSON object with fields `tenant_id` (string UUID), `account_id` (string), and `trade_id` (string).

#### Scenario: New trade triggers notification
- **WHEN** a trade is successfully ingested via the JetStream consumer
- **THEN** the service SHALL publish to `ledger.trades.notify.<tenantID>` with a JSON payload containing the trade's `tenant_id`, `account_id`, and `trade_id`

#### Scenario: Duplicate trade does not trigger notification
- **WHEN** a duplicate trade is received (already exists in the database)
- **THEN** the service SHALL NOT publish a NATS notification

#### Scenario: NATS publish failure does not fail ingestion
- **WHEN** the NATS connection is unavailable at the time of publish
- **THEN** the trade IS stored successfully, the publish error is logged at warn level, and the JetStream message IS acknowledged (not NAK'd)

#### Scenario: Notification published after DB commit, not before
- **WHEN** a trade is ingested and the database transaction commits successfully
- **THEN** the NATS notification is published after the commit returns

### Requirement: NATS subject format for trade notifications
The notification subject SHALL follow the pattern `ledger.trades.notify.<tenantID>` where `<tenantID>` is the full UUID string in lowercase with hyphens (e.g. `ledger.trades.notify.550e8400-e29b-41d4-a716-446655440000`). Subscribers scoped to a tenant can subscribe to the exact subject; no wildcard subscription is needed per tenant.

#### Scenario: Subject uses full UUID with hyphens
- **WHEN** a notification is published for tenant `550e8400-e29b-41d4-a716-446655440000`
- **THEN** the NATS subject is exactly `ledger.trades.notify.550e8400-e29b-41d4-a716-446655440000`

#### Scenario: Different tenants receive separate subjects
- **WHEN** trades are ingested for two different tenants A and B
- **THEN** notifications for tenant A are published to `ledger.trades.notify.<uuidA>` and for tenant B to `ledger.trades.notify.<uuidB>`, never cross-published
