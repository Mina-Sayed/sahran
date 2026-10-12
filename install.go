package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

func homeDir() string { h, _ := os.UserHomeDir(); return h }

func installBinPath() string { return filepath.Join(homeDir(), ".local", "bin", "sahran") }

func unitPath() string {
	return filepath.Join(homeDir(), ".config", "systemd", "user", "sahran.service")
}

func unitText(bin string) string {
	return fmt.Sprintf(`[Unit]
Description=sahran - keep AI agents running with the lid closed

[Service]
Type=simple
ExecStart=%[1]s daemon
ExecStopPost=%[1]s resume
Restart=on-failure
RestartSec=3

[Install]
WantedBy=default.target
`, bin)
}

func copySelf(dest string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if exe == dest {
		return nil
	}
	data, err := os.ReadFile(exe)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	tmp := dest + ".new"
	if err := os.WriteFile(tmp, data, 0o755); err != nil {
		return err
	}
	return os.Rename(tmp, dest) // rename works even if the old binary is running
}

func sysctl(args ...string) error {
	c := exec.Command("systemctl", append([]string{"--user"}, args...)...)
	c.Stdout, c.Stderr = os.Stdout, os.Stderr
	return c.Run()
}

func runInstall() error {
	bin := installBinPath()
	if err := copySelf(bin); err != nil {
		return fmt.Errorf("copying binary: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(unitPath()), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(unitPath(), []byte(unitText(bin)), 0o644); err != nil {
		return err
	}
	if _, err := os.Stat(configPath()); os.IsNotExist(err) {
		if err := defaultConfig().save(); err != nil {
			return err
		}
	}
	if err := sysctl("daemon-reload"); err != nil {
		return err
	}
	if err := sysctl("enable", "sahran.service"); err != nil {
		return err
	}
	if err := sysctl("restart", "sahran.service"); err != nil { // also applies upgrades
		return err
	}
	fmt.Printf(`
sahran is installed and running.

  Open the dashboard   sahran ui
  Terminal view        sahran tui
  Logs                 journalctl --user -u sahran -f

Add it to Waybar (~/.config/waybar/config.jsonc):

  "custom/sahran": {
    "exec": "sahran waybar",
    "return-type": "json",
    "interval": 3,
    "on-click": "sahran toggle",
    "on-click-right": "sahran ui"
  }

...then put "custom/sahran" in modules-right and style it in style.css:

  #custom-sahran.awake    { color: #f6b73c; }
  #custom-sahran.cooling  { color: #ff7b5c; }
  #custom-sahran.off,
  #custom-sahran.watching { opacity: 0.55; }
`)
	return nil
}

func runUninstall() error {
	_ = sysctl("disable", "--now", "sahran.service")
	_ = os.Remove(unitPath())
	_ = os.Remove(installBinPath())
	_ = sysctl("daemon-reload")
	fmt.Println("sahran removed. Your settings are still in", filepath.Dir(configPath()))
	return nil
}

func openDashboard() error {
	url := baseURL() + "/"
	// Omarchy ships a helper that opens a URL as a standalone app window.
	for _, c := range [][]string{
		{"omarchy-launch-webapp", url},
		{"chromium", "--app=" + url},
		{"google-chrome-stable", "--app=" + url},
		{"brave", "--app=" + url},
		{"firefox", url},
		{"xdg-open", url},
	} {
		if p, err := exec.LookPath(c[0]); err == nil {
			cmd := exec.Command(p, c[1:]...)
			cmd.Stdout, cmd.Stderr = nil, nil
			return cmd.Start()
		}
	}
	fmt.Println("Open this in your browser:", url)
	return nil
}
