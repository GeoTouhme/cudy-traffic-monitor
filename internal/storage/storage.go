// Package storage owns SQLite migrations, atomic ingestion and query models.
package storage

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/GeoTouhme/cudy-traffic-monitor/internal/accounting"
	"github.com/GeoTouhme/cudy-traffic-monitor/internal/router/provider"
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrations embed.FS

var ErrStale = errors.New("observation is not newer than persisted state")

type Store struct {
	db    *sql.DB
	write sync.Mutex
}

func Open(ctx context.Context, path string) (*Store, error) {
	if path == "" || path == ":memory:" {
		return nil, errors.New("persistent database path required")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(filepath.Dir(abs), 0700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(abs, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = f.Close(); err != nil {
		return nil, err
	}
	dsnPath := filepath.ToSlash(abs)
	if filepath.VolumeName(abs) != "" {
		dsnPath = "/" + dsnPath
	}
	dsn := (&url.URL{Scheme: "file", Path: dsnPath}).String()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	for _, q := range []string{"PRAGMA busy_timeout=5000", "PRAGMA foreign_keys=ON", "PRAGMA journal_mode=WAL", "PRAGMA synchronous=FULL"} {
		if _, err = db.ExecContext(ctx, q); err != nil {
			db.Close()
			return nil, err
		}
	}
	if err = s.migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}
func (s *Store) migrate(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var version int
	if err = tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if version > 1 {
		return errors.New("database schema is newer than this application")
	}
	if version == 0 {
		b, e := migrations.ReadFile("migrations/001_core.sql")
		if e != nil {
			return e
		}
		if _, err = tx.ExecContext(ctx, string(b)); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "PRAGMA user_version=1"); err != nil {
			return err
		}
	}
	return tx.Commit()
}
func (s *Store) Close() error                   { return s.db.Close() }
func (s *Store) Ping(ctx context.Context) error { return s.db.PingContext(ctx) }

// Settings is deliberately a whitelist, excluding router addresses and secrets.
type Settings struct {
	Provider     string
	PollInterval time.Duration
	TimeZone     string
}

func (s *Store) SaveSettings(ctx context.Context, cfg Settings) error {
	s.write.Lock()
	defer s.write.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for k, v := range map[string]string{"provider": cfg.Provider, "poll_interval": cfg.PollInterval.String(), "time_zone": cfg.TimeZone} {
		if _, err = tx.ExecContext(ctx, "INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", k, v); err != nil {
			return err
		}
	}
	return tx.Commit()
}

type key struct{ device, counter string }

func checkpoints(ctx context.Context, tx *sql.Tx, router string) (map[key]accounting.Sample, error) {
	rows, err := tx.QueryContext(ctx, "SELECT payload FROM checkpoints WHERE router_id=?", router)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := map[key]accounting.Sample{}
	for rows.Next() {
		var b string
		if err = rows.Scan(&b); err != nil {
			return nil, err
		}
		var sample accounting.Sample
		if err = json.Unmarshal([]byte(b), &sample); err != nil {
			return nil, err
		}
		m[key{sample.DeviceID, sample.Counter.ID}] = sample
	}
	return m, rows.Err()
}
func textNumber(n *uint64) any {
	if n == nil {
		return nil
	}
	return strconv.FormatUint(*n, 10)
}

// Record commits raw observations, derived intervals and baselines together.
// A replay or older snapshot is rejected as a whole, preventing double-counts.
func (s *Store) Record(ctx context.Context, snap provider.Snapshot, policy accounting.Policy) error {
	s.write.Lock()
	defer s.write.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = record(ctx, tx, snap, policy, false); err != nil {
		return err
	}
	return tx.Commit()
}
func record(ctx context.Context, tx *sql.Tx, snap provider.Snapshot, policy accounting.Policy, gap bool) error {
	if snap.Router.ID == "" || snap.At.IsZero() || snap.At.Year() < 2000 || snap.At.Year() > 2200 {
		return provider.NewError(provider.InvalidData, "snapshot")
	}
	snap.At = snap.At.UTC()
	var last int64
	err := tx.QueryRowContext(ctx, "SELECT observed_at FROM router_state WHERE router_id=?", snap.Router.ID).Scan(&last)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil && snap.At.UnixNano() <= last {
		return ErrStale
	}
	prior, err := checkpoints(ctx, tx, snap.Router.ID)
	if err != nil {
		return err
	}
	devices := map[string]bool{}
	for _, d := range snap.Devices {
		if d.ID == "" || devices[d.ID] {
			return provider.NewError(provider.InvalidData, "inventory")
		}
		devices[d.ID] = true
	}
	seen := map[key]bool{}
	for _, r := range snap.Readings {
		k := key{r.DeviceID, r.Counter.ID}
		if !devices[r.DeviceID] || r.Counter.ID == "" || seen[k] {
			return provider.NewError(provider.InvalidData, "reading")
		}
		seen[k] = true
	}
	if !gap {
		if _, err = tx.ExecContext(ctx, "UPDATE devices SET present=0 WHERE router_id=?", snap.Router.ID); err != nil {
			return err
		}
		for _, d := range snap.Devices {
			_, err = tx.ExecContext(ctx, `INSERT INTO devices(router_id,device_id,name,present,first_seen,last_seen) VALUES(?,?,?,1,?,?)
ON CONFLICT(router_id,device_id) DO UPDATE SET name=excluded.name,present=1,last_seen=excluded.last_seen`, snap.Router.ID, d.ID, d.Name, snap.At.UnixNano(), snap.At.UnixNano())
			if err != nil {
				return err
			}
		}
	}
	// Missing inventory entries and missing readings must break continuity.
	// Never silently carry a previous counter across disappearance/reappearance.
	for k, old := range prior {
		if seen[k] {
			continue
		}
		r := old.Reading
		r.RX = nil
		r.TX = nil
		r.MissingReason = "missing_counter"
		if !devices[k.device] {
			r.MissingReason = "device_absent"
		}
		if gap {
			r.MissingReason = "collection_failed"
		}
		snap.Readings = append(snap.Readings, r)
	}
	for _, r := range snap.Readings {
		cur := accounting.Sample{At: snap.At, BootID: snap.Router.BootID, Reading: r}
		var previous *accounting.Sample
		if p, ok := prior[key{r.DeviceID, r.Counter.ID}]; ok {
			previous = &p
		}
		i := accounting.Calculate(previous, cur, policy)
		payload, err := json.Marshal(cur)
		if err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, "INSERT INTO samples(router_id,device_id,counter_id,observed_at,rx,tx,payload) VALUES(?,?,?,?,?,?,?)", snap.Router.ID, r.DeviceID, r.Counter.ID, snap.At.UnixNano(), textNumber(r.RX), textNumber(r.TX), string(payload))
		if err != nil {
			return err
		}
		id, err := res.LastInsertId()
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO usage_intervals(sample_id,router_id,device_id,counter_id,start_at,end_at,rx,tx,rx_reason,tx_reason,scope) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, id, snap.Router.ID, r.DeviceID, r.Counter.ID, i.Start.UnixNano(), i.End.UnixNano(), textNumber(i.RX), textNumber(i.TX), i.RXReason, i.TXReason, i.Scope)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO checkpoints(router_id,device_id,counter_id,payload) VALUES(?,?,?,?) ON CONFLICT(router_id,device_id,counter_id) DO UPDATE SET payload=excluded.payload`, snap.Router.ID, r.DeviceID, r.Counter.ID, string(payload))
		if err != nil {
			return err
		}
	}
	info, err := json.Marshal(snap.Router)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO router_state(router_id,observed_at,info_json) VALUES(?,?,?) ON CONFLICT(router_id) DO UPDATE SET observed_at=excluded.observed_at,info_json=excluded.info_json`, snap.Router.ID, snap.At.UnixNano(), string(info))
	return err
}

