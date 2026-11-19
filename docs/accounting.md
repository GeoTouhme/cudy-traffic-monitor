# Sampling and accounting

This is **not billing-grade accounting**. The hardware counter semantics are
unvalidated. Mock results are explicitly simulated. A per-device interface total
must never be relabeled as WAN consumption.

For each `(router, device, counter)` the persisted previous observation is the
baseline. At the first sample, usage is unknown, not the absolute counter value.
Subsequent RX and TX deltas are evaluated independently:

1. Reject non-forward timestamps and intervals longer than 1.5 polling periods.
   A 30s poll normally allows at most 45s between observations. Unknown gaps are
   never interpolated, assigned to an arbitrary day, or filled with zeroes.
2. Require the same known boot identifier and matching counter metadata. Reboot,
   metadata change, unknown byte units or unknown scope makes the interval unknown.
3. An explicit epoch change indicates a reset. Decreases without validated
   rollover/reset continuity remain unknown.
4. Missing RX or TX makes only that direction unknown. Missing device/counter
   observations insert NULL baselines, breaking continuity on reappearance. Full
   collection failures persist unknown observations and a sanitized event code.
5. For supported byte counters with known width and a validated positive rate
   bound, monotonic values produce `current - previous`. A validated rollover
   with unchanged known epoch/boot uses `(2^width - previous) + current`.
   Never guess width from observed values or confuse a reset with rollover.
6. Reject deltas above the rate bound times elapsed time. If enough bytes could
   have passed to wrap a 32-bit counter more than once (even when the current
   value is larger), the interval is unknown. Bounds that exceed uint64 are
   unknown. No signed subtraction or negative usage is possible.

Raw samples are retained even when the resulting delta is unknown or implausible.
The new observation becomes the next baseline; a corrupt jump and subsequent
decrease may invalidate more than one interval. On restart the last baseline is
loaded from SQLite within the ingestion transaction. Replayed/older snapshots
are rejected atomically, and a unique interval per ending sample prevents replay
double-counts. A failure to commit updates neither the sample nor its checkpoint.
An out-of-order failed collection cannot insert a forward observation; it still
invalidates the checkpoint values so the next read cannot bridge a clock jump.

Unknown reasons include `baseline`, `polling_gap`, `boot_unknown`, `router_reboot`,
`counter_changed`, `unit_unknown`, `scope_unknown`, `counter_reset`,
`missing_counter`, `device_absent`, `collection_failed`, `width_unknown`,
`rate_bound_unknown`, `rate_bound_overflow`, `counter_out_of_range`,
`reset_or_rollover`, `multiple_wraps_possible`, `implausible_jump`, and
`clock_discontinuity`. The returning observation can still be unknown because
the preceding baseline was NULL. Baseline rows are zero-duration unknown rows,
included in the API's unknown-interval counts.

The API returns lifetime **known subtotals** and separate per-direction known and
unknown counts. If a direction has no known intervals, its subtotal is JSON null;
otherwise a known zero is the string `"0"`. Totals are decimal strings to preserve
integers above JavaScript's exact range. A partial subtotal is never a complete
total. Timestamps are UTC; `TIME_ZONE` declares a preferred display timezone and
does not change storage or create calendar aggregations.

## Limits

- Two polls cannot reveal a reset followed by enough traffic to exceed the
  previous value unless a trustworthy epoch/reboot marker changes. Hardware
  providers must establish continuity metadata rather than invent it.
- Sampling cannot recover bytes during unknown intervals or locate activity
  precisely within an interval. The lifetime API does not split days/months.
- Local scheduling jitter, long request/retry delays and wall-clock changes can
  cause conservative unknown intervals. Correct host time synchronization matters.
- Counts may include LAN traffic, protocol overhead, NAT/offload differences,
  guest/mesh/bridge traffic or only a subset of clients. Scope must be validated.
- MAC randomization/identity changes split devices; names are not identity keys.
- DB retention and compaction are not automated. Monitor disk space, protect the
  volume and back up with SQLite's online backup facility or stop the application
  before copying the database together with its WAL state.
- Liveness does not prove collection freshness. Consult collector last success,
  failure status, latest sample time and counter scope before using a subtotal.
