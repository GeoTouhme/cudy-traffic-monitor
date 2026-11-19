CREATE TABLE devices (
 router_id TEXT NOT NULL, device_id TEXT NOT NULL, name TEXT NOT NULL,
 present INTEGER NOT NULL, first_seen INTEGER NOT NULL, last_seen INTEGER NOT NULL,
 PRIMARY KEY (router_id, device_id)
);
CREATE TABLE router_state (
 router_id TEXT PRIMARY KEY, observed_at INTEGER NOT NULL, info_json TEXT NOT NULL
);
CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT NOT NULL);
CREATE TABLE samples (
 id INTEGER PRIMARY KEY, router_id TEXT NOT NULL, device_id TEXT NOT NULL,
 counter_id TEXT NOT NULL, observed_at INTEGER NOT NULL, rx TEXT, tx TEXT,
 payload TEXT NOT NULL,
 UNIQUE(router_id, device_id, counter_id, observed_at)
);
CREATE TABLE usage_intervals (
 sample_id INTEGER PRIMARY KEY REFERENCES samples(id),
 router_id TEXT NOT NULL, device_id TEXT NOT NULL, counter_id TEXT NOT NULL,
 start_at INTEGER NOT NULL, end_at INTEGER NOT NULL,
 rx TEXT, tx TEXT, rx_reason TEXT NOT NULL, tx_reason TEXT NOT NULL, scope TEXT NOT NULL
);
CREATE INDEX usage_device_time ON usage_intervals(router_id, device_id, end_at);
CREATE TABLE checkpoints (
 router_id TEXT NOT NULL, device_id TEXT NOT NULL, counter_id TEXT NOT NULL,
 payload TEXT NOT NULL, PRIMARY KEY(router_id, device_id, counter_id)
);
CREATE TABLE collection_events (
 id INTEGER PRIMARY KEY, observed_at INTEGER NOT NULL, code TEXT NOT NULL
);
