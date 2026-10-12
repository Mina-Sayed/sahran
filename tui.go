package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

const (
	cReset = "\x1b[0m"
	cDim   = "\x1b[2m"
	cBold  = "\x1b[1m"
	cLamp  = "\x1b[38;5;214m"
	cHot   = "\x1b[38;5;203m"
	cDusk  = "\x1b[38;5;104m"
)

func bar(pct, width int) string {
	pct = clamp(pct, 0, 100)
	n := pct * width / 100
	return strings.Repeat("█", n) + strings.Repeat("░", width-n)
}

func renderTUI(s Status, err error) string {
	var b strings.Builder
	w := func(f string, a ...any) { fmt.Fprintf(&b, f+"\r\n", a...) }
	w("")
	w("  %ssahran%s %s%s%s", cBold, cReset, cDim, version, cReset)
	w("")
	if err != nil {
		w("  %s%v%s", cHot, err, cReset)
		w("")
		w("  %sq quit%s", cDim, cReset)
		return b.String()
	}
	col := map[string]string{"awake": cLamp, "watching": cDusk, "off": cDim, "cooling": cHot}[s.State]
	dot := "●"
	if s.State == "off" {
		dot = "○"
	}
	w("  %s%s %s%s", col, dot, s.Headline, cReset)
	w("  %s  %s%s", cDim, s.Detail, cReset)
	w("")
	modes := []struct{ k, n, label string }{{"1", "auto", "Auto"}, {"2", "on", "Always awake"}, {"3", "off", "Off"}}
	var ms []string
	for _, m := range modes {
		if m.n == s.Mode {
			ms = append(ms, fmt.Sprintf("%s%s[ %s ]%s", cBold, cLamp, m.label, cReset))
		} else {
			ms = append(ms, fmt.Sprintf("%s %s %s", cDim, m.label, cReset))
		}
	}
	w("  %-9s %s", "Mode", strings.Join(ms, " "))
	lid := "open"
	if s.LidClosed {
		lid = "closed"
	}
	w("  %-9s %s", "Lid", lid)
	ag := "none running"
	if len(s.Agents) > 0 {
		ag = strings.Join(s.Agents, ", ")
	}
	w("  %-9s %s", "Agents", ag)
	tc := cLamp
	if s.TempC >= s.MaxTemp-8 {
		tc = cHot
	}
	w("  %-9s %s%s%s %d°C %s(limit %d°C)%s", "CPU", tc, bar((s.TempC-30)*100/(s.MaxTemp+10-30), 20), cReset, s.TempC, cDim, s.MaxTemp, cReset)
	if s.HasBattery {
		src := "on AC"
		if s.OnBattery {
			src = "on battery"
		}
		w("  %-9s %s%s%s %d%% %s%s%s", "Battery", cLamp, bar(s.Battery, 20), cReset, s.Battery, cDim, src, cReset)
	}
	net := "offline"
	if s.Net.Online {
		net = "online"
		if s.Net.Connection != "" {
			net += " via " + s.Net.Connection
		}
		if s.Net.OnHotspot {
			net += " (hotspot)"
		}
	}
	w("  %-9s %s", "Network", net)
	sl := "allowed"
	if s.Inhibiting {
		sl = "blocked"
	}
	w("  %-9s %s", "Sleep", sl)
	w("")
	w("  %sActivity%s", cBold, cReset)
	start := 0
	if len(s.Events) > 6 {
		start = len(s.Events) - 6
	}
	if len(s.Events) == 0 {
		w("  %snothing yet%s", cDim, cReset)
	}
	for i := len(s.Events) - 1; i >= start; i-- {
		ev := s.Events[i]
		c := cDim
		if ev.Level == "warn" {
			c = cHot
		}
		w("  %s%s  %s%s", c, ev.Time.Local().Format("15:04"), ev.Text, cReset)
	}
	w("")
	w("  %s1 auto   2 always awake   3 off   space toggle   o dashboard   q quit%s", cDim, cReset)
	return b.String()
}

func runTUI() error {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("no terminal available: %w", err)
	}
	defer tty.Close()
	stty := func(args ...string) {
		c := exec.Command("stty", args...)
		c.Stdin = tty
		_ = c.Run()
	}
	stty("raw", "-echo")
	defer stty("sane")
	fmt.Print("\x1b[?1049h\x1b[?25l")
	defer fmt.Print("\x1b[?25h\x1b[?1049l")

	keys := make(chan byte, 8)
	go func() {
		buf := make([]byte, 1)
		for {
			n, err := tty.Read(buf)
			if err != nil {
				close(keys)
				return
			}
			if n > 0 {
				keys <- buf[0]
			}
		}
	}()

	draw := func() Status {
		s, err := fetchStatus()
		fmt.Print("\x1b[H\x1b[2J" + renderTUI(s, err))
		return s
	}
	s := draw()
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case k, ok := <-keys:
			if !ok {
				return nil
			}
			switch k {
			case 'q', 3:
				return nil
			case '1':
				_ = api("POST", "/api/mode", map[string]string{"mode": "auto"}, nil)
			case '2':
				_ = api("POST", "/api/mode", map[string]string{"mode": "on"}, nil)
			case '3':
				_ = api("POST", "/api/mode", map[string]string{"mode": "off"}, nil)
			case ' ':
				next := "off"
				if s.Mode == "off" {
					next = "auto"
				}
				_ = api("POST", "/api/mode", map[string]string{"mode": next}, nil)
			case 'o':
				_ = openDashboard()
			}
			time.Sleep(150 * time.Millisecond)
			s = draw()
		case <-tick.C:
			s = draw()
		}
	}
}
