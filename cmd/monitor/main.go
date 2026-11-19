package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/GeoTouhme/cudy-traffic-monitor/internal/api"
	"github.com/GeoTouhme/cudy-traffic-monitor/internal/collector"
	"github.com/GeoTouhme/cudy-traffic-monitor/internal/config"
	"github.com/GeoTouhme/cudy-traffic-monitor/internal/router/cudy"
	"github.com/GeoTouhme/cudy-traffic-monitor/internal/router/mock"
	"github.com/GeoTouhme/cudy-traffic-monitor/internal/router/provider"
	"github.com/GeoTouhme/cudy-traffic-monitor/internal/storage"
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "healthcheck" {
		if err := healthcheck(); err != nil {
			os.Exit(1)
		}
		return
	}
	if len(os.Args) > 1 {
		fmt.Fprintln(os.Stderr, "usage: monitor [healthcheck]")
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run(ctx context.Context) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))
	var p provider.Provider = mock.New()
	if cfg.Mode == "cudy" {
		p = cudy.New()
	}
	defer p.Close()
	initCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	db, err := storage.Open(initCtx, cfg.Database)
	if err == nil {
		err = db.SaveSettings(initCtx, storage.Settings{Provider: cfg.Mode, PollInterval: cfg.PollInterval, TimeZone: cfg.TimeZone})
	}
	cancel()
	if db != nil {
		defer db.Close()
	}
	if err != nil {
		return errors.New("database initialization failed")
	}
	c, err := collector.New(p, db, collector.Config{Interval: cfg.PollInterval, RequestTimeout: cfg.RequestTimeout, Retries: cfg.Retries, InitialBackoff: 250 * time.Millisecond, MaxBackoff: 2 * time.Second}, log)
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		return errors.New("HTTP listener failed")
	}
	server := &http.Server{Handler: api.New(c, db, cfg.TimeZone), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10, BaseContext: func(net.Listener) context.Context { return ctx }}
	runCtx, runCancel := context.WithCancel(ctx)
	defer runCancel()
	done := make(chan error, 1)
	go func() { done <- c.Run(runCtx) }()
	serverDone := make(chan error, 1)
	go func() { serverDone <- server.Serve(listener) }()
	log.Info("monitor started", "listen", listener.Addr().String(), "mode", cfg.Mode, "simulated", p.Capabilities().Simulated)
	if cfg.Mode == "cudy" {
		log.Warn("Cudy provider disabled pending WR1200 V2.1 validation", "code", provider.Unvalidated)
	}
	var serveErr error
	select {
	case <-ctx.Done():
	case serveErr = <-serverDone:
	}
	runCancel()
	shutdown, cancelShutdown := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelShutdown()
	if err = server.Shutdown(shutdown); err != nil {
		_ = server.Close()
	}
	<-done
	log.Info("monitor stopped")
	if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
		return errors.New("HTTP server failed")
	}
	return nil
}
func healthcheck() error {
	listen := os.Getenv("HTTP_LISTEN_ADDRESS")
	if listen == "" {
		listen = "127.0.0.1:8080"
	}
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return err
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	client := http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	defer client.CloseIdleConnections()
	resp, err := client.Get("http://" + net.JoinHostPort(host, port) + "/healthz")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode != 200 {
		return errors.New("unhealthy")
	}
	return nil
}
