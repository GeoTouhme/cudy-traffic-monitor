# Cudy Traffic Monitor

A lightweight, privacy-first, self-hosted foundation for per-device bandwidth
accounting on Cudy stock firmware. Go, SQLite, no cloud service, packet inspection
or custom router firmware. Licensed under the existing [Apache 2.0 license](LICENSE).

**Phase 0/1 status: working mock-backed core; live Cudy collection is gated.**
No Cudy model is yet supported for real traffic collection. The initial research
target is **WR1200 V2.1 on stock firmware**, but its API, counter direction and
WAN-only semantics require physical-device validation. Selecting `cudy` mode
reports `unvalidated_firmware` and false capabilities without contacting a router.

## Implemented features

- Read-only provider interface, capability reporting and sanitized typed errors.
- Deterministic lifecycle fixture and a running simulated router demo, covering
  increments, disconnects, session expiry, resets, reboot, disappearance,
  reappearance, missing counters and rollover.
- Single polling loop (30s default), timeouts, bounded exponential retries,
  session recovery, cancellation, structured JSON logging and collection status.
- Independent RX/TX accounting, explicit unknown intervals, reset/reboot handling,
  validated rollover rules, rate plausibility limits and restart-safe baselines.
- SQLite migrations, raw samples, inventory, intervals, router state and settings.
- Minimal GET-only JSON API; no dashboard or unrestricted router RPC endpoint.
- Standalone binary, non-root Docker Compose setup and automated CI.

These are mock/software capabilities, not verified Cudy hardware capabilities.
Do not use the results as billing-grade WAN accounting.

## Local installation and mock demonstration

Use Go 1.26 or newer. Build from the repository root:

```sh
go mod download
go build -o bin/monitor ./cmd/monitor
PROVIDER_MODE=mock POLL_INTERVAL=1s ./bin/monitor
```

PowerShell:

```powershell
go build -o bin/monitor.exe ./cmd/monitor
$env:PROVIDER_MODE = 'mock'
$env:POLL_INTERVAL = '1s'
.\bin\monitor.exe
```

