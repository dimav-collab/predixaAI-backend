# Dev Changelog

## 2026-02-19 — POST /simulate/write v2: extraColumns + structured errors (db-connector)

### What changed
- `WriteRow` interface signature extended: added `extraColumns map[string]any` parameter.
- All three connector implementations (`mysql`, `postgres`, `mssql`) updated to dynamically build the INSERT column/placeholder list from `extraColumns`.
- `simulateWriteRequest` gains `ExtraColumns map[string]any` field.
- Identifier regex corrected to `^[a-zA-Z_][a-zA-Z0-9_]{0,127}$` (no `$`, matching spec).
- All error responses now use typed JSON: `{ "ok": false, "error": "<code>", "details": [...] }`.
- Error codes: `validation_error`, `connection_not_found`, `connection_not_configured`, `db_unreachable`, `db_write_failed`.
- `decodeJSONLenient` helper added to `utils.go` for endpoints where unknown fields should not error.
- `isErr` helper added to `utils.go`.
- Resolver errors that are not `ErrNotConfigured` all map to `404 connection_not_found`.

### Env vars required
No new env vars.

### How to test locally
```bash
# Validation error
curl -X POST http://localhost:8085/simulate/write \
  -H 'Content-Type: application/json' \
  -d '{"connectionRef":"","tableName":"t","columnName":"c","timestampColumn":"ts","value":1}'
# → {"ok":false,"error":"validation_error","details":["connectionRef: required"]}

# Unknown connection → 404
curl -X POST http://localhost:8085/simulate/write \
  -H 'Content-Type: application/json' \
  -d '{"connectionRef":"nonexistent","tableName":"etchers_data","columnName":"rf_power","timestampColumn":"run_order","value":30.0,"extraColumns":{"unit":"SIM"}}'
# → {"ok":false,"error":"connection_not_found"}

# Happy path (requires valid connectionRef)
curl -X POST http://localhost:8085/simulate/write \
  -H 'Content-Type: application/json' \
  -d '{"connectionRef":"<your-ref>","tableName":"etchers_data","columnName":"rf_power","timestampColumn":"run_order","value":30.0,"extraColumns":{"unit":"SIM"}}'
# → {"ok":true,"rowsAffected":1}
```

### Tests added / updated
- `TestValidateSimulateWriteRequest` — 10 table-driven cases incl. extraColumns key validation and multi-error
- `TestHandleSimulateWrite` — 10 table-driven cases incl. new error codes
- `TestHandleSimulateWritePassesExtraColumns` — verifies extras propagate to connector

### Migrations
None required.

---



### What changed
- Added `WriteRow(ctx, table, valueColumn, timestampColumn string, value any) (int64, error)` to the `DbConnector` interface (`connector.go`).
- Implemented `WriteRow` on `MySQLConnector` (backtick quoting, `NOW()`), `PostgresConnector` (double-quote quoting, `NOW()`), and `MSSQLConnector` (bracket quoting, `GETDATE()`).
- New handler file `cmd/service/simulate_write.go`:
  - `POST /simulate/write`
  - Request: `{ connectionRef, tableName, columnName, timestampColumn, value }`
  - Validates all identifier fields against `^[a-zA-Z_][a-zA-Z0-9_$]{0,127}$`.
  - Reuses existing `Resolver` / connection registry (same lookup as other endpoints).
  - 5 s context timeout on DB operations.
  - Returns `{ "ok": true, "rowsAffected": 1 }` on success.
  - Structured logging (never logs credentials).
- Route registered in `cmd/service/main.go`.

### Env vars required
No new env vars. Requires `DATABASE_URL` and `ENCRYPTION_KEY` already in use by the service.

### How to test locally
```bash
# Confirm a valid connectionRef from your db_connections table, then:
curl -X POST http://localhost:8085/simulate/write \
  -H 'Content-Type: application/json' \
  -d '{
    "connectionRef": "<your-connection-ref>",
    "tableName": "etchers_data",
    "columnName": "rf_power",
    "timestampColumn": "run_order",
    "value": 99.5
  }'
# Expected: {"ok":true,"rowsAffected":1}
```

