# Compatibility

## Implemented and tested without hardware

- Go mock provider, deterministic lifecycle fixture, collector and accounting.
- Persistent SQLite samples/checkpoints and restart-safe replay rejection.
- Local JSON API, standalone binary, non-root Docker configuration and CI checks.
  The actual executed results belong in the PR; this list is not evidence that
  a particular environment's Docker build or race run passed.

## Experimental target

**Cudy WR1200 V2.1, stock firmware: NOT YET SUPPORTED for live collection.**
Firmware/region/build is unknown for the physical target. No authentication,
session behavior, RPC method, schema, byte unit, counter direction or WAN-only
semantics has been validated. Cudy mode exposes safe capabilities/status, but
collection returns `unvalidated_firmware` without contacting the router.

Public clients for WR3000/WR3000H or LT700V are research leads; they are not a
compatibility promise. Stock firmware, custom OpenWrt, regional builds and router,
AP, mesh or repeater modes may have different interfaces and accounting domains.
We do not install packages, flash firmware, enable shells or bypass controls.

To propose support, follow the [research validation procedure](cudy-api-research.md)
and submit sanitized observations and fixtures for each exact firmware build.
Capability promotion requires reproducible target evidence and passing protocol
tests. WAN-only claims additionally require controlled WAN and LAN experiments.
