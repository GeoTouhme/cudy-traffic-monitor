package accounting

import (
	"math"
	"testing"
	"time"

	"github.com/GeoTouhme/cudy-traffic-monitor/internal/router/provider"
)

func ptr(n uint64) *uint64 { return &n }
func sample(n uint64) Sample {
	return Sample{At: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), BootID: "boot-1", Reading: provider.Reading{DeviceID: "device", Counter: provider.Counter{ID: "counter", Scope: "interface", Unit: "bytes", Width: 32, Rollover: true, MaxBytesPerSecond: 1_000}, Epoch: "epoch-1", RX: ptr(n), TX: ptr(n)}}
}
func TestCalculate(t *testing.T) {
	tests := []struct {
		name   string
		change func(*Sample, *Sample)
		reason string
		want   *uint64
	}{
		{"monotonic", func(a, b *Sample) {}, "monotonic", ptr(100)},
		{"zero", func(a, b *Sample) { b.RX = ptr(100) }, "monotonic", ptr(0)},
		{"missing current", func(a, b *Sample) { b.RX = nil }, "missing_counter", nil},
		{"missing previous", func(a, b *Sample) { a.RX = nil }, "missing_counter", nil},
		{"reboot", func(a, b *Sample) { b.BootID = "boot-2" }, "router_reboot", nil},
		{"boot unknown", func(a, b *Sample) { b.BootID = "" }, "boot_unknown", nil},
		{"reset", func(a, b *Sample) { b.Epoch = "epoch-2" }, "counter_reset", nil},
		{"unproven rollover", func(a, b *Sample) { a.Epoch = ""; b.Epoch = ""; b.RX = ptr(10) }, "reset_or_rollover", nil},
		{"rollover32", func(a, b *Sample) { a.RX = ptr(math.MaxUint32 - 49); b.RX = ptr(50) }, "rollover", ptr(100)},
		{"rollover64", func(a, b *Sample) {
			a.Counter.Width = 64
			b.Counter.Width = 64
			a.RX = ptr(math.MaxUint64 - 49)
			b.RX = ptr(50)
		}, "rollover", ptr(100)},
		{"reset near wrap", func(a, b *Sample) { a.RX = ptr(math.MaxUint32 - 49); b.RX = ptr(50); b.Epoch = "epoch-2" }, "counter_reset", nil},
		{"wrap disabled", func(a, b *Sample) { a.Counter.Rollover = false; b.Counter.Rollover = false; b.RX = ptr(1) }, "reset_or_rollover", nil},
		{"jump", func(a, b *Sample) { b.RX = ptr(100_000) }, "implausible_jump", nil},
		{"multiple wraps increasing", func(a, b *Sample) { a.Counter.MaxBytesPerSecond = 1 << 32; b.Counter.MaxBytesPerSecond = 1 << 32 }, "multiple_wraps_possible", nil},
		{"out of range", func(a, b *Sample) { b.RX = ptr(1 << 32) }, "counter_out_of_range", nil},
		{"missed polling interval", func(a, b *Sample) { b.At = a.At.Add(60 * time.Second) }, "polling_gap", nil},
		{"clock backwards", func(a, b *Sample) { b.At = a.At.Add(-time.Second) }, "clock_discontinuity", nil},
		{"same time", func(a, b *Sample) { b.At = a.At }, "clock_discontinuity", nil},
		{"unknown scope", func(a, b *Sample) { a.Counter.Scope = "unknown"; b.Counter.Scope = "unknown" }, "scope_unknown", nil},
		{"unknown unit", func(a, b *Sample) { a.Counter.Unit = ""; b.Counter.Unit = "" }, "unit_unknown", nil},
		{"unknown width", func(a, b *Sample) { a.Counter.Width = 0; b.Counter.Width = 0 }, "width_unknown", nil},
		{"unknown rate bound", func(a, b *Sample) { a.Counter.MaxBytesPerSecond = 0; b.Counter.MaxBytesPerSecond = 0 }, "rate_bound_unknown", nil},
		{"metadata change", func(a, b *Sample) { b.Counter.Scope = "wan" }, "counter_changed", nil},
		{"overflow bound", func(a, b *Sample) { a.Counter.MaxBytesPerSecond = math.MaxUint64; b.Counter = a.Counter }, "rate_bound_overflow", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, b := sample(100), sample(200)
			b.At = a.At.Add(30 * time.Second)
			tt.change(&a, &b)
			i := Calculate(&a, b, Policy{MaxGap: 45 * time.Second})
			if i.RXReason != tt.reason {
				t.Fatalf("reason %q, want %q", i.RXReason, tt.reason)
			}
			if (i.RX == nil) != (tt.want == nil) || (i.RX != nil && *i.RX != *tt.want) {
				t.Fatalf("delta %v, want %v", i.RX, tt.want)
			}
			if tt.name == "missing current" && (i.TX == nil || *i.TX != 100) {
				t.Fatal("missing RX lost valid TX")
			}
		})
	}
	i := Calculate(nil, sample(5000), Policy{MaxGap: time.Minute})
	if i.RX != nil || i.RXReason != "baseline" {
		t.Fatal("first sample was counted")
	}
}
func FuzzNoInflatedDeltas(f *testing.F) {
	f.Add(uint64(0), uint64(100))
	f.Add(uint64(math.MaxUint64), uint64(0))
	f.Add(uint64(10), uint64(1))
	f.Fuzz(func(t *testing.T, a, b uint64) {
		before, after := sample(a), sample(b)
		before.Counter.Width = 64
		after.Counter.Width = 64
		after.At = before.At.Add(time.Second)
		i := Calculate(&before, after, Policy{MaxGap: time.Minute})
		if i.RX != nil && *i.RX > 1000 {
			t.Fatalf("unbounded delta %d", *i.RX)
		}
	})
}
