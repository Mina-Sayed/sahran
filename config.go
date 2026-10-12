package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Config is stored as JSON in ~/.config/sahran/config.json and editable from the dashboard.
type Config struct {
	Mode          string   `json:"mode"` // auto | on | off
	AgentProcs    []string `json:"agent_procs"`
	IgnoreArgs    []string `json:"ignore_args"` // skip agent processes with one of these arguments (e.g. app-server)
	PauseApps     []string `json:"pause_apps"`
	IntervalSec   int      `json:"interval_sec"`
	PowerSaver    bool     `json:"power_saver"`
	HotspotConn   string   `json:"hotspot_conn"`
	PreferredConn string   `json:"preferred_conn"`
	CheckAddr     string   `json:"check_addr"`
	NetFails      int      `json:"net_fails"`
	MaxTemp       int      `json:"max_temp"`
	ResumeTemp    int      `json:"resume_temp"`
	MinBattery    int      `json:"min_battery"`
	Notify        bool     `json:"notify"`
	Port          int      `json:"port"`
}

func defaultConfig() Config {
	return Config{
		Mode:        "auto",
		IgnoreArgs:  []string{"app-server", "serve"}, // helpers that outlive the session (codex/opencode daemons)
		AgentProcs:  []string{"claude", "codex", "opencode", "gemini", "aider", "cursor-agent", "t3code", "claude-desktop"},
		PauseApps:   []string{"chrome", "chromium", "brave", "firefox", "slack", "discord", "spotify"},
		IntervalSec: 5,
		PowerSaver:  true,
		CheckAddr:   "1.1.1.1:443",
		NetFails:    3,
		MaxTemp:     85,
		ResumeTemp:  70,
		MinBattery:  10,
		Notify:      true,
		Port:        7878,
	}
}

func configPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = filepath.Join(os.Getenv("HOME"), ".config")
	}
	return filepath.Join(dir, "sahran", "config.json")
}

func loadConfig() Config {
	c := defaultConfig()
	if b, err := os.ReadFile(configPath()); err == nil {
		if err := json.Unmarshal(b, &c); err != nil {
			fmt.Fprintln(os.Stderr, "sahran: ignoring corrupt config, using defaults:", err)
		}
	}
	c.sanitize()
	return c
}

func (c Config) save() error {
	p := configPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

func cleanList(in []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func (c *Config) sanitize() {
	switch c.Mode {
	case "auto", "on", "off":
	default:
		c.Mode = "auto"
	}
	c.AgentProcs = cleanList(c.AgentProcs)
	c.IgnoreArgs = cleanList(c.IgnoreArgs)
	c.PauseApps = cleanList(c.PauseApps)
	c.IntervalSec = clamp(c.IntervalSec, 2, 120)
	c.NetFails = clamp(c.NetFails, 1, 20)
	c.MaxTemp = clamp(c.MaxTemp, 60, 105)
	c.ResumeTemp = clamp(c.ResumeTemp, 30, c.MaxTemp-5)
	c.MinBattery = clamp(c.MinBattery, 0, 50)
	c.Port = clamp(c.Port, 1024, 65535)
	c.HotspotConn = strings.TrimSpace(c.HotspotConn)
	c.PreferredConn = strings.TrimSpace(c.PreferredConn)
	if c.PreferredConn == c.HotspotConn {
		c.PreferredConn = ""
	}
	c.CheckAddr = strings.TrimSpace(c.CheckAddr)
	if c.CheckAddr == "" {
		c.CheckAddr = "1.1.1.1:443"
	}
	if !strings.Contains(c.CheckAddr, ":") {
		c.CheckAddr += ":443"
	}
}
