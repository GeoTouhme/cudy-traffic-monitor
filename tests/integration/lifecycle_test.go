package integration

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/GeoTouhme/cudy-traffic-monitor/internal/accounting"
	"github.com/GeoTouhme/cudy-traffic-monitor/internal/router/mock"
	"github.com/GeoTouhme/cudy-traffic-monitor/internal/router/provider"
	"github.com/GeoTouhme/cudy-traffic-monitor/internal/storage"
)

func TestFixtureLifecycleWithRestart(t *testing.T) {
	f, err := os.Open("../fixtures/lifecycle.json")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	p, err := mock.Load(f)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if _, err = f.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	var fixture mock.Fixture
	if err = json.NewDecoder(f).Decode(&fixture); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err = p.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "monitor.db")
	db, err := storage.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { db.Close() }()
	policy := accounting.Policy{MaxGap: 45 * time.Second}
	for i, step := range fixture.Steps {
		snap, err := p.Read(ctx)
		if step.Error != "" {
			if provider.CodeOf(err) != step.Error {
				t.Fatalf("step %d: %v", i, err)
			}
			if err = db.RecordGap(ctx, step.Snapshot.At, step.Error, policy); err != nil {
				t.Fatal(err)
			}
			if step.Error == provider.Unauthorized {
				if err = p.Recover(ctx); err != nil {
					t.Fatal(err)
				}
			}
			continue
		}
		if err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
		if err = db.Record(ctx, snap, policy); err != nil {
			t.Fatalf("step %d record: %v", i, err)
		}
		if i == 7 {
			if err = db.Close(); err != nil {
				t.Fatal(err)
			}
			db, err = storage.Open(ctx, path)
			if err != nil {
				t.Fatal(err)
			}
			if err = db.Record(ctx, snap, policy); !errors.Is(err, storage.ErrStale) {
				t.Fatalf("restart replay counted: %v", err)
			}
		}
	}
	u, err := db.Usage(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(u) != 1 || u[0].RX == nil || *u[0].RX != "800" || *u[0].TX != "1800" || u[0].RXKnown != 4 || u[0].TXKnown != 6 || u[0].RXUnknown != 12 || u[0].TXUnknown != 10 {
		t.Fatalf("fixture usage: %+v", u)
	}
	d, err := db.Devices(ctx)
	if err != nil || len(d) != 1 || !d[0].Present {
		t.Fatalf("inventory: %+v %v", d, err)
	}
}