In a second terminal, read [health](http://127.0.0.1:8080/healthz),
[status](http://127.0.0.1:8080/api/v1/status),
[capabilities](http://127.0.0.1:8080/api/v1/capabilities),
[devices](http://127.0.0.1:8080/api/v1/devices), or
[usage](http://127.0.0.1:8080/api/v1/usage).

```sh
curl http://127.0.0.1:8080/api/v1/usage
```

The first sample establishes an unknown baseline; later polls add simulated
known deltas. Over the 16-read cycle the demo also generates failure/uncertainty
events. Retries can consume more than one read per poll. Fixture tests use fixed
timestamps and exact counters; the running demo uses wall-clock observation times
and a new simulated boot ID per process. Restart using the same database path:
known subtotals remain, and the new mock boot breaks continuity conservatively.
Stop with Ctrl+C (or SIGTERM on Unix) for graceful shutdown.

## Docker Compose

Docker Engine with Compose v2 is required. From the repository root:

```sh
cp .env.example .env
docker compose --env-file .env -f deployments/docker/compose.yaml up --build -d --wait
docker compose -f deployments/docker/compose.yaml logs -f
docker compose -f deployments/docker/compose.yaml down
```

The named `monitor-data` volume persists SQLite across restarts and `down`.
Do not add `--volumes` unless you intend to delete that database. The scratch
runtime runs as UID/GID 65532, has a read-only root filesystem, drops capabilities
and publishes HTTP only at `127.0.0.1:8080`. Its built-in healthcheck checks API
liveness/storage, not router reachability. The image embeds timezone data and
includes CA certificates. Stop grace is 15s; normal shutdown cancels polling.

Compose forwards the nonsecret application settings listed below, while fixing
the container DB at `/data/monitor.db` and listener at `0.0.0.0:8080` inside the
container. The root `.env` values for those two fields apply to the binary only.
Router credentials are not forwarded by this initial Compose configuration;
there is no usable live provider yet.

For future hardware support, normal container bridge networking must permit
access to the router management LAN/address. Check LAN firewall/routing from the
host and container; do not solve connectivity by using host networking,
privileged mode or extra Linux capabilities.

## Configuration

The binary reads environment variables; it does not automatically parse `.env`.
Use [.env.example](.env.example) as a safe starting point and load it explicitly
in your service manager or shell. Never commit actual credentials.

- `PROVIDER_MODE`: `mock` (default) or `cudy` (validation gate).
- `POLL_INTERVAL`: Go duration, default `30s`, allowed `1s` to `24h`.
- `REQUEST_TIMEOUT`: timeout per connection/read attempt and storage operation,
  default `5s`, allowed `100ms` to `1m`.
- `MAX_RETRIES`: additional attempts after the first, default `2`, allowed 0–5.
- `DATABASE_PATH`: persistent SQLite path, default `data/monitor.db`. One process
  per database; in-memory databases are deliberately rejected.
- `LOG_LEVEL`: `debug`, `info` (default), `warn` or `error`.
- `HTTP_LISTEN_ADDRESS`: default `127.0.0.1:8080`.
- `TIME_ZONE`: IANA display-zone name, default `UTC`; timestamps remain UTC.
- `ROUTER_ADDRESS`: optional HTTP(S) origin for future validated Cudy support;
  credentials in URLs, queries, fragments and endpoint paths are rejected.
- `ROUTER_USERNAME`: reserved for the validated authentication contract.
- `ROUTER_PASSWORD` or `ROUTER_PASSWORD_FILE`: mutually exclusive credential
  sources, reserved for future support. File reads are limited to 64KiB and
  trailing newline characters are removed. Credentials are never persisted.

## HTTP API

All endpoints accept GET only and return `{"data": ..., "error": null}` on
success or a safe error object with `data: null`. Responses disable caching.

- `/healthz`: process/API and SQLite liveness (200/503).
- `/api/v1/status`: running state, mode/simulation flag, collection counts,
  last attempt/success and sanitized last error.
- `/api/v1/capabilities`: support flags with evidence and limitations.
- `/api/v1/devices`: persistent inventory, latest inventory presence and last-seen
  timestamps. Presence is not a reachability test and may be stale during errors.
- `/api/v1/usage`: all-time known subtotals grouped by device/counter/scope, with
  per-direction known/unknown counts and sample bounds. Totals are decimal
  strings; a subtotal with no known intervals is null, not zero.

No authentication or TLS is provided by the API. Keep loopback binding, or use
an authenticated TLS reverse proxy and appropriate network controls.

## Architecture and limitations

One process wires provider → collector → atomic SQLite ingestion/accounting;
the HTTP API reads SQLite and safe collector status. The provider returns a
coherent inventory/counter snapshot. Accounting remains a pure function;
checkpoints and results commit together. There are no background RPC proxies,
external services, empty placeholder packages or frontend dependencies.

Read [architecture](docs/architecture.md), [accounting rules](docs/accounting.md),
[compatibility](docs/compatibility.md), [API research and real-router validation](docs/cudy-api-research.md),
[security](SECURITY.md) and [contribution guidelines](CONTRIBUTING.md).

Known limits: no live hardware support, no WAN-accuracy claim, no calendar
aggregation, automatic retention, data encryption, multi-process collector
coordination or dashboard. Missing polls, resets, unknown continuity and device
disappearance create unknown intervals. Sampling cannot recover unobserved
traffic or distinguish all possible resets. Local validation does not establish
production operation or compatibility with a physical router.

## Development checks

```sh
go fmt ./...
go vet ./...
go test ./...
CGO_ENABLED=1 go test -race ./...
go build ./cmd/monitor
docker compose -f deployments/docker/compose.yaml build
```

The runtime does not require a C compiler; Go's race detector does. CI runs
format/vet/tests/race/build checks and a Docker build plus HTTP/restart smoke test
with persistent storage. No tests require physical hardware.
