# Architecture

A modular Go monolith uses standard-library contexts, HTTP, JSON and structured
logging. The only direct runtime dependency is the pure-Go `modernc.org/sqlite`
driver (with its transitive modules). No frontend framework is needed.

```text
environment -> config -> cmd/monitor
                          |-> provider (mock / gated Cudy)
                          |       ^
                          |   collector (one poll at a time)
                          |       |
                          |       v
                          |   storage -> accounting (pure delta calculation)
                          |       |
                          |       v
                          |     SQLite
                          |       ^
                          +-> HTTP API (queries and safe collector status)
```

- `internal/router/provider`: read-only operations, capabilities, typed safe
  errors, coherent snapshots and explicit metadata. Boot ID and counter epoch
  are continuity evidence, not display strings. Devices have stable provider IDs.
- `internal/router/mock`: deterministic fixture reader and simulated event-cycle
  demo. Includes auth expiry, disconnect, disappearance, missing values, reset,
  reboot and rollover. Values never represent a real client or WAN measurement.
- `internal/router/cudy`: validation gate, no network transport. Unsupported
  capabilities differ from failed collections via false flags and typed errors.
- `internal/collector`: immediate first poll then a configurable delay after
  completion. Each attempt (connect/recover plus read) has a timeout. At most
  `MAX_RETRIES + 1` attempts; exponential waits of 250ms then 500ms by default,
  capped at 2s. Only transport/timeouts/auth errors retry. No overlap, catch-up
  burst, unbounded retry or goroutine per client. Errors are logged by safe code.
- `internal/accounting`: pure per-direction delta rules and uncertainty reasons.
- `internal/storage`: embedded versioned SQL migration, device inventory, raw
  observations, usage intervals, router state, checkpoints, safe settings and
  failed-collection events. Samples, deltas and checkpoints commit atomically.
- `internal/api`: GET-only JSON envelope; no credentials or arbitrary RPC access.
- `cmd/monitor`: wiring, signal cancellation, bounded HTTP shutdown and binary
  healthcheck. SQLite closes only after collection stops.

Devices belong to a router; this MVP runs one provider per process. Multiple
processes must not collect into the same DB. Run one container/binary per DB.
SQLite uses WAL, foreign keys, a busy timeout, full synchronous commits and one
connection. Transactions protect restart state, not concurrent collectors.
Migration version is in `PRAGMA user_version`; newer schemas fail closed.

The database uses integer UTC nanoseconds for timestamps. Raw uint64 counters
and interval deltas use decimal TEXT to preserve the full range; missing values
use NULL. Summary arithmetic checks overflow. Summaries group by router, device,
counter and scope. Application settings persist only provider mode, poll interval
and timezone. Environment configuration is authoritative on startup; passwords,
session data, router address and authentication bodies never enter the schema.

New database directories/files request modes 0700/0600 on Unix. Protect existing
directories and WAL/SHM files with filesystem ownership and host access controls.
Windows permissions follow the directory ACL; POSIX modes are not an ACL policy.

There are no empty devices/web packages: inventory belongs to storage, and this
phase exposes an API without a dashboard. Split those packages when needed.
