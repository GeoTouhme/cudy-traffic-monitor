# Cudy stock-firmware API research

Research date: 2026-11-19. Target: **WR1200 hardware V2.1, stock firmware**.
No physical router, management-interface capture, credentials or firmware image
was available during this work. No router was contacted. Phase 0 delivers a
research record and validation procedure; hardware validation remains blocked.

## Evidence terminology

- **VERIFIED**: directly inspectable source evidence or a reproducible test,
  with the precise scope named. Source inspection verifies what the source says,
  not that a router implements it.
- **INFERRED**: plausible target behavior based on indirect evidence.
- **UNKNOWN**: requires target-device observations or controlled experiments.

**No target RPC, authentication flow or counter is VERIFIED.** Merely finding
another project's method name does not validate it for this application.

## Primary sources inspected

1. [Cudy WR1200 2.1 product specifications](https://www.cudy.com/products/wr1200-2-1):
   identifies hardware revision, Fast Ethernet ports and local web/app control.
2. [Cudy WR1200 2.1 downloads](https://www.cudy.com/es-mx/pages/download-center/wr1200-2-1):
   lists revision-specific firmware, including 2.4.23 (R26, build
   `20251224-145945`) and 2.4.12. A download listing does not identify the
   owner's installed firmware, promise an API, or warrant upgrading.
3. CudyPy commit `5fc25dd5a152d71ab79bc8977d5a8b5d88855b96`:
   [protocol notes](https://github.com/Eleiber/cudypy/blob/5fc25dd5a152d71ab79bc8977d5a8b5d88855b96/docs/protocol.md),
   [implementation](https://github.com/Eleiber/cudypy/blob/5fc25dd5a152d71ab79bc8977d5a8b5d88855b96/cudypy/core/router.py),
   [compatibility](https://github.com/Eleiber/cudypy/blob/5fc25dd5a152d71ab79bc8977d5a8b5d88855b96/docs/compatibility.md),
   [device semantics](https://github.com/Eleiber/cudypy/blob/5fc25dd5a152d71ab79bc8977d5a8b5d88855b96/docs/device-semantics.md).
   This is an experimental client. Its authors report checks on WR3000H V1.0
   and WR3000 V2.0, not this target revision. Those reports are not our tests.
4. cudy-admin-api commit `1b8aa49ee9a4cdb5e49124af23608f601f3aa9a5`:
   [README](https://github.com/sebcej/cudy-admin-api/blob/1b8aa49ee9a4cdb5e49124af23608f601f3aa9a5/README.md),
   [login](https://github.com/sebcej/cudy-admin-api/blob/1b8aa49ee9a4cdb5e49124af23608f601f3aa9a5/api/login.go),
   [device parser](https://github.com/sebcej/cudy-admin-api/blob/1b8aa49ee9a4cdb5e49124af23608f601f3aa9a5/api/connectedDevices.go).
   Its README names LT700V firmware 2.1.15 DE. It uses a browser interface,
   illustrating that app RPC and web management are separate contracts.

Sources were read for protocol research. Their code is not copied or imported.
The application retains the repository's existing Apache 2.0 license.

## Evidence table

| Subject | Observed source evidence | Target status / implementation decision |
| --- | --- | --- |
| Hardware revision | Cudy publishes WR1200 2.1 specifications and firmware downloads [1,2]. **VERIFIED** as vendor documentation. | Installed firmware **UNKNOWN**. Match full revision, region and build before testing. |
| Authentication | CudyPy [3] describes an auth RPC challenge and salted SHA-256 login. The Go wrapper [4] parses browser login fields. **VERIFIED** as source content only. | **INFERRED**: local management uses authenticated sessions. Exact target login is **UNKNOWN**; no login implementation enabled. |
| Session lifecycle | CudyPy describes a session value in an authenticated request and reports auth rejection handling. | Lifetime, refresh, concurrent browser/API sessions, logout and lockout policy **UNKNOWN**. Never assume a fixed expiry. |
| CSRF | The browser wrapper parses `_csrf`, `token` and `salt` from its supported UI [4]. | Whether target requires these fields and how they relate to RPC authentication **UNKNOWN**. Never omit or bypass required controls. |
| Read-only methods | CudyPy references `system.info`, `devices.get_devlist_ex`, `net.traffic_stat`, `devices.traffic_stat`. | Candidate names only, all target methods **UNKNOWN**. They are not an allowlist to probe blindly. |
| Device enumeration | CudyPy has full-list/ranged handling; [4] instead parses a device HTML table. | Target schema, pagination, stale clients, stable MAC identity and completeness **UNKNOWN**. |
| WAN totals | CudyPy exposes interface/runtime byte fields and distinct traffic methods. | WAN interface mapping, units and persistence **UNKNOWN**; no WAN capability advertised. |
| Per-device counters | CudyPy preserves native `inbytes`, `outbytes`, `upbytes`, `downbytes` separately [3]. | Byte accumulation, direction, accounting domain and relationship between fields **UNKNOWN**. Do not equate fields or integrate displayed speeds as cumulative totals. |
| Counter direction/units | CudyPy discusses app rate formatting, while explicitly qualifying cumulative-field semantics. | RX/TX perspective and cumulative units **UNKNOWN**. Controlled bidirectional transfers are required. |
| Reboot/reset/rollover | No controlled WR1200 V2.1 evidence obtained. | Width, epoch/reset identifiers, idle resets, reconnect behavior, wrap and reboot persistence **UNKNOWN**. |
| Firmware differences | [3] reports traffic methods succeeding on one tested model and method-not-found on another. | **INFERRED**: firmware-specific discovery is necessary. No inheritance of compatibility from another Cudy model. |
| Application behavior | Mock fixtures and automated tests in this repository. | **VERIFIED only when the named tests execute successfully**; these tests cannot validate hardware. |

The generic OpenWrt ubus protocol does not establish that stock Cudy firmware
exposes `/ubus`, uses a particular username, or accepts generic ubus payloads.
No ubus client, arbitrary-RPC route, fallback guessing or unauthenticated probe
is implemented. `internal/router/cudy` returns typed `unvalidated_firmware`
errors for operations and false capability flags, without network I/O.

## Reproducible real-device validation

Perform these steps on an owner-authorized LAN test setup. Do not supply router
credentials in issues, chat, command-line arguments or committed files.

1. Record label model, hardware revision/region, stock firmware version and full
   build, operating mode (router/AP/repeater), wired/Wi-Fi topology, mesh nodes,
   uptime, offload setting if already visible, timezone and test UTC timestamps.
   Keep an identifying photo privately; redact serial numbers before sharing.
2. Open the normal local management UI in a browser. With developer tools open,
   sign in normally using credentials entered directly in the browser. Observe
   the actual authentication exchange, required CSRF/challenge fields, session
   transport and ordinary expiry response. Do not brute-force expiry or login.
   Keep TLS verification enabled; use the owner's trusted certificate setup.
3. Visit only existing status/device/traffic pages without changing settings.
   Record the read requests the UI itself makes. If the candidate extended
   device-list method is absent, record **not observed** rather than trying
   guessed calls. Determine read-only behavior before any replay.
4. Save private request/response shapes, status codes and timestamps. Strip
   passwords, password hashes, salts/challenges, cookies, authorization headers,
   auth queries, tokens, serial numbers, WAN IPs, SSIDs and client identifiers
   from shared fixtures. Inspect HAR files manually; they commonly embed secrets.
   Replace identifiers consistently. Preserve numbers, nulls and missing fields.
5. Verify complete inventory with one wired and one Wi-Fi test client. Observe
   quiet/sleeping clients, lease expiry, disconnect and reconnect. Determine
   pagination from the UI's own request sequence. A presence/activity field is
   not proof of reachability or Internet access.
6. Collect at least three idle snapshots 30 seconds apart. From one isolated
   test client, download a known-size file from an owner-controlled WAN host;
   collect before/during/after snapshots. Repeat with upload. Compare every
   candidate cumulative field, WAN interface totals and host transfer bytes,
   allowing documented protocol overhead. Repeat for the other client and IPv6
   if enabled. Do not inspect packet contents.
7. Transfer a known-size file between two LAN clients. If per-device fields move,
   they are not established as WAN-only. Repeat with inter-Wi-Fi and wired/Wi-Fi
   traffic and existing acceleration/offload settings unchanged. Record a scope
   of `interface` or `unknown` until supported by the experiments.
8. Stop polling briefly, resume, and compare continuity. Observe natural session
   expiry and normal reauthentication, checking whether it disturbs the browser
   session. Request at most one read at a time; no broad method enumeration.
9. During a separately owner-scheduled physical reboot, capture before/after
   counters and uptime/boot identifiers. The monitor itself must never issue
   reboot, clear-counter, config-write, firmware, shell or session-management
   RPCs that mutate router configuration. Do not trigger resets for research.
10. Width/rollover cannot be established from a small observed value. Prefer
    firmware/schema evidence plus a naturally observed rollover. If no reliable
    reset epoch or rate bound can be established, leave rollover unsupported
    and decreases unknown. Do not generate excessive traffic just to force wrap.
11. Add sanitized fixtures for the exact firmware, document observed payloads
    and sources, implement only confirmed reads in `internal/router/cudy`, and
    add `httptest` tests for auth rejection, malformed data, pagination, timeouts,
    redirect refusal and secret redaction before advertising capabilities.

Acceptance evidence must state test setup, UTC times, request forms, firmware,
expected and observed deltas, LAN-only results, uncertainty and the limits of
the experiment. Never claim billing accuracy from matching a single transfer.

## Remaining blockers

- Physical WR1200 V2.1 and owner-authorized normal management observations.
- Exact authentication/session/CSRF contract and sanitized read captures.
- Counter domains, units, direction, width, reset semantics and validated bounds.
- Independent WAN-versus-LAN and offload measurements on each supported build.
