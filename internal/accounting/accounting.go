// Package accounting calculates conservative per-direction deltas without I/O.
package accounting

import (
	"math"
	"time"

	"github.com/GeoTouhme/cudy-traffic-monitor/internal/router/provider"
)

type Sample struct {
	At     time.Time `json:"at"`
	BootID string    `json:"boot_id"`
	provider.Reading
}
type Policy struct{ MaxGap time.Duration }
type Interval struct {
	Start    time.Time `json:"start"`
	End      time.Time `json:"end"`
	RX       *uint64   `json:"rx_bytes"`
	TX       *uint64   `json:"tx_bytes"`
	RXReason string    `json:"rx_reason"`
	TXReason string    `json:"tx_reason"`
	Scope    string    `json:"scope"`
}

// Calculate never extrapolates. Unknown is nil, which persists as SQL NULL.
// A missing direction does not discard a trustworthy delta in the other one.
func Calculate(previous *Sample, current Sample, policy Policy) Interval {
	i := Interval{Start: current.At, End: current.At, Scope: current.Counter.Scope}
	reason := ""
	switch {
	case previous == nil:
		reason = "baseline"
	case !current.At.After(previous.At):
		reason = "clock_discontinuity"
	case policy.MaxGap <= 0 || current.At.Sub(previous.At) > policy.MaxGap:
		reason = "polling_gap"
	case current.BootID == "" || previous.BootID == "":
		reason = "boot_unknown"
	case current.BootID != previous.BootID:
		reason = "router_reboot"
	case current.Counter != previous.Counter || current.DeviceID != previous.DeviceID:
		reason = "counter_changed"
	case current.Counter.Unit != "bytes":
		reason = "unit_unknown"
	case current.Counter.Scope != "simulated" && current.Counter.Scope != "interface" && current.Counter.Scope != "wan":
		reason = "scope_unknown"
	case current.Epoch != previous.Epoch:
		reason = "counter_reset"
	}
	if previous != nil {
		i.Start = previous.At
	}
	if reason != "" {
		i.RXReason, i.TXReason = reason, reason
		return i
	}
	i.RX, i.RXReason = delta(previous.RX, current.RX, *previous, current)
	i.TX, i.TXReason = delta(previous.TX, current.TX, *previous, current)
	return i
}

func delta(prev, cur *uint64, before, after Sample) (*uint64, string) {
	if cur == nil || prev == nil {
		r := after.MissingReason
		if r == "" {
			r = "missing_counter"
		}
		return nil, r
	}
	c := after.Counter
	if c.Width != 32 && c.Width != 64 {
		return nil, "width_unknown"
	}
	maxValue := uint64(math.MaxUint64)
	if c.Width == 32 {
		maxValue = math.MaxUint32
	}
	if *prev > maxValue || *cur > maxValue {
		return nil, "counter_out_of_range"
	}
	if c.MaxBytesPerSecond == 0 {
		return nil, "rate_bound_unknown"
	}
	// Saturate before converting float64 to uint64. A conservative bound is
	// rounded up by one byte to allow sub-second timestamp precision.
	boundFloat := math.Ceil(after.At.Sub(before.At).Seconds() * float64(c.MaxBytesPerSecond))
	if boundFloat >= float64(math.MaxUint64) {
		return nil, "rate_bound_overflow"
	}
	bound := uint64(boundFloat)
	// Even an increasing sample may have wrapped one or more times.
	if c.Width == 32 && bound >= uint64(1)<<32 {
		return nil, "multiple_wraps_possible"
	}
	var d uint64
	why := "monotonic"
	if *cur >= *prev {
		d = *cur - *prev
	} else {
		if !c.Rollover || after.Epoch == "" {
			return nil, "reset_or_rollover"
		}
		// A stable explicit epoch and boot ID are necessary; the physical
		// provider must validate their reset semantics before enabling this.
		d = (maxValue - *prev) + 1 + *cur // decreasing values make this fit uint64
		why = "rollover"
	}
	if d > bound {
		return nil, "implausible_jump"
	}
	return &d, why
}
