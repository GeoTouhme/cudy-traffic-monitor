// Package cudy gates hardware use until the target firmware has been validated.
// It intentionally has no HTTP client, invented RPC contract or login fallback.
package cudy

import (
	"context"

	"github.com/GeoTouhme/cudy-traffic-monitor/internal/router/provider"
)

type Provider struct{}

var _ provider.Provider = (*Provider)(nil)

func New() *Provider { return &Provider{} }
func (*Provider) Capabilities() provider.Capabilities {
	u := provider.Support{Evidence: "UNKNOWN: WR1200 V2.1 stock firmware requires real-device validation; see docs/cudy-api-research.md"}
	return provider.Capabilities{Mode: "cudy", Authentication: u, Identification: u, Devices: u, Counters: u, WANOnly: u, Health: u, Recovery: u}
}
func blocked(ctx context.Context, op string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return provider.NewError(provider.Unvalidated, op)
}
func (*Provider) Connect(ctx context.Context) error { return blocked(ctx, "connect") }
func (*Provider) Identify(ctx context.Context) (provider.Info, error) {
	return provider.Info{}, blocked(ctx, "identify")
}
func (*Provider) Devices(ctx context.Context) ([]provider.Device, error) {
	return nil, blocked(ctx, "devices")
}
func (*Provider) Counters(ctx context.Context) ([]provider.Counter, error) {
	return nil, blocked(ctx, "counters")
}
func (*Provider) Read(ctx context.Context) (provider.Snapshot, error) {
	return provider.Snapshot{}, blocked(ctx, "read")
}
func (*Provider) Health(ctx context.Context) error  { return blocked(ctx, "health") }
func (*Provider) Recover(ctx context.Context) error { return blocked(ctx, "recover") }
func (*Provider) Close() error                      { return nil }
