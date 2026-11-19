package storage

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/GeoTouhme/cudy-traffic-monitor/internal/accounting"
	"github.com/GeoTouhme/cudy-traffic-monitor/internal/router/provider"
)

func observation(n uint64, at time.Time) provider.Snapshot {
	rx, tx := n, n*2
	return provider.Snapshot{At: at, Router: provider.Info{ID: "router", BootID: "boot"}, Devices: []provider.Device{{ID: "device", Name: "Laptop"}}, Readings: []provider.Reading{{DeviceID: "device", Counter: provider.Counter{ID: "total", Scope: "interface", Unit: "bytes", Width: 64, MaxBytesPerSecond: 1_000_000}, Epoch: "epoch", RX: &rx, TX: &tx}}}
}
func openTest(t *testing.T, path string) *Store {
	t.Helper()
	s, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func TestPersistenceRestartReplayAndAtomicity(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "database #.db")
	s := openTest(t, path)
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	policy := accounting.Policy{MaxGap: 45 * time.Second}
	for n := uint64(1); n <= 2; n++ {
		if err := s.Record(ctx, observation(n*100, at.Add(time.Duration(n)*30*time.Second)), policy); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.SaveSettings(ctx, Settings{Provider: "mock", PollInterval: 30 * time.Second, TimeZone: "UTC"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s = openTest(t, path)
	defer s.Close()
	replay := observation(200, at.Add(60*time.Second))
	if err := s.Record(ctx, replay, policy); !errors.Is(err, ErrStale) {
		t.Fatalf("replay: %v", err)
	}
	if err := s.Record(ctx, observation(300, at.Add(90*time.Second)), policy); err != nil {
		t.Fatal(err)
	}
	usage, err := s.Usage(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(usage) != 1 || usage[0].RX == nil || *usage[0].RX != "200" || *usage[0].TX != "400" || usage[0].RXUnknown != 1 {
		t.Fatalf("restart usage: %+v", usage)
	}
	bad := observation(400, at.Add(120*time.Second))
	bad.Readings = append(bad.Readings, bad.Readings[0])
	if err = s.Record(ctx, bad, policy); provider.CodeOf(err) != provider.InvalidData {
		t.Fatalf("invalid sample: %v", err)
	}
	var count int
	if err = s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM samples").Scan(&count); err != nil || count != 3 {
		t.Fatalf("partial write: %d %v", count, err)
	}
	if err = s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM settings").Scan(&count); err != nil || count != 3 {
		t.Fatalf("settings: %d %v", count, err)
	}
}
func TestMissingDisappearanceReappearanceAndCollectionGap(t *testing.T) {
	ctx := context.Background()
	s := openTest(t, filepath.Join(t.TempDir(), "db"))
	defer s.Close()
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	policy := accounting.Policy{MaxGap: 45 * time.Second}
	for i := 0; i < 7; i++ {
		snap := observation(uint64(100+i*100), at.Add(time.Duration(i)*30*time.Second))
		if i == 2 {
			snap.Devices = nil
			snap.Readings = nil
		}
		if i == 4 {
			snap.Readings[0].RX = nil
		}
		if i == 5 {
			if err := s.RecordGap(ctx, snap.At, provider.Unavailable, policy); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := s.Record(ctx, snap, policy); err != nil {
			t.Fatal(err)
		}
	}
	u, err := s.Usage(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(u) != 1 || *u[0].RX != "100" || *u[0].TX != "400" || u[0].RXUnknown != 6 || u[0].TXUnknown != 5 {
		t.Fatalf("gaps: %+v", u)
	}
	d, err := s.Devices(ctx)
	if err != nil || len(d) != 1 || !d[0].Present {
		t.Fatalf("reappearance: %+v %v", d, err)
	}
}
func TestUint64RawAndNull(t *testing.T) {
	ctx := context.Background()
	s := openTest(t, filepath.Join(t.TempDir(), "db"))
	defer s.Close()
	snap := observation(1, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	max := ^uint64(0)
	snap.Readings[0].RX = &max
	snap.Readings[0].TX = nil
	if err := s.Record(ctx, snap, accounting.Policy{MaxGap: time.Minute}); err != nil {
		t.Fatal(err)
	}
	var raw string
	var missing any
	if err := s.db.QueryRowContext(ctx, "SELECT rx,tx FROM samples").Scan(&raw, &missing); err != nil {
		t.Fatal(err)
	}
	if raw != "18446744073709551615" || missing != nil {
		t.Fatalf("raw counter truncated or NULL lost: %q %v", raw, missing)
	}
	u, err := s.Usage(ctx)
	if err != nil || u[0].RX != nil || u[0].TX != nil {
		t.Fatalf("unknown silently zeroed: %+v %v", u, err)
	}
}
func TestNewerSchemaRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db")
	s := openTest(t, path)
	if _, err := s.db.Exec("PRAGMA user_version=999"); err != nil {
		t.Fatal(err)
	}
	s.Close()
	if s, err := Open(context.Background(), path); err == nil {
		s.Close()
		t.Fatal("downgrade accepted")
	}
}

func TestIngestionRollbackPreservesBaseline(t *testing.T) {
	ctx := context.Background()
	s := openTest(t, filepath.Join(t.TempDir(), "db"))
	defer s.Close()
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	policy := accounting.Policy{MaxGap: time.Minute}
	if err := s.Record(ctx, observation(100, at), policy); err != nil {
		t.Fatal(err)
	}
	// Force failure after raw sample insertion and before checkpoint update.
	if _, err := s.db.Exec(`CREATE TRIGGER reject_usage BEFORE INSERT ON usage_intervals BEGIN SELECT RAISE(ABORT, 'test failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := s.Record(ctx, observation(200, at.Add(30*time.Second)), policy); err == nil {
		t.Fatal("failure was swallowed")
	}
	var count int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM samples").Scan(&count); err != nil || count != 1 {
		t.Fatalf("partial raw write: %d %v", count, err)
	}
	if _, err := s.db.Exec("DROP TRIGGER reject_usage"); err != nil {
		t.Fatal(err)
	}
	if err := s.Record(ctx, observation(250, at.Add(30*time.Second)), policy); err != nil {
		t.Fatal(err)
	}
	u, err := s.Usage(ctx)
	if err != nil || len(u) != 1 || u[0].RX == nil || *u[0].RX != "150" {
		t.Fatalf("checkpoint changed on rollback: %+v %v", u, err)
	}
}

func TestBackwardsClockFailureBreaksContinuity(t *testing.T) {
	ctx := context.Background()
	s := openTest(t, filepath.Join(t.TempDir(), "db"))
	defer s.Close()
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	policy := accounting.Policy{MaxGap: time.Minute}
	if err := s.Record(ctx, observation(100, at), policy); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordGap(ctx, at.Add(-time.Second), provider.InvalidData, policy); err != nil {
		t.Fatal(err)
	}
	if err := s.Record(ctx, observation(200, at.Add(30*time.Second)), policy); err != nil {
		t.Fatal(err)
	}
	u, err := s.Usage(ctx)
	if err != nil || u[0].RX != nil || u[0].RXUnknown != 2 {
		t.Fatalf("clock gap counted: %+v %v", u, err)
	}
}
