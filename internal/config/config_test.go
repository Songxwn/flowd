package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadMissingUsesDefaults(t *testing.T) {
	t.Setenv("FLOWD_LISTEN", "")
	t.Setenv("FLOWD_VL_URL", "")
	t.Setenv("FLOWD_IFACES", "")
	t.Setenv("FLOWD_TOKEN", "")
	cfg, err := Load(filepath.Join(t.TempDir(), "missing.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen != "127.0.0.1:8787" {
		t.Fatalf("listen %s", cfg.Listen)
	}
	if cfg.Capture.IdleTimeout != 30*time.Second {
		t.Fatalf("idle %s", cfg.Capture.IdleTimeout)
	}
	if !cfg.Proc.On() {
		t.Fatal("进程关联默认开启")
	}
}

func TestEnvOverridesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("listen: 127.0.0.1:1\nvictorialogs:\n  url: http://127.0.0.1:9\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FLOWD_LISTEN", "127.0.0.1:8787")
	t.Setenv("FLOWD_VL_URL", "http://127.0.0.1:9428")
	t.Setenv("FLOWD_IFACES", "eth0, not a nic")
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen != "127.0.0.1:8787" || cfg.VictoriaLogs.URL != "http://127.0.0.1:9428" {
		t.Fatalf("%+v", cfg)
	}
	if len(cfg.Capture.Ifaces) != 1 || cfg.Capture.Ifaces[0] != "eth0" {
		t.Fatalf("ifaces %#v", cfg.Capture.Ifaces)
	}
}

func TestSaveRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "config.yaml")
	cfg, err := Load(filepath.Join(t.TempDir(), "missing.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	cfg.Capture.Ifaces = []string{"eth0"}
	cfg.Filter.ExcludeDomains = []string{"Ads.Example.com"}
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Capture.IdleTimeout != 30*time.Second {
		t.Fatalf("duration %s", got.Capture.IdleTimeout)
	}
	if len(got.Filter.ExcludeDomains) != 1 || got.Filter.ExcludeDomains[0] != "ads.example.com" {
		t.Fatalf("%#v", got.Filter.ExcludeDomains)
	}
}
