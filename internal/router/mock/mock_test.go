package mock

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/GeoTouhme/cudy-traffic-monitor/internal/router/provider"
)

func TestDemoLifecycle(t *testing.T) {
	p := New()
	p.now = func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }
	ctx := context.Background()
	if err := p.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	var missing, absent, auth, disconnect, reboot, reset bool
	previousBoot := ""
	previousEpoch := ""
	for i := 0; i < 16; i++ {
		s, err := p.Read(ctx)
		if err != nil {
			switch provider.CodeOf(err) {
			case provider.Unauthorized:
				auth = true
				p.Recover(ctx)
			case provider.Unavailable:
				disconnect = true
			default:
				t.Fatal(err)
			}
			continue
		}
		if previousBoot != "" && previousBoot != s.Router.BootID {
			reboot = true
		}
		previousBoot = s.Router.BootID
		if len(s.Devices) == 0 {
			absent = true
			devices, err := p.Devices(ctx)
			if err != nil || len(devices) != 0 {
				t.Fatal("enumeration contradicts snapshot")
			}
		}
		info, err := p.Identify(ctx)
		if err != nil || info != s.Router {
			t.Fatal("identification contradicts snapshot")
		}
		for _, r := range s.Readings {
			if r.RX == nil {
				missing = true
			}
			if previousEpoch != "" && previousEpoch != r.Epoch {
				reset = true
			}
			previousEpoch = r.Epoch
		}
	}
	if !missing || !absent || !auth || !disconnect || !reboot || !reset {
		t.Fatalf("incomplete mock events: %v %v %v %v %v %v", missing, absent, auth, disconnect, reboot, reset)
	}
	if p.Capabilities().WANOnly.Supported || !p.Capabilities().Simulated {
		t.Fatal("mock misrepresented")
	}
}
func TestFixtureValidationAndIsolation(t *testing.T) {
	for _, s := range []string{`{}`, `{"steps":[]}`, `{"steps":[{"error":"internal"}]}`, `{"unknown":true}`, `{"steps":[{"snapshot":{"at":"2026-01-01T00:00:00Z","router":{"id":"real-router"}}}]}`} {
		if _, err := Load(strings.NewReader(s)); err == nil {
			t.Fatal("invalid fixture accepted")
		}
	}
	p, err := Load(strings.NewReader(`{"steps":[{"snapshot":{"at":"2026-01-01T00:00:00Z","router":{"id":"mock-router"},"devices":[{"id":"d","name":"original"}],"readings":[]}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	p.Connect(ctx)
	a, _ := p.Read(ctx)
	a.Devices[0].Name = "mutated"
	b, _ := p.Read(ctx)
	if b.Devices[0].Name != "original" || !b.At.Equal(a.At) {
		t.Fatal("fixture changed or exhaustion invented time")
	}
	p.Close()
	if _, err = p.Read(ctx); provider.CodeOf(err) != provider.Unauthorized {
		t.Fatal("closed provider readable")
	}
}