// RecordGap persists a failed collection and NULL observations at that time for
// every tracked entity. No router error message or authentication body is saved.
func (s *Store) RecordGap(ctx context.Context, at time.Time, code provider.Code, policy accounting.Policy) error {
	s.write.Lock()
	defer s.write.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "INSERT INTO collection_events(observed_at,code) VALUES(?,?)", at.UnixNano(), string(code)); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, "SELECT info_json FROM router_state")
	if err != nil {
		return err
	}
	var infos []provider.Info
	for rows.Next() {
		var b string
		if err = rows.Scan(&b); err != nil {
			rows.Close()
			return err
		}
		var info provider.Info
		if err = json.Unmarshal([]byte(b), &info); err != nil {
			rows.Close()
			return err
		}
		infos = append(infos, info)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, info := range infos {
		err = record(ctx, tx, provider.Snapshot{At: at, Router: info}, policy, true)
		if errors.Is(err, ErrStale) {
			// A backwards clock cannot create a forward-timestamped sample,
			// but it must still break accounting continuity at the next read.
			prior, e := checkpoints(ctx, tx, info.ID)
			if e != nil {
				return e
			}
			for k, p := range prior {
				p.RX, p.TX = nil, nil
				p.MissingReason = "clock_discontinuity"
				payload, e := json.Marshal(p)
				if e != nil {
					return e
				}
				if _, e = tx.ExecContext(ctx, "UPDATE checkpoints SET payload=? WHERE router_id=? AND device_id=? AND counter_id=?", string(payload), info.ID, k.device, k.counter); e != nil {
					return e
				}
			}
		} else if err != nil {
			return err
		}
	}
	return tx.Commit()
}

