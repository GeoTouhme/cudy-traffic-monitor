package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
func TestDefaults(t *testing.T) {
	c, err := FromEnv(env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if c.PollInterval != 30*time.Second || c.Mode != "mock" || c.Listen != "127.0.0.1:8080" {
		t.Fatalf("unsafe defaults: %s", c)
	}
}
func TestInvalid(t *testing.T) {
	cases := map[string]string{"PROVIDER_MODE": "typo", "POLL_INTERVAL": "0s", "REQUEST_TIMEOUT": "-1s", "MAX_RETRIES": "100", "LOG_LEVEL": "secret-password", "HTTP_LISTEN_ADDRESS": "0.0.0.0:99999", "TIME_ZONE": "Mars/Base", "ROUTER_ADDRESS": "http://admin:secret@router/", "DATABASE_PATH": ":memory:"}
	for k, v := range cases {
		t.Run(k, func(t *testing.T) {
			_, err := FromEnv(env(map[string]string{k: v}))
			if err == nil || strings.Contains(err.Error(), v) {
				t.Fatalf("accepted invalid config or leaked value: %v", err)
			}
		})
	}
	for _, v := range []string{"http://router/api", "ftp://router", "https://router/?auth=secret", "http://router/#token"} {
		if _, err := FromEnv(env(map[string]string{"ROUTER_ADDRESS": v})); err == nil {
			t.Fatal("invalid address accepted")
		}
	}
}
func TestSecretFileAndRedaction(t *testing.T) {
	path := filepath.Join(t.TempDir(), "password")
	if err := os.WriteFile(path, []byte("unique-router-secret\n"), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := FromEnv(env(map[string]string{"ROUTER_PASSWORD_FILE": path, "TIME_ZONE": "Asia/Beirut"}))
	if err != nil || c.RouterPassword != "unique-router-secret" {
		t.Fatal("secret file failed")
	}
	if strings.Contains(fmt.Sprint(c), c.RouterPassword) {
		t.Fatal("config log leaked secret")
	}
	_, err = FromEnv(env(map[string]string{"ROUTER_PASSWORD_FILE": path, "ROUTER_PASSWORD": "secret"}))
	if err == nil {
		t.Fatal("conflicting credential sources accepted")
	}
	_, err = FromEnv(env(map[string]string{"ROUTER_PASSWORD_FILE": path + "missing"}))
	if err == nil || strings.Contains(err.Error(), path) {
		t.Fatal("file error leaked path or accepted missing file")
	}
}
