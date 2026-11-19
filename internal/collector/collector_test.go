package collector

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/GeoTouhme/cudy-traffic-monitor/internal/accounting"
	"github.com/GeoTouhme/cudy-traffic-monitor/internal/router/cudy"
	"github.com/GeoTouhme/cudy-traffic-monitor/internal/router/mock"
	"github.com/GeoTouhme/cudy-traffic-monitor/internal/router/provider"
)

type memorySink struct {
	mu            sync.Mutex
	records, gaps int
	fail          bool
	recorded      chan struct{}
	once          sync.Once
}

func (s *memorySink) Record(context.Context, provider.Snapshot, accounting.Policy) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail {
		return errors.New("password=must-not-leak")
	}
	s.records++
	if s.recorded != nil {
		s.once.Do(func() { close(s.recorded) })
	}
	return nil
}
func (s *memorySink) RecordGap(context.Context, time.Time, provider.Code, accounting.Policy) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gaps++
	return nil
}
func testConfig() Config {
	return Config{Interval: 30 * time.Second, RequestTimeout: time.Second, Retries: 2, InitialBackoff: time.Millisecond, MaxBackoff: 4 * time.Millisecond}
}
func newCollector(t *testing.T, p provider.Provider, s *memorySink, cfg Config) *Collector {
	t.Helper()
	c, err := New(p, s, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func TestAuthExpirationRecovery(t *testing.T) {
	p, err := mock.Load(strings.NewReader(`{"steps":[{"error":"unauthorized"},{"snapshot":{"at":"2026-01-01T00:00:00Z","router":{"id":"mock-router"},"devices":[],"readings":[]}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	s := &memorySink{}
	c := newCollector(t, p, s, testConfig())
	if err = c.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if s.records != 1 || s.gaps != 0 || c.Status().Collections != 1 || c.Status().LastError != "" {
		t.Fatalf("session recovery failed: %+v", c.Status())
	}
}

type stubProvider struct {
	provider.Provider
	read  func(context.Context) (provider.Snapshot, error)
	calls int
}

func (p *stubProvider) Capabilities() provider.Capabilities { return mock.New().Capabilities() }
func (p *stubProvider) Connect(ctx context.Context) error   { return ctx.Err() }
func (p *stubProvider) Read(ctx context.Context) (provider.Snapshot, error) {
	p.calls++
	return p.read(ctx)
}
func TestBoundedRetryAndTimeout(t *testing.T) {
	p := &stubProvider{read: func(context.Context) (provider.Snapshot, error) {
		return provider.Snapshot{}, provider.NewError(provider.Unavailable, "read")
	}}
	s := &memorySink{}
	c := newCollector(t, p, s, testConfig())
	if err := c.Poll(context.Background()); provider.CodeOf(err) != provider.Unavailable {
		t.Fatalf("failure code: %v", err)
	}
	if p.calls != 3 || s.gaps != 1 || c.Status().ConsecutiveFailures != 1 {
		t.Fatalf("retry bound: %d %+v", p.calls, c.Status())
	}
	cfg := testConfig()
	cfg.Retries = 0
	cfg.RequestTimeout = 10 * time.Millisecond
	p = &stubProvider{read: func(ctx context.Context) (provider.Snapshot, error) {
		<-ctx.Done()
		return provider.Snapshot{}, ctx.Err()
	}}
	c = newCollector(t, p, &memorySink{}, cfg)
	if err := c.Poll(context.Background()); provider.CodeOf(err) != provider.Timeout {
		t.Fatalf("timeout: %v", err)
	}
}
func TestRunCancellationAndStatus(t *testing.T) {
	s := &memorySink{recorded: make(chan struct{})}
	c := newCollector(t, mock.New(), s, testConfig())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx) }()
	select {
	case <-s.recorded:
	case <-time.After(3 * time.Second):
		t.Fatal("no initial poll")
	}
	if !c.Status().Running {
		t.Fatal("collector not running")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("shutdown blocked")
	}
	if c.Status().Running {
		t.Fatal("stale running status")
	}
}
func TestCancellationDuringRequestAndBackoff(t *testing.T) {
	for _, backoff := range []bool{false, true} {
		t.Run(map[bool]string{false: "request", true: "backoff"}[backoff], func(t *testing.T) {
			started := make(chan struct{})
			p := &stubProvider{read: func(ctx context.Context) (provider.Snapshot, error) {
				close(started)
				if backoff {
					return provider.Snapshot{}, provider.NewError(provider.Unavailable, "read")
				}
				<-ctx.Done()
				return provider.Snapshot{}, ctx.Err()
			}}
			cfg := testConfig()
			cfg.InitialBackoff = time.Minute
			cfg.MaxBackoff = time.Minute
			c := newCollector(t, p, &memorySink{}, cfg)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- c.Poll(ctx) }()
			<-started
			cancel()
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
			case <-time.After(time.Second):
				t.Fatal("cancellation blocked")
			}
		})
	}
}
func TestUnsupportedAndStorageFailure(t *testing.T) {
	c := newCollector(t, cudy.New(), &memorySink{}, testConfig())
	if err := c.Poll(context.Background()); provider.CodeOf(err) != provider.Unvalidated {
		t.Fatal(err)
	}
	if c.Capabilities().Counters.Supported {
		t.Fatal("unvalidated capability advertised")
	}
	c = newCollector(t, mock.New(), &memorySink{fail: true}, testConfig())
	if err := c.Poll(context.Background()); provider.CodeOf(err) != provider.Internal || strings.Contains(err.Error(), "must-not-leak") {
		t.Fatalf("raw error leak: %v", err)
	}
}
