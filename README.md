# doppel

A minimal shadow server that records and replays database query results for validating shadow traffic migrations. It follows the basic design in `docs/basic_design.md` and currently focuses on the `/record` and `/replay` APIs described there.

## Getting Started

### Prerequisites
- Go 1.21+ (module tested with Go 1.24 toolchain in this environment)

### Run the tests

```bash
go test ./...
```

### Start the server

The server uses SQLite for persistence and exposes `/record`, `/replay`, and `/healthz`.

```bash
go run ./cmd/server --addr :8080 --db file:shadow.db
```

- `--addr`: TCP address for HTTP listeners (default `:8080`).
- `--db`: SQLite DSN (defaults to `file:shadow.db?_pragma=busy_timeout=5000`). Use `file:memdb1?mode=memory&cache=shared` for ephemeral runs.

### API overview
- `POST /record`: store a DB query recording. Body includes `trace_id`, `sequence`, `sql`, optional `bindings`/`result`, and `duration_ms`. Returns `201 Created` or `409 Conflict` on duplicate `(trace_id, sequence)`.
- `GET /replay?trace_id=...&sequence=...`: return a previously recorded query payload or `404` if none exists.
- `GET /healthz`: readiness probe.
