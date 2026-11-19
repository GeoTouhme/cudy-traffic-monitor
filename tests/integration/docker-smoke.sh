#!/usr/bin/env bash
set -euo pipefail
compose=(docker compose -f deployments/docker/compose.yaml)
"${compose[@]}" up --build --detach --wait --wait-timeout 120
id=$("${compose[@]}" ps --quiet monitor)
test "$(docker inspect --format '{{.Config.User}}' "$id")" = '65532:65532'
test "$(docker inspect --format '{{.HostConfig.ReadonlyRootfs}}' "$id")" = true
docker exec "$id" /monitor healthcheck
for attempt in $(seq 1 40); do
  if curl -fsS http://127.0.0.1:8080/api/v1/usage > /tmp/cudy-before.json && \
    python3 -c 'import json; d=json.load(open("/tmp/cudy-before.json")); u=d["data"]["usage"]; assert u and u[0]["scope"]=="simulated" and int(u[0]["known_rx_bytes"] or 0)>0'; then
    break
  fi
  sleep 0.25
done
python3 -c 'import json; u=json.load(open("/tmp/cudy-before.json"))["data"]["usage"]; assert u and int(u[0]["known_rx_bytes"] or 0)>0'
"${compose[@]}" restart --timeout 15 monitor
"${compose[@]}" up --detach --wait --wait-timeout 90
curl -fsS http://127.0.0.1:8080/api/v1/devices > /tmp/cudy-devices.json
curl -fsS http://127.0.0.1:8080/api/v1/usage > /tmp/cudy-after.json
python3 - <<'PY'
import json
before = json.load(open('/tmp/cudy-before.json'))['data']['usage'][0]
after = json.load(open('/tmp/cudy-after.json'))['data']['usage'][0]
devices = json.load(open('/tmp/cudy-devices.json'))['data']
assert devices and devices[0]['id'] == 'mock-device-1'
assert int(after['known_rx_bytes']) >= int(before['known_rx_bytes'])
assert after['first_sample'] == before['first_sample']
assert after['rx_unknown_intervals'] >= before['rx_unknown_intervals']
PY
