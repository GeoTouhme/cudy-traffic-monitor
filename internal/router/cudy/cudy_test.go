package cudy

import (
	"context"
	"errors"
	"testing"

	"github.com/GeoTouhme/cudy-traffic-monitor/internal/router/provider"
)

func TestUnvalidatedProviderMakesNoNetworkRequests(t *testing.T) {
	p := New()
	ctx := context.Background()
	if p.Capabilities().Authentication.Supported || p.Capabilities().Counters.Supported || p.Capabilities().WANOnly.Supported {
		t.Fatal("unsupported advertised")
	}
	_, identify := p.Identify(ctx)
	_, devices := p.Devices(ctx)
	_, counters := p.Counters(ctx)
	_, read := p.Read(ctx)
	for _, err := range []error{p.Connect(ctx), identify, devices, counters, read, p.Health(ctx), p.Recover(ctx)} {
		if provider.CodeOf(err) != provider.Unvalidated {
			t.Fatalf("missing typed validation blocker: %v", err)
		}
	}
	cancelCtx, cancel := context.WithCancel(ctx)
	cancel()
	if !errors.Is(p.Connect(cancelCtx), context.Canceled) {
		t.Fatal("context ignored")
	}
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
}