### Tests added / updated
- `cmd/service/handlers_test.go` — added `WriteRow` stub to `mockConnector`.
- `cmd/service/simulate_write_test.go` — new file with:
  - `TestValidateSimulateWriteRequest` (8 table-driven cases): empty connectionRef, SQL injection in identifiers, digit-prefixed column, zero value, etc.
  - `TestHandleSimulateWrite` (8 table-driven cases): happy path, wrong method, missing fields, connection not found, resolver not configured, target DB unreachable, write error.

### Migrations
None required. No new tables.

---

## 2026-02-23 — Fix: stepper rules now sync to scheduler (rule-service)

### What changed
**Root cause of no alerts**: When a stepper rule (TREND_6_POINTS, SHEWHART, etc.)
was created via `POST /api/rules`, it was saved to `ui_rules` only. The scheduler
reads exclusively from the `rules` table, so no evaluation ever occurred.

**Fix implemented:**
- `services/rule-service/internal/api/stepper_sync.go` — new file:
  - `BuildRuleSpecFromStepper(rule, unit)` converts a `ui_rules` + `machine_units`
    record into a scheduler-compatible `RuleSpec` JSON for all supported rule types:
    `TREND_6_POINTS`, `SHEWHART_3SIGMA`, `SHEWHART_2SIGMA`, `RANGE_CHART_R`,
    `SPEC_LIMIT_VIOLATION`, `TPA`.
- `services/rule-service/internal/storage/repository.go` — added:
  - `UpsertStepperRuleToRules(ctx, RuleRecord)` — INSERT…ON CONFLICT UPDATE into `rules`.
  - `DeleteRule(ctx, id)` — DELETE from `rules` by ID.
- `services/rule-service/internal/api/stepper_rules.go` — wired sync into all CRUD:
  - `handleStepperRuleCreate` → syncs + publishes `rule.created`
  - `handleStepperRuleUpdate` → syncs + publishes `rule.updated`
  - `handleStepperRuleEnable` → syncs + publishes `rule.enabled`
  - `handleStepperRuleDisable` → syncs + publishes `rule.disabled`
  - `handleStepperRuleDelete` → deletes from `rules` + publishes `rule.deleted`

### Env vars required
No new env vars.

### How to test locally
```bash
# 1. Create or re-enable a TREND_6_POINTS rule via the UI (POST /api/rules)
# 2. The rule appears in the `rules` table with status ACTIVE
docker compose exec postgres psql -U postgres -d rules -c \
  "SELECT id, status FROM rules WHERE id='<rule_id>';"

# 3. Check the scheduler has it scheduled
curl http://localhost:8091/jobs

# 4. Insert 6 consecutive increasing rows (step > epsilon) into the source table
# 5. Wait for the poll interval (default 60s), then check alerts:
docker compose exec postgres psql -U postgres -d rules -c \
  "SELECT rule_id, observed_value, detector_type, hit FROM alerts ORDER BY ts_utc DESC LIMIT 5;"
```

### Tests added
- `services/rule-service/internal/api/stepper_sync_test.go` — 16 tests:
  - Happy path: all 5 rule types with full config
  - Defaults: TREND_6_POINTS with empty config uses windowSize=6, epsilon=0
  - Error paths: missing rule ID, missing connection, missing table, missing
    timestamp column, invalid parameterID format, unsupported rule type, invalid config JSON
  - Edge cases: `pollIntervalSeconds` override from config, disabled rule propagates `enabled=false`

### Migrations
None required. Uses existing `rules` and `ui_rules` tables.

### Verified
- Alert ID=1 created for rule `2cb5885c` (TREND_6_POINTS on `etchers_data.rf_power`):
  `observed_value=70`, `direction=up`, `detector_type=trend`, `hit=true`

---


### What changed
- Applied migration `008_create_ui_rules.sql` manually to fix broken `POST /api/rules`.
- Built embedded migration runner:
  - `services/rule-service/embed.go` — embeds `migrations/` directory into binary.
  - `services/rule-service/internal/storage/migrations.go` — `Store.RunMigrations(ctx)`.
  - `services/rule-service/cmd/server/main.go` — calls `RunMigrations` before serving.
  - `services/rule-service/Dockerfile` — build context now repo root so `migrations/` can be copied in.
  - `docker-compose.yml` — rule-service build context updated to `.`.
- Added `migrations/010_create_simulation_jobs.sql`.

### Env vars required
No new env vars.

### Tests added
- `services/rule-service/internal/storage/migrations_test.go` — 4 tests covering FS non-empty, files sorted, all readable, idempotent re-run.
