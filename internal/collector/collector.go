// Package collector runs a single bounded polling loop and exposes safe status.
package collector

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/GeoTouhme/cudy-traffic-monitor/internal/accounting"
	"github.com/GeoTouhme/cudy-traffic-monitor/internal/router/provider"
	"github.com/GeoTouhme/cudy-traffic-monitor/internal/storage"
)

type Sink interface {
	Record(context.Context, provider.Snapshot, accounting.Policy) error
	RecordGap(context.Context, time.Time, provider.Code, accounting.Policy) error
}
type Config struct {
	Interval, RequestTimeout, InitialBackoff, MaxBackoff time.Duration
	Retries                                              int
}
type Status struct {
	Running             bool          `json:"running"`
	Mode                string        `json:"mode"`
	Simulated           bool          `json:"simulated"`
	LastAttempt         *time.Time    `json:"last_attempt"`
	LastSuccess         *time.Time    `json:"last_success"`
	LastError           provider.Code `json:"last_error,omitempty"`
	ConsecutiveFailures uint64        `json:"consecutive_failures"`
	Collections         uint64        `json:"collections"`
	Samples             uint64        `json:"samples"`
}
type Collector struct {
	provider       provider.Provider
	sink           Sink
	cfg            Config
	log            *slog.Logger
	mu             sync.RWMutex
	status         Status
	poll           sync.Mutex
	connected      bool
	recoverSession bool
}

func New(p provider.Provider, s Sink, cfg Config, log *slog.Logger) (*Collector, error) {
	if p == nil || s == nil || log == nil || cfg.Interval <= 0 || cfg.RequestTimeout <= 0 || cfg.Retries < 0 || cfg.Retries > 5 || cfg.InitialBackoff <= 0 || cfg.MaxBackoff < cfg.InitialBackoff || cfg.MaxBackoff > time.Minute {
		return nil, errors.New("invalid collector configuration")
	}
	caps := p.Capabilities()
	return &Collector{provider: p, sink: s, cfg: cfg, log: log, status: Status{Mode: caps.Mode, Simulated: caps.Simulated}}, nil
}
func (c *Collector) Status() Status                      { c.mu.RLock(); defer c.mu.RUnlock(); return c.status }
func (c *Collector) Capabilities() provider.Capabilities { return c.provider.Capabilities() }

func wait(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
func retryable(err error) bool {
	switch provider.CodeOf(err) {
	case provider.Unauthorized, provider.Unavailable, provider.Timeout:
		return true
	}
	return false
}
func (c *Collector) attempt(ctx context.Context) (provider.Snapshot, error) {
	request, cancel := context.WithTimeout(ctx, c.cfg.RequestTimeout)
	defer cancel()
	if !c.connected {
		var err error
		if c.recoverSession {
			err = c.provider.Recover(request)
		} else {
			err = c.provider.Connect(request)
		}
		if err != nil {
			return provider.Snapshot{}, err
		}
		c.connected = true
		c.recoverSession = false
	}
	snap, err := c.provider.Read(request)
	if provider.CodeOf(err) == provider.Unauthorized && err != nil {
		c.connected = false
		c.recoverSession = true
	}
	return snap, err
}
func (c *Collector) Poll(ctx context.Context) error {
	c.poll.Lock()
	defer c.poll.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	at := time.Now().UTC()
	c.mu.Lock()
	c.status.LastAttempt = &at
	c.mu.Unlock()
	var snap provider.Snapshot
	var err error
	delay := c.cfg.InitialBackoff
	for attempt := 0; attempt <= c.cfg.Retries; attempt++ {
		snap, err = c.attempt(ctx)
		if err == nil || !retryable(err) || attempt == c.cfg.Retries {
			break
		}
		if err = wait(ctx, delay); err != nil {
			break
		}
		delay = min(delay*2, c.cfg.MaxBackoff)
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	policy := accounting.Policy{MaxGap: c.cfg.Interval + c.cfg.Interval/2}
	persist, cancel := context.WithTimeout(ctx, c.cfg.RequestTimeout)
	defer cancel()
	if err == nil {
		err = c.sink.Record(persist, snap, policy)
	}
	if errors.Is(err, storage.ErrStale) {
		// Replayed fixture or backwards clock: no new usage. Report stale data
		// and invalidate continuity at wall time when it moves forward again.
		err = provider.NewError(provider.InvalidData, "stale_snapshot")
	}
	if err != nil {
		code := provider.CodeOf(err)
		gapCtx, gapCancel := context.WithTimeout(ctx, c.cfg.RequestTimeout)
		gapErr := c.sink.RecordGap(gapCtx, time.Now().UTC(), code, policy)
		gapCancel()
		if gapErr != nil {
			code = provider.Internal
		}
		c.mu.Lock()
		c.status.LastError = code
		c.status.ConsecutiveFailures++
		c.mu.Unlock()
		c.log.Warn("collection failed", "code", code)
		// Return the same safe code that appears in status, never raw errors.
		return provider.NewError(code, "collection")
	}
	success := time.Now().UTC()
	c.mu.Lock()
	c.status.LastSuccess = &success
	c.status.LastError = ""
	c.status.ConsecutiveFailures = 0
	c.status.Collections++
	c.status.Samples += uint64(len(snap.Readings))
	c.mu.Unlock()
	c.log.Debug("collection persisted", "readings", len(snap.Readings), "simulated", c.provider.Capabilities().Simulated)
	return nil
}

// Run blocks until cancellation. Polls never overlap, and missed deadlines do
// not create a burst of catch-up requests. The caller closes the provider.
func (c *Collector) Run(ctx context.Context) error {
	c.mu.Lock()
	if c.status.Running {
		c.mu.Unlock()
		return errors.New("collector already running")
	}
	c.status.Running = true
	c.mu.Unlock()
	defer func() { c.mu.Lock(); c.status.Running = false; c.mu.Unlock() }()
	for {
		_ = c.Poll(ctx)
		if err := wait(ctx, c.cfg.Interval); err != nil {
			return err
		}
	}
}
