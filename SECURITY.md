# Security policy

This project is pre-release. Live Cudy collection is disabled until the exact
stock firmware's authentication and read contracts are validated. Mock mode
does not access the router or require credentials.

Report vulnerabilities privately using this repository's **Security > Report a
vulnerability** feature if enabled. If unavailable, contact the maintainer via
the contact method on their GitHub profile to arrange a private channel. Do not
post passwords, tokens, HAR files, auth responses, router backups or client
identifiers in a public issue. Never include secrets in a proof of concept.
No response-time commitment is currently offered.

- The API has no authentication or TLS and defaults to localhost. Keep it local
  or behind an authenticated TLS reverse proxy on a trusted network. Deliberately
  binding a public address exposes device inventory and usage metadata.
- Compose publishes only to host loopback, runs non-root, drops all capabilities,
  disables privilege escalation and makes the image filesystem read-only.
  Only the SQLite volume is writable. Do not enable privileged/host networking.
- No credentials/tokens are stored in SQLite or returned in the API. Raw router
  authentication responses and arbitrary RPC are never exposed. Errors and logs
  use sanitized codes. Do not add configuration dumps or HTTP wire logging.
- Prefer an owner-protected credential file for future validated providers.
  Environment credentials are visible to users with process/container inspection
  privileges. `.env` files are ignored and must be permission-restricted.
- SQLite inventory/usage is sensitive. Restrict directory access, protect backups,
  keep it off public volumes, and run one collector per database. There is no
  application encryption at rest or automatic data-retention policy.
- A future transport must preserve TLS verification, reject redirects, avoid
  environment proxies for LAN credentials, bound response sizes/timeouts, use a
  confirmed read allowlist, and sanitize failures. Never add an auth bypass or
  mutate configuration to make monitoring work.

Review dependency and Go toolchain updates regularly. Existing Apache 2.0
licensing and third-party dependency licenses apply.
