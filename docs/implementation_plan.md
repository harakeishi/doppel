# Implementation Plan

This plan scopes the first slice of the shadowing platform based on the basic design. The goal is to ship a minimal Shadow Server that can record and replay DB queries for traceable HTTP requests.

## Scope
- Provide an HTTP server (Go) exposing `POST /record` and `GET /replay`.
- Persist DB query recordings in SQLite with a unique `(trace_id, sequence)` constraint.
- Return recorded results to the replay endpoint; respond with HTTP 409 on duplicate recordings and 404 when replay data is missing.
- Keep the surface small so we can iterate toward proxy and diffing later.

## Data Model
- Table `db_queries`
  - `trace_id` (TEXT, not null)
  - `sequence` (INTEGER, not null)
  - `sql` (TEXT)
  - `bindings` (JSON text)
  - `result` (JSON text)
  - `duration_ms` (INTEGER)
  - Unique index on `(trace_id, sequence)`

## Application Components
- **Storage layer**: wraps `database/sql` with SQLite driver. Handles migrations and CRUD operations for `db_queries`.
- **HTTP handlers**: 
  - `/record`: validate payload, insert row, return 201 on success, 409 on unique conflict, 400 on validation errors.
  - `/replay`: query by `trace_id` & `sequence`; return 200 + JSON body, or 404 if not found.
- **Server wiring**: router, health check, graceful shutdown friendly (simple `http.Server`).

## Testing Strategy (TDD)
1. Start with handler tests using `httptest` against an in-memory SQLite database.
2. Drive schema creation through tests ensuring unique constraints and validation behavior.
3. Add replay tests to ensure correct data retrieval and 404 handling.
4. Add health check test for server readiness endpoint.

## Acceptance Criteria
- All handler behaviors above are covered by tests.
- `go test ./...` passes.
- README updated with quickstart for the server.
