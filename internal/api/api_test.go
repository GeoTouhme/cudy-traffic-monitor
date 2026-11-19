package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/GeoTouhme/cudy-traffic-monitor/internal/collector"
	"github.com/GeoTouhme/cudy-traffic-monitor/internal/router/mock"
	"github.com/GeoTouhme/cudy-traffic-monitor/internal/storage"
)

func TestEndpointsAndErrors(t *testing.T) {
	s, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	c, err := collector.New(mock.New(), s, collector.Config{Interval: 30 * time.Second, RequestTimeout: time.Second, InitialBackoff: time.Millisecond, MaxBackoff: time.Millisecond}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if err = c.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	h := New(c, s, "Asia/Beirut")
	for _, path := range []string{"/healthz", "/api/v1/status", "/api/v1/capabilities", "/api/v1/devices", "/api/v1/usage"} {
		t.Run(path, func(t *testing.T) {
			r := httptest.NewRecorder()
			h.ServeHTTP(r, httptest.NewRequest(http.MethodGet, path, nil))
			if r.Code != 200 || !strings.Contains(r.Header().Get("Content-Type"), "application/json") {
				t.Fatalf("bad response: %d %s", r.Code, r.Body.String())
			}
			var body map[string]json.RawMessage
			if err := json.Unmarshal(r.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if string(body["error"]) != "null" || len(body["data"]) == 0 {
				t.Fatalf("invalid envelope: %s", r.Body.String())
			}
			if strings.Contains(r.Body.String(), "password") || strings.Contains(r.Body.String(), "auth_token") {
				t.Fatal("secret field exposed")
			}
			if path == "/api/v1/usage" && !strings.Contains(r.Body.String(), `"known_rx_bytes":null`) {
				t.Fatal("baseline reported zero usage")
			}
		})
	}
	for _, tc := range []struct {
		method, path string
		code         int
	}{{"POST", "/api/v1/devices", 405}, {"GET", "/rpc", 404}, {"GET", "/api/v1/nope", 404}} {
		r := httptest.NewRecorder()
		h.ServeHTTP(r, httptest.NewRequest(tc.method, tc.path, nil))
		if r.Code != tc.code {
			t.Fatalf("%s %s = %d", tc.method, tc.path, r.Code)
		}
	}
	s.Close()
	r := httptest.NewRecorder()
	h.ServeHTTP(r, httptest.NewRequest("GET", "/healthz", nil))
	if r.Code != 503 {
		t.Fatal("unhealthy storage returned success")
	}
}
