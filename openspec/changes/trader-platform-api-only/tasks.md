## 1. Dependencies & Config

- [x] 1.1 Add GCP Firestore client library to `go.mod` / `go.sum` (`cloud.google.com/go/firestore`)
- [x] 1.2 Add `FirestoreProjectID` field to `internal/config/config.go`, read from `FIRESTORE_PROJECT_ID` env var; fail fast in engine startup if absent when `TRADING_ENABLED=true`
- [x] 1.3 Add `TRADER_API_URL` env var to `config.go` (default: `https://signalngn-api-potbdcvufa-ew.a.run.app`); rename existing `SNAPIURL` field to `TraderAPIURL` for consistency

## 2. Move PlatformClient to internal/platform

- [x] 2.1 Create `internal/platform/` package; move `PlatformClient` struct and `platformDo`, `apiURL`, `ingestionURL`, helper methods from `cmd/trader/platform_client.go`
- [x] 2.2 Update all `cmd/trader/` files that reference `PlatformClient` to import from `internal/platform`
- [x] 2.3 Verify `cmd/trader` CLI builds and existing platform commands work after the move

## 3. Platform Client — New Engine Methods

- [x] 3.1 Add `ResolveAuth(ctx) (tenantID string, err error)` — calls `GET /auth/resolve`, returns `tenant_id`
- [x] 3.2 Add `ListAccounts(ctx) ([]Account, error)` — calls `GET /api/v1/accounts`, returns accounts with `balance` field
- [x] 3.3 Add `GetPortfolio(ctx, accountID string) (*Portfolio, error)` — calls `GET /api/v1/accounts/{id}/portfolio`, returns open positions with `symbol`, `market_type`, `side`, `quantity`, `avg_entry_price`, `stop_loss`, `take_profit`, `leverage`, `opened_at`
- [x] 3.4 Add `SubmitTrade(ctx, trade) error` — calls `POST /api/v1/trades`; returns nil on 2xx or 409, error otherwise
- [x] 3.5 Add `SetBalance(ctx, accountID string, balance float64) error` — calls `PUT /api/v1/accounts/{id}/balance` with `{"balance": <amount>}`
- [x] 3.6 Add `Account` and `Portfolio` / `PortfolioPosition` structs in `internal/platform/` to represent API responses

## 4. EngineStore Interface

- [x] 4.1 Define `EngineStore` interface in `internal/engine/store.go` with all methods the engine calls: `InsertTradeAndUpdatePosition`, `GetAccountBalance`, `AdjustBalance`, `GetAvgEntryPrice`, `CountOpenPositionStates`, `ListOpenPositionsForAccount`, `ListAccounts`, `LoadPositionStates`, `InsertPositionState`, `UpdatePositionState`, `DeletePositionState`, `DailyRealizedPnL`
- [x] 4.2 Update `Engine` struct in `engine.go` to hold `repo EngineStore` instead of `repo *store.Repository`
- [x] 4.3 Update `engine.New(...)` constructor signature to accept `EngineStore` instead of `*store.Repository`
- [x] 4.4 Remove `store.NewUserRepository` / `userRepo.GetByAPIKey` tenant resolution from `engine.go`; replace with a call to `platform.ResolveAuth` at startup

## 5. APIEngineStore — Firestore Risk State

- [x] 5.1 Create `internal/engine/apistore.go` with `APIEngineStore` struct holding a `*platform.PlatformClient` and a `*firestore.Client`
- [x] 5.2 Implement `InsertPositionState` — writes Firestore document at `engine-state/{accountID}/positions/{symbol}-{marketType}` with all risk fields
- [x] 5.3 Implement `UpdatePositionState` — updates trailing stop, peak price, stop loss, take profit fields on the existing Firestore document
- [x] 5.4 Implement `DeletePositionState` — deletes the Firestore document for the closed position
- [x] 5.5 Implement `LoadPositionStates` — queries the `positions` sub-collection for the account and returns all `EnginePositionState` entries
- [x] 5.6 Implement `CountOpenPositionStates` — returns the count of documents in the `positions` sub-collection for the account

