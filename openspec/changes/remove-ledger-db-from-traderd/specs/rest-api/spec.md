## MODIFIED Requirements

### Requirement: Health check endpoint
The system SHALL expose a `GET /health` endpoint that returns HTTP 200 unconditionally. The traderd binary has no database or NATS dependency; the health check does not probe any external service.

#### Scenario: Health check always returns OK
- **WHEN** `GET /health` is called while traderd is running
- **THEN** the system SHALL return HTTP 200 with body `{"status": "ok"}`

## REMOVED Requirements

### Requirement: List accounts endpoint
**Reason:** Accounts are now served directly by the platform API. The CLI calls `GET {api_url}/api/v1/accounts` instead. traderd has no DB to read from.
**Migration:** Use `GET {api_url}/api/v1/accounts` with `Authorization: Bearer {api_key}`.

### Requirement: Set account balance endpoint
**Reason:** Balance writes go directly to the platform API. traderd has no DB to write to.
**Migration:** Use `PUT {api_url}/api/v1/accounts/{id}/balance` with `Authorization: Bearer {api_key}`.

### Requirement: Get account balance endpoint
**Reason:** Balance reads come directly from the platform API. traderd has no DB to read from.
**Migration:** Use `GET {api_url}/api/v1/accounts/{id}/balance` with `Authorization: Bearer {api_key}`.

### Requirement: Portfolio summary endpoint
**Reason:** Portfolio data is served directly by the platform API. traderd has no DB to read from.
**Migration:** Use `GET {api_url}/api/v1/accounts/{id}/portfolio` with `Authorization: Bearer {api_key}`.

### Requirement: List positions endpoint
**Reason:** Position data is served directly by the platform API. traderd has no DB to read from.
**Migration:** Use `GET {api_url}/api/v1/accounts/{id}/positions` with `Authorization: Bearer {api_key}`.

### Requirement: List trades endpoint
**Reason:** Trade data is served directly by the platform API. traderd has no DB to read from.
**Migration:** Use `GET {api_url}/api/v1/accounts/{id}/trades` with `Authorization: Bearer {api_key}`.

### Requirement: List orders endpoint
**Reason:** Order data is served directly by the platform API. traderd has no DB to read from.
**Migration:** Use `GET {api_url}/api/v1/accounts/{id}/orders` with `Authorization: Bearer {api_key}`.

### Requirement: Read-only API
**Reason:** Superseded. The remaining traderd API (health, auth-resolve, SSE stream) does not expose any data mutation or query endpoints. The read-only constraint was specific to the now-removed DB-backed handlers.
**Migration:** No migration needed; data endpoints are removed not replaced.
