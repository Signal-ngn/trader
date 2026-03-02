## ADDED Requirements

### Requirement: Trade stream SSE endpoint
The system SHALL expose a `GET /api/v1/accounts/{accountId}/trades/stream` endpoint that returns a Server-Sent Events stream. The response SHALL have `Content-Type: text/event-stream` and remain open until the client disconnects. Each event SHALL be a JSON object containing the full trade payload. The endpoint SHALL be protected by the standard auth middleware.

#### Scenario: Client connects and receives trade events
- **WHEN** `GET /api/v1/accounts/paper/trades/stream` is called with a valid auth token
- **THEN** the server SHALL return HTTP 200 with `Content-Type: text/event-stream` and keep the connection open

#### Scenario: Trade recorded while client is connected
- **WHEN** a trade is recorded for the watched account while an SSE client is connected
- **THEN** the server SHALL push an SSE `data:` event containing the trade JSON to the client

#### Scenario: Trade for different account not pushed
- **WHEN** a trade is recorded for account `live` and a client is watching account `paper`
- **THEN** the server SHALL NOT push the event to that client

#### Scenario: Multiple clients watching same account
- **WHEN** two clients are connected to `trades/stream` for the same account
- **THEN** both clients SHALL receive the same trade event

#### Scenario: Client disconnects — server cleans up
- **WHEN** an SSE client disconnects
- **THEN** the server SHALL remove it from the fan-out registry and stop writing to it

#### Scenario: No active subscribers — event dropped silently
- **WHEN** a trade is recorded and no SSE clients are connected for that account
- **THEN** the server SHALL discard the event with no error

#### Scenario: Requires authentication
- **WHEN** `GET /api/v1/accounts/paper/trades/stream` is called without an `Authorization` header
- **THEN** the system SHALL return HTTP 401
