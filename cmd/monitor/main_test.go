package main

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/GeoTouhme/cudy-traffic-monitor/internal/storage"
)

func TestApplicationHTTPAndGracefulShutdown(t *testing.T) {
	// Reserve a free loopback port, then let the real application bind it.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := l.Addr().String()
	l.Close()
	path := filepath.Join(t.TempDir(), "monitor.db")
	for k, v := range map[string]string{"PROVIDER_MODE": "mock", "HTTP_LISTEN_ADDRESS": address, "DATABASE_PATH": path, "POLL_INTERVAL": "1s", "ROUTER_PASSWORD": "", "ROUTER_PASSWORD_FILE": "", "TIME_ZONE": "UTC"} {
		t.Setenv(k, v)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- run(ctx); close(done) }()
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
		}
	}()
	client := &http.Client{Timeout: 500 * time.Millisecond, Transport: &http.Transport{Proxy: nil}}
	defer client.CloseIdleConnections()
	deadline := time.Now().Add(5 * time.Second)
	for {
		select {
		case err := <-done:
			t.Fatalf("application exited before collecting: %v", err)
		default:
		}
		resp, err := client.Get("http://" + address + "/api/v1/usage")
		if err == nil {
			var body struct {
				Data struct {
					Usage []storage.Usage `json:"usage"`
				} `json:"data"`
			}
			decodeErr := json.NewDecoder(resp.Body).Decode(&body)
			resp.Body.Close()
			if decodeErr == nil && len(body.Data.Usage) > 0 && body.Data.Usage[0].RX != nil {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("application did not collect and serve usage")
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	select {
	case err = <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("graceful shutdown blocked")
	}
	// The application has closed its DB; persisted observations survive reopen.
	s, err := storage.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	u, err := s.Usage(context.Background())
	if err != nil || len(u) != 1 || u[0].RX == nil || *u[0].RX != "1000" {
		t.Fatalf("application persistence: %+v %v", u, err)
	}
}
