# Contributing

Start with a focused issue or pull request. Go 1.26+ is required; CI checks the
toolchain lines configured in `.github/workflows/ci.yml`.

```sh
go fmt ./...
go vet ./...
go test ./...
go test -race ./...  # Requires CGO_ENABLED=1 and a supported C compiler.
go build ./cmd/monitor
docker compose -f deployments/docker/compose.yaml build
```

Keep changes small and idiomatic. Add meaningful tests for accounting, protocol,
storage and failure behavior. Tests must not require a router. Keep provider I/O
read-only, context-aware, bounded and separate from accounting. Preserve unknown
values and full unsigned counters. Never infer WAN scope from a field name.

For hardware contributions, follow `docs/cudy-api-research.md`. Name exact model,
hardware/region, firmware/build and operating mode. Label VERIFIED, INFERRED and
UNKNOWN evidence explicitly. Sanitize captures before sharing; do not include
raw HARs or credentials. Do not copy code with incompatible licensing.

Update documentation alongside behavior. PR descriptions must identify checks
actually executed, unverified assumptions and outstanding hardware tests.
Do not claim passing Docker, race tests or deployment based only on a Go build.
Use feature branches and never force-push shared branches or merge automatically.
