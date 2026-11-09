package config

import (
	"net"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Listen       string  `yaml:"listen"`
	Token        string  `yaml:"token,omitempty"`
	Hostname     string  `yaml:"hostname,omitempty"`
	VictoriaLogs VL      `yaml:"victorialogs"`
	Capture      Capture `yaml:"capture"`
	Proc         Proc    `yaml:"proc"`
	Filter       Filter  `yaml:"filter"`
}

type VL struct {
	URL string `yaml:"url"`
}

type Capture struct {
	Ifaces         []string      `yaml:"ifaces"`
	IdleTimeout    time.Duration `yaml:"idle_timeout"`
	ExportInterval time.Duration `yaml:"export_interval"`
}

type Proc struct {
	Enabled  *bool         `yaml:"enabled"`
	Root     string        `yaml:"root"`
	Interval time.Duration `yaml:"interval"`
}

func (p Proc) On() bool {
	return p.Enabled == nil || *p.Enabled
}

type Filter struct {
	IncludeDomains []string `yaml:"include_domains"`
	ExcludeDomains []string `yaml:"exclude_domains"`
	IncludeApps    []string `yaml:"include_apps"`
	ExcludeApps    []string `yaml:"exclude_apps"`
}

var validIface = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,15}$`)

func Load(path string) (Config, error) {
	var cfg Config
	b, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return Config{}, err
	}
	if err == nil && len(strings.TrimSpace(string(b))) > 0 {
		if err := yaml.Unmarshal(b, &cfg); err != nil {
			return Config{}, err
		}
	}
	cfg.applyEnv()
	cfg.Normalize()
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func Save(path string, cfg Config) error {
	cfg.Normalize()
	if err := cfg.Validate(); err != nil {
		return err
	}
	b, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	dir := strings.TrimSpace(path)
	if i := strings.LastIndexAny(dir, `/\`); i >= 0 {
		if err := os.MkdirAll(dir[:i], 0o755); err != nil {
			return err
		}
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (c *Config) applyEnv() {
	if v := strings.TrimSpace(os.Getenv("FLOWD_LISTEN")); v != "" {
		c.Listen = v
	}
	if v := strings.TrimSpace(os.Getenv("FLOWD_VL_URL")); v != "" {
		c.VictoriaLogs.URL = v
	}
	if v := strings.TrimSpace(os.Getenv("FLOWD_IFACES")); v != "" {
		c.Capture.Ifaces = splitList(v)
	}
	if v := strings.TrimSpace(os.Getenv("FLOWD_PROC_ROOT")); v != "" {
		c.Proc.Root = v
	}
	if v := os.Getenv("FLOWD_TOKEN"); v != "" {
		c.Token = v
	}
	if v := strings.TrimSpace(os.Getenv("FLOWD_HOSTNAME")); v != "" {
		c.Hostname = v
	}
}

func (c *Config) Normalize() {
	if strings.TrimSpace(c.Listen) == "" {
		c.Listen = "127.0.0.1:8787"
	}
	c.Listen = strings.TrimSpace(c.Listen)
	if strings.TrimSpace(c.VictoriaLogs.URL) == "" {
		c.VictoriaLogs.URL = "http://127.0.0.1:9428"
	}
	c.VictoriaLogs.URL = strings.TrimRight(strings.TrimSpace(c.VictoriaLogs.URL), "/")
	if c.Capture.IdleTimeout <= 0 {
		c.Capture.IdleTimeout = 30 * time.Second
	}
	if c.Capture.IdleTimeout < 5*time.Second {
		c.Capture.IdleTimeout = 5 * time.Second
	}
	if c.Capture.IdleTimeout > 10*time.Minute {
		c.Capture.IdleTimeout = 10 * time.Minute
	}
	if c.Capture.ExportInterval <= 0 {
		c.Capture.ExportInterval = 15 * time.Second
	}
	if c.Capture.ExportInterval < 5*time.Second {
		c.Capture.ExportInterval = 5 * time.Second
	}
	if c.Capture.ExportInterval > 5*time.Minute {
		c.Capture.ExportInterval = 5 * time.Minute
	}
	if strings.TrimSpace(c.Proc.Root) == "" {
		c.Proc.Root = "/proc"
	}
	if c.Proc.Interval <= 0 {
		c.Proc.Interval = 2 * time.Second
	}
	if c.Proc.Interval < time.Second {
		c.Proc.Interval = time.Second
	}
	if c.Proc.Interval > time.Minute {
		c.Proc.Interval = time.Minute
	}
	if c.Proc.Enabled == nil {
		on := true
		c.Proc.Enabled = &on
	}
	c.Capture.Ifaces = CleanIfaces(c.Capture.Ifaces)
	c.Filter.IncludeDomains = CleanDomains(c.Filter.IncludeDomains)
	c.Filter.ExcludeDomains = CleanDomains(c.Filter.ExcludeDomains)
	c.Filter.IncludeApps = CleanApps(c.Filter.IncludeApps)
	c.Filter.ExcludeApps = CleanApps(c.Filter.ExcludeApps)
	c.Token = strings.TrimSpace(c.Token)
}

func (c Config) Validate() error {
	if _, _, err := net.SplitHostPort(c.Listen); err != nil {
		return errString("监听地址无效，需要 host:port")
	}
	u, err := url.Parse(c.VictoriaLogs.URL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return errString("VictoriaLogs 地址无效")
	}
	if strings.ContainsAny(c.Token, "\r\n; ") || len(c.Token) > 256 {
		return errString("口令无效")
	}
	return nil
}

type errString string

func (e errString) Error() string { return string(e) }

func CleanIfaces(in []string) []string {
	return unique(in, func(s string) (string, bool) {
		s = strings.TrimSpace(s)
		if !validIface.MatchString(s) {
			return "", false
		}
		return s, true
	}, 32)
}

func CleanDomains(in []string) []string {
	return unique(in, func(s string) (string, bool) {
		s = strings.ToLower(strings.TrimSpace(strings.TrimSuffix(s, ".")))
		if s == "" || len(s) > 253 || strings.ContainsAny(s, " \t") {
			return "", false
		}
		return s, true
	}, 200)
}

func CleanApps(in []string) []string {
	return unique(in, func(s string) (string, bool) {
		s = strings.TrimSpace(s)
		if s == "" || len(s) > 80 || strings.ContainsAny(s, "\r\n") {
			return "", false
		}
		return s, true
	}, 200)
}

func unique(in []string, norm func(string) (string, bool), max int) []string {
	if in == nil {
		return []string{}
	}
	out := make([]string, 0, len(in))
	seen := map[string]struct{}{}
	for _, s := range in {
		s, ok := norm(s)
		if !ok {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
		if len(out) >= max {
			break
		}
	}
	return out
}

func splitList(s string) []string {
	parts := strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == ';' || r == '\n'
	})
	return parts
}