type Device struct {
	RouterID  string    `json:"router_id"`
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Present   bool      `json:"present_at_last_inventory"`
	FirstSeen time.Time `json:"first_seen"`
	LastSeen  time.Time `json:"last_seen"`
}

func (s *Store) Devices(ctx context.Context) ([]Device, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT router_id,device_id,name,present,first_seen,last_seen FROM devices ORDER BY router_id,device_id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Device{}
	for rows.Next() {
		var d Device
		var first, last int64
		if err = rows.Scan(&d.RouterID, &d.ID, &d.Name, &d.Present, &first, &last); err != nil {
			return nil, err
		}
		d.FirstSeen = time.Unix(0, first).UTC()
		d.LastSeen = time.Unix(0, last).UTC()
		result = append(result, d)
	}
	return result, rows.Err()
}

type Usage struct {
	RouterID  string `json:"router_id"`
	DeviceID  string `json:"device_id"`
	CounterID string `json:"counter_id"`
	Scope     string `json:"scope"`
	// Strings avoid loss above JavaScript's exact integer range.
	RX          *string   `json:"known_rx_bytes"`
	TX          *string   `json:"known_tx_bytes"`
	RXKnown     int       `json:"rx_known_intervals"`
	TXKnown     int       `json:"tx_known_intervals"`
	RXUnknown   int       `json:"rx_unknown_intervals"`
	TXUnknown   int       `json:"tx_unknown_intervals"`
	FirstSample time.Time `json:"first_sample"`
	LastSample  time.Time `json:"last_sample"`
}

// Usage returns lifetime known subtotals and explicit unknown counts. It never
// prorates an interval into calendar buckets or mixes distinct accounting scopes.
func (s *Store) Usage(ctx context.Context) ([]Usage, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT router_id,device_id,counter_id,scope,rx,tx,start_at,end_at FROM usage_intervals ORDER BY sample_id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type group struct {
		Usage
		rx, tx uint64
	}
	groups := map[[4]string]*group{}
	for rows.Next() {
		var router, device, counter, scope string
		var rx, tx sql.NullString
		var start, end int64
		if err = rows.Scan(&router, &device, &counter, &scope, &rx, &tx, &start, &end); err != nil {
			return nil, err
		}
		k := [4]string{router, device, counter, scope}
		g := groups[k]
		if g == nil {
			g = &group{Usage: Usage{RouterID: router, DeviceID: device, CounterID: counter, Scope: scope, FirstSample: time.Unix(0, start).UTC()}}
			groups[k] = g
		}
		g.LastSample = time.Unix(0, end).UTC()
		for _, dir := range []struct {
			value          sql.NullString
			total          *uint64
			known, unknown *int
		}{{rx, &g.rx, &g.RXKnown, &g.RXUnknown}, {tx, &g.tx, &g.TXKnown, &g.TXUnknown}} {
			if !dir.value.Valid {
				*dir.unknown++
				continue
			}
			n, e := strconv.ParseUint(dir.value.String, 10, 64)
			if e != nil {
				return nil, e
			}
			if n > math.MaxUint64-*dir.total {
				return nil, fmt.Errorf("usage sum overflow")
			}
			*dir.total += n
			*dir.known++
		}
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	result := []Usage{}
	for _, g := range groups {
		if g.RXKnown > 0 {
			v := strconv.FormatUint(g.rx, 10)
			g.RX = &v
		}
		if g.TXKnown > 0 {
			v := strconv.FormatUint(g.tx, 10)
			g.TX = &v
		}
		result = append(result, g.Usage)
	}
	sort.Slice(result, func(i, j int) bool {
		a, b := result[i], result[j]
		return a.RouterID+"\x00"+a.DeviceID+"\x00"+a.CounterID+"\x00"+a.Scope < b.RouterID+"\x00"+b.DeviceID+"\x00"+b.CounterID+"\x00"+b.Scope
	})
	return result, nil
}
