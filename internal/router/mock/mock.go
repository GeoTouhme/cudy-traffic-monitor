// Package mock supplies deterministic, explicitly simulated router observations.
package mock

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/GeoTouhme/cudy-traffic-monitor/internal/router/provider"
)

type Step struct {
	Error    provider.Code     `json:"error,omitempty"`
	Snapshot provider.Snapshot `json:"snapshot"`
}
type Fixture struct {
	Steps []Step `json:"steps"`
}

// Load accepts fixtures only, never router captures. The last observation is
// held at its original time when exhausted, so it cannot generate extra usage.
func Load(r io.Reader) (*Provider, error) {
	var f Fixture
	d := json.NewDecoder(io.LimitReader(r, 4<<20))
	d.DisallowUnknownFields()
	if err := d.Decode(&f); err != nil {
		return nil, fmt.Errorf("invalid mock fixture")
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return nil, fmt.Errorf("invalid mock fixture trailing content")
	}
	if len(f.Steps) == 0 {
		return nil, fmt.Errorf("mock fixture requires steps")
	}
	for _, s := range f.Steps {
		if s.Error != "" && s.Error != provider.Unavailable && s.Error != provider.Unauthorized {
			return nil, fmt.Errorf("unsupported fixture error")
		}
		if s.Error == "" {
			if s.Snapshot.At.IsZero() || s.Snapshot.Router.ID != "mock-router" {
				return nil, fmt.Errorf("fixture must identify mock-router and observation time")
			}
			for _, r := range s.Snapshot.Readings {
				if r.Counter.Scope != "simulated" {
					return nil, fmt.Errorf("mock counters must use simulated scope")
				}
			}
		}
	}
	return &Provider{steps: f.Steps}, nil
}

type Provider struct {
	mu         sync.Mutex
	connected  bool
	steps      []Step
	index      uint64
	boot       string
	bootPrefix string
	epoch      string
	rx         uint64
	tx         uint64
	now        func() time.Time
	last       *provider.Snapshot
}

var _ provider.Provider = (*Provider)(nil)

func New() *Provider {
	prefix := fmt.Sprintf("mock-instance-%d", time.Now().UnixNano())
	return &Provider{now: time.Now, boot: prefix + "-boot-1", bootPrefix: prefix, epoch: "epoch-1", rx: 1000, tx: 2000}
}
func (*Provider) Capabilities() provider.Capabilities {
	s := provider.Support{Supported: true, Evidence: "VERIFIED: deterministic mock tests; simulated data only"}
	u := provider.Support{Evidence: "Mock counters are simulated, never real WAN measurements"}
	return provider.Capabilities{Mode: "mock", Simulated: true, Authentication: s, Identification: s, Devices: s, Counters: s, Health: s, Recovery: s, WANOnly: u}
}
func (p *Provider) Connect(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	p.connected = true
	return nil
}
func (p *Provider) Recover(ctx context.Context) error { return p.Connect(ctx) }
func (p *Provider) Close() error                      { p.mu.Lock(); defer p.mu.Unlock(); p.connected = false; return nil }
func (p *Provider) check(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !p.connected {
		return provider.NewError(provider.Unauthorized, "mock_session")
	}
	return nil
}
func (p *Provider) Health(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.check(ctx)
}
func (p *Provider) info() provider.Info {
	if view := p.view(); view != nil {
		return view.Router
	}
	return provider.Info{ID: "mock-router", Model: "simulated-router", Firmware: "mock-v1", BootID: p.boot}
}
func (p *Provider) view() *provider.Snapshot {
	if p.last != nil {
		return p.last
	}
	for i := range p.steps {
		if p.steps[i].Error == "" {
			return &p.steps[i].Snapshot
		}
	}
	return nil
}
func (p *Provider) remember(s provider.Snapshot) {
	copy := s
	copy.Devices = append([]provider.Device{}, s.Devices...)
	copy.Readings = append([]provider.Reading{}, s.Readings...)
	p.last = &copy
}
func (p *Provider) Identify(ctx context.Context) (provider.Info, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.info(), p.check(ctx)
}
func (p *Provider) Devices(ctx context.Context) ([]provider.Device, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if view := p.view(); view != nil {
		return append([]provider.Device{}, view.Devices...), p.check(ctx)
	}
	return []provider.Device{{ID: "mock-device-1", Name: "Simulated laptop"}}, p.check(ctx)
}
func counter() provider.Counter {
	return provider.Counter{ID: "device-total", Scope: "simulated", Unit: "bytes", Width: 32, Rollover: true, MaxBytesPerSecond: 1_000_000}
}
func (p *Provider) Counters(ctx context.Context) ([]provider.Counter, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if view := p.view(); view != nil {
		result := []provider.Counter{}
		seen := map[string]bool{}
		for _, r := range view.Readings {
			if !seen[r.Counter.ID] {
				result = append(result, r.Counter)
				seen[r.Counter.ID] = true
			}
		}
		return result, p.check(ctx)
	}
	return []provider.Counter{counter()}, p.check(ctx)
}
func (p *Provider) Read(ctx context.Context) (provider.Snapshot, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.check(ctx); err != nil {
		return provider.Snapshot{}, err
	}
	if len(p.steps) > 0 {
		i := min(p.index, uint64(len(p.steps)-1))
		p.index++
		s := p.steps[i]
		if s.Error != "" {
			if s.Error == provider.Unauthorized {
				p.connected = false
			}
			return provider.Snapshot{}, provider.NewError(s.Error, "mock_read")
		}
		// Deep copy so callers cannot mutate deterministic fixture state.
		b, _ := json.Marshal(s.Snapshot)
		var copy provider.Snapshot
		_ = json.Unmarshal(b, &copy)
		p.remember(copy)
		return copy, nil
	}
	// Bounded deterministic event cycle. Values depend on the successful read
	// sequence; timestamps use the local clock for a useful running demo.
	n := p.index % 16
	p.index++
	if n == 3 {
		return provider.Snapshot{}, provider.NewError(provider.Unavailable, "mock_read")
	}
	if n == 5 {
		p.connected = false
		return provider.Snapshot{}, provider.NewError(provider.Unauthorized, "mock_read")
	}
	if n == 7 {
		p.epoch = fmt.Sprintf("reset-%d", p.index)
		p.rx, p.tx = 10, 20
	}
	if n == 9 {
		p.boot = fmt.Sprintf("%s-boot-%d", p.bootPrefix, p.index)
		p.epoch = "epoch-1"
		p.rx, p.tx = 100, 200
	}
	if n == 12 {
		p.rx, p.tx = (1<<32)-500, (1<<32)-1000
	}
	if n != 12 {
		p.rx = (p.rx + 1000) % (1 << 32)
		p.tx = (p.tx + 2000) % (1 << 32)
	}
	s := provider.Snapshot{At: p.now().UTC(), Router: provider.Info{ID: "mock-router", Model: "simulated-router", Firmware: "mock-v1", BootID: p.boot}, Devices: []provider.Device{{ID: "mock-device-1", Name: "Simulated laptop"}}, Readings: []provider.Reading{}}
	if n == 10 {
		s.Devices = []provider.Device{}
		p.remember(s)
		return s, nil
	}
	rx, tx := p.rx, p.tx
	r := provider.Reading{DeviceID: "mock-device-1", Counter: counter(), Epoch: p.epoch, RX: &rx, TX: &tx}
	if n == 11 {
		r.RX = nil
	}
	s.Readings = append(s.Readings, r)
	p.remember(s)
	return s, nil
}