## 6. APIEngineStore — Daily P&L

- [x] 6.1 Implement `DailyRealizedPnL(ctx, accountID)` — reads today's Firestore document at `engine-state/{accountID}/daily-pnl/{accountID}-{YYYY-MM-DD}`; returns 0 if document does not exist
- [x] 6.2 Implement internal `incrementDailyPnL(ctx, accountID, delta float64)` — atomically increments the daily P&L Firestore document using `firestore.Increment`; called from `InsertTradeAndUpdatePosition` after a successful trade close

## 7. APIEngineStore — Platform API Methods

- [x] 7.1 Implement `InsertTradeAndUpdatePosition` — calls `platform.SubmitTrade`; on success calls `incrementDailyPnL` if the trade is a close (realised P&L non-zero), then calls `AdjustBalance`; returns `(true, nil)` on 2xx, `(false, nil)` on 409, `(false, err)` otherwise
- [x] 7.2 Implement `GetAccountBalance` — reads balance from `platform.ListAccounts`, finds the matching account by ID, returns its balance (nil if account not found or balance not set)
- [x] 7.3 Implement `AdjustBalance` — reads current balance via `GetAccountBalance`, computes new balance by applying delta, calls `platform.SetBalance`; if no balance exists seeds from `PORTFOLIO_SIZE_USD`
- [x] 7.4 Implement `GetAvgEntryPrice` — calls `platform.GetPortfolio`, finds matching open position by symbol and market type, returns `avg_entry_price`; returns 0 if not found
- [x] 7.5 Implement `ListOpenPositionsForAccount` — calls `platform.GetPortfolio`, maps `PortfolioPosition` to `domain.Position`
- [x] 7.6 Implement `ListAccounts` — calls `platform.ListAccounts`, maps `platform.Account` to `domain.Account`

## 8. Wire APIEngineStore in main

- [x] 8.1 In `cmd/traderd/main.go`, construct `platform.PlatformClient` using `SN_API_KEY` and `TRADER_API_URL` when `TRADING_ENABLED=true`
- [x] 8.2 Construct `firestore.Client` using `FIRESTORE_PROJECT_ID` and ADC when `TRADING_ENABLED=true`
- [x] 8.3 Construct `engine.APIEngineStore` with the platform client and Firestore client; pass it to `engine.New` instead of `repo`
- [x] 8.4 Ensure `store.Repository` (DB) is still constructed and passed to `api.NewServer` and `ingest.NewConsumer` — the DB dependency remains for the REST API and NATS consumer

## 9. Remove DB from Engine Startup

- [x] 9.1 Remove `store.NewUserRepository` / `userRepo.GetByAPIKey` call from engine startup in `engine.go`; tenant ID now comes from `APIEngineStore.ResolveAuth` called during `engine.Start`
- [x] 9.2 Remove `repo.ListAccounts` call from engine startup; accounts now loaded via `APIEngineStore.ListAccounts`
- [x] 9.3 Update `isDailyLossLimitReached` comment to reflect that P&L is now from Firestore, not DB
- [x] 9.4 Confirm engine package has no remaining import of `internal/store`

## 10. Update Install Script & Docs

- [x] 10.1 Remove `DATABASE_URL` / Cloud SQL env var from the `gcloud run deploy` command in `scripts/install-tenant.sh`
- [x] 10.2 Update `scripts/deploy-tenant.sh` similarly — remove any DB-related env vars
- [x] 10.3 Update `docs/tenant-install.md` to remove any mention of `DATABASE_URL` and reflect that the trader has no database dependency

## 11. Tests

- [x] 11.1 Add unit tests for `APIEngineStore.InsertTradeAndUpdatePosition` using a mock `PlatformClient` — covers success, 409 idempotency, and error paths
- [x] 11.2 Add unit tests for `APIEngineStore.AdjustBalance` — covers open, close, and seed-from-portfolio-size paths
- [x] 11.3 Add unit tests for Firestore position state round-trip: insert → load → update → delete
- [x] 11.4 Add unit tests for `DailyRealizedPnL` and `incrementDailyPnL` using the Firestore emulator
