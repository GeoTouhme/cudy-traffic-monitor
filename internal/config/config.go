// Package config validates environment configuration without echoing secrets.
package config

import (
	"errors"
	"io"
	"log/slog"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata"
)

type Config struct {
	Mode           string
	RouterAddress  string `json:"-"`
	RouterUsername string `json:"-"`
	RouterPassword string `json:"-"`
	PollInterval   time.Duration
	RequestTimeout time.Duration
	Retries        int
	Database       string
	LogLevel       slog.Level
	Listen         string
	TimeZone       string
}

// String prevents accidental fmt/log output of configuration credentials.
func (Config) String() string     { return "monitor configuration (credentials redacted)" }
func (c Config) GoString() string { return c.String() }
func Load() (Config, error)       { return FromEnv(os.Getenv) }
func FromEnv(get func(string) string) (Config, error) {
	value := func(k, def string) string {
		if v := get(k); v != "" {
			return v
		}
		return def
	}
	c := Config{Mode: value("PROVIDER_MODE", "mock"), RouterAddress: get("ROUTER_ADDRESS"), RouterUsername: get("ROUTER_USERNAME"), RouterPassword: get("ROUTER_PASSWORD"), Database: value("DATABASE_PATH", "data/monitor.db"), Listen: value("HTTP_LISTEN_ADDRESS", "127.0.0.1:8080"), TimeZone: value("TIME_ZONE", "UTC")}
	fail := func(k string) (Config, error) { return Config{}, errors.New("invalid configuration: " + k) }
	if c.Mode != "mock" && c.Mode != "cudy" {
		return fail("PROVIDER_MODE")
	}
	var err error
	c.PollInterval, err = time.ParseDuration(value("POLL_INTERVAL", "30s"))
	if err != nil || c.PollInterval < time.Second || c.PollInterval > 24*time.Hour {
		return fail("POLL_INTERVAL")
	}
	c.RequestTimeout, err = time.ParseDuration(value("REQUEST_TIMEOUT", "5s"))
	if err != nil || c.RequestTimeout < 100*time.Millisecond || c.RequestTimeout > time.Minute {
		return fail("REQUEST_TIMEOUT")
	}
	c.Retries, err = strconv.Atoi(value("MAX_RETRIES", "2"))
	if err != nil || c.Retries < 0 || c.Retries > 5 {
		return fail("MAX_RETRIES")
	}
	if err = c.LogLevel.UnmarshalText([]byte(value("LOG_LEVEL", "info"))); err != nil {
		return fail("LOG_LEVEL")
	}
	_, port, err := net.SplitHostPort(c.Listen)
	if err != nil {
		return fail("HTTP_LISTEN_ADDRESS")
	}
	p, err := strconv.Atoi(port)
	if err != nil || p < 1 || p > 65535 {
		return fail("HTTP_LISTEN_ADDRESS")
	}
	if _, err = time.LoadLocation(c.TimeZone); err != nil {
		return fail("TIME_ZONE")
	}
	if c.Database == ":memory:" || strings.ContainsAny(c.Database, "\x00\r\n") {
		return fail("DATABASE_PATH")
	}
	if c.RouterAddress != "" {
		u, e := url.Parse(c.RouterAddress)
		if e != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
			return fail("ROUTER_ADDRESS")
		}
	}
	if file := get("ROUTER_PASSWORD_FILE"); file != "" {
		if c.RouterPassword != "" {
			return fail("ROUTER_PASSWORD / ROUTER_PASSWORD_FILE conflict")
		}
		f, e := os.Open(file)
		if e != nil {
			return fail("ROUTER_PASSWORD_FILE")
		}
		b, e := io.ReadAll(io.LimitReader(f, 65537))
		closeErr := f.Close()
		if e != nil || closeErr != nil || len(b) > 65536 {
			return fail("ROUTER_PASSWORD_FILE")
		}
		c.RouterPassword = strings.TrimRight(string(b), "\r\n")
		if c.RouterPassword == "" {
			return fail("ROUTER_PASSWORD_FILE")
		}
	}
	return c, nil
}
