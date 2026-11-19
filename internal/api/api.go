// Package api exposes GET-only local status and stored observations, never RPC.
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/GeoTouhme/cudy-traffic-monitor/internal/collector"
	"github.com/GeoTouhme/cudy-traffic-monitor/internal/router/provider"
	"github.com/GeoTouhme/cudy-traffic-monitor/internal/storage"
)

type StatusSource interface {
	Status() collector.Status
	Capabilities() provider.Capabilities
}
type Queries interface {
	Ping(context.Context) error
	Devices(context.Context) ([]storage.Device, error)
	Usage(context.Context) ([]storage.Usage, error)
}
type envelope struct {
	Data  any       `json:"data"`
	Error *apiError `json:"error"`
}
type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func write(w http.ResponseWriter, status int, data any, err *apiError) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(envelope{Data: data, Error: err})
}
func New(c StatusSource, s Queries, zone string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		r = r.WithContext(ctx)
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			write(w, 405, nil, &apiError{"method_not_allowed", "Only GET is supported"})
			return
		}
		switch r.URL.Path {
		case "/healthz":
			if err := s.Ping(r.Context()); err != nil {
				write(w, 503, nil, &apiError{"storage_unavailable", "Storage is unavailable"})
				return
			}
			write(w, 200, map[string]string{"status": "ok"}, nil) // Liveness, not proof of router reachability.
		case "/api/v1/status":
			write(w, 200, c.Status(), nil)
		case "/api/v1/capabilities":
			write(w, 200, c.Capabilities(), nil)
		case "/api/v1/devices":
			d, err := s.Devices(r.Context())
			if err != nil {
				write(w, 503, nil, &apiError{"storage_unavailable", "Device data is unavailable"})
				return
			}
			write(w, 200, d, nil)
		case "/api/v1/usage":
			u, err := s.Usage(r.Context())
			if err != nil {
				write(w, 503, nil, &apiError{"storage_unavailable", "Usage data is unavailable"})
				return
			}
			write(w, 200, map[string]any{"period": "all_time", "timestamp_zone": "UTC", "display_time_zone": zone, "usage": u, "limitation": "Known subtotals exclude unknown intervals. Scope is per counter; simulated data is not measured WAN usage."}, nil)
		default:
			write(w, 404, nil, &apiError{"not_found", "Endpoint not found"})
		}
	})
}
