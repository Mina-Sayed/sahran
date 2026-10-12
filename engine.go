package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

type Event struct {
	Time  time.Time `json:"time"`
	Level string    `json:"level"` // info | warn
	Text  string    `json:"text"`
}

type NetInfo struct {
	Online     bool   `json:"online"`
	Connection string `json:"connection"`
	OnHotspot  bool   `json:"on_hotspot"`
	HotspotSet bool   `json:"hotspot_set"`
}

type Status struct {
	Version     string      `json:"version"`
	Mode        string      `json:"mode"`
	State       string      `json:"state"` // awake | watching | off | cooling
	Headline    string      `json:"headline"`
	Detail      string      `json:"detail"`
	LidClosed   bool        `json:"lid_closed"`
	Agents      []string    `json:"agents"`
	AgentProcs  []AgentProc `json:"agent_procs"`
	TempC       int         `json:"temp_c"`
	MaxTemp     int         `json:"max_temp"`
	ResumeTemp  int         `json:"resume_temp"`
	HasBattery  bool        `json:"has_battery"`
	Battery     int         `json:"battery"`
	OnBattery   bool        `json:"on_battery"`
	Inhibiting  bool        `json:"inhibiting"`
	PausedCount int         `json:"paused_count"`
	Net         NetInfo     `json:"net"`
	StartedAt   time.Time   `json:"started_at"`
	Events      []Event     `json:"events"`
}

type inhibitor struct {
	cmd  *exec.Cmd
	dead atomic.Bool
}

// Engine owns all side effects. Only the loop goroutine touches the "loop-owned"
// fields; everything shared with the HTTP server is behind mu.
type Engine struct {
	mu      sync.Mutex
	cfg     Config
	st      Status
	events  []Event
	started time.Time
	kick    chan struct{}

	// loop-owned
	inh         *inhibitor
	paused      map[int]bool
	saverOn     bool
	prevProfile string
	cooling     bool
	lowBat      bool
	fails       int
	lastReturn  time.Time
	prevLid     bool
	prevAgents  map[string]bool
	dismissed   map[int]uint64 // guarded by mu
	lastAgents  []AgentProc    // guarded by mu
	warnedInh   bool
	inhLogged   bool
	inhStarts   []time.Time
	inhBackoff  time.Time
	net         NetInfo
	netBusy     atomic.Bool  // a hotspot/home switch is running in the background
	netBackoff  atomic.Int64 // unix time before which a return home is not retried
}

func newEngine(cfg Config) *Engine {
	return &Engine{
		cfg:        cfg,
		started:    time.Now(),
		kick:       make(chan struct{}, 1),
		paused:     map[int]bool{},
		prevAgents: map[string]bool{},
		dismissed:  map[int]uint64{},
	}
}

func (e *Engine) getCfg() Config {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.cfg
}

func (e *Engine) log(level, text string) {
	e.mu.Lock()
	e.events = append(e.events, Event{time.Now(), level, text})
	if len(e.events) > 60 {
		e.events = e.events[len(e.events)-60:]
	}
	notify := e.cfg.Notify && level == "warn"
	e.mu.Unlock()
	fmt.Printf("%s [%s] %s\n", time.Now().Format("15:04:05"), level, text)
	if notify {
		if p, err := exec.LookPath("notify-send"); err == nil {
			_ = exec.Command(p, "-a", "sahran", "-i", "weather-clear-night", "sahran", text).Start()
		}
	}
}

func (e *Engine) Snapshot() Status {
	e.mu.Lock()
	defer e.mu.Unlock()
	s := e.st
	s.Version = version
	s.StartedAt = e.started
	s.Events = append([]Event(nil), e.events...)
	if s.Agents == nil {
		s.Agents = []string{}
	}
	if s.AgentProcs == nil {
		s.AgentProcs = []AgentProc{}
	}
	return s
}

func (e *Engine) kickNow() {
	select {
	case e.kick <- struct{}{}:
	default:
	}
}

func (e *Engine) SetMode(mode string) error {
	switch mode {
	case "auto", "on", "off":
	default:
		return fmt.Errorf("mode must be auto, on or off")
	}
	e.mu.Lock()
	changed := e.cfg.Mode != mode
	e.cfg.Mode = mode
	cfg := e.cfg
	e.mu.Unlock()
	if changed {
		names := map[string]string{"auto": "Auto", "on": "Always awake", "off": "Off"}
		e.log("info", "Mode set to "+names[mode]+".")
	}
	e.kickNow()
	return cfg.save()
}

func (e *Engine) SetConfig(c Config) error {
	c.sanitize()
	e.mu.Lock()
	c.Port = e.cfg.Port // port changes need a restart; keep the live one until then
	old := e.cfg
	e.cfg = c
	e.mu.Unlock()
	if old.Mode != c.Mode {
		e.log("info", "Mode set to "+c.Mode+".")
	}
	e.log("info", "Settings saved.")
	e.kickNow()
	return c.save()
}

// ---- main loop ----

func (e *Engine) loop(ctx context.Context) {
	e.tick()
	for {
		t := time.NewTimer(time.Duration(e.getCfg().IntervalSec) * time.Second)
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-e.kick:
			t.Stop()
		case <-t.C:
		}
		e.tick()
	}
}

func (e *Engine) shutdown() {
	e.releaseInhibit()
	e.resumeApps(e.getCfg(), false)
}

func (e *Engine) tick() {
	cfg := e.getCfg()
	lid := lidClosed()
	temp := cpuTemp()
	procs := scanProcs()
	aps := e.agentProcs(procs, cfg)
	agents := agentNames(aps)
	batt, onBatt, hasBatt := battery()

	e.trackAgents(agents)
	if lid != e.prevLid {
		e.prevLid = lid
		if cfg.Mode != "off" && (cfg.Mode == "on" || len(agents) > 0) {
			if lid {
				e.log("info", "Lid closed. Staying awake.")
			} else {
				e.log("info", "Lid opened.")
			}
		}
	}

	active := cfg.Mode == "on" || (cfg.Mode == "auto" && len(agents) > 0)

	// safety nets: only matter when we are the reason the laptop is awake
	if e.cooling && temp <= cfg.ResumeTemp {
		e.cooling = false
		e.log("info", fmt.Sprintf("Cooled down to %d°C. Back to normal.", temp))
	}
	if e.lowBat && (!onBatt || batt > cfg.MinBattery+5) {
		e.lowBat = false
	}
	if active && lid && !e.cooling && temp >= cfg.MaxTemp {
		e.cooling = true
		e.log("warn", fmt.Sprintf("Laptop is at %d°C with the lid closed. Going to sleep to cool down.", temp))
		e.sleepNow(cfg)
	}
	if active && lid && !e.lowBat && hasBatt && onBatt && batt <= cfg.MinBattery {
		e.lowBat = true
		e.log("warn", fmt.Sprintf("Battery is at %d%%. Going to sleep.", batt))
		e.sleepNow(cfg)
	}

	hold := active && !e.cooling && !e.lowBat
	if hold {
		e.acquireInhibit()
	} else {
		e.releaseInhibit()
	}
	if hold && lid {
		e.pauseApps(cfg, procs)
	} else {
		e.resumeApps(cfg, true)
	}
	e.netStep(cfg, active)

	state, head, detail := describe(cfg, hold, lid, e.cooling, e.lowBat, agents, temp, batt)
	e.mu.Lock()
	e.st = Status{
		Mode: cfg.Mode, State: state, Headline: head, Detail: detail,
		LidClosed: lid, Agents: agents, AgentProcs: aps, TempC: temp, MaxTemp: cfg.MaxTemp, ResumeTemp: cfg.ResumeTemp,
		HasBattery: hasBatt, Battery: batt, OnBattery: onBatt,
		Inhibiting: e.inh != nil && !e.inh.dead.Load(), PausedCount: len(e.paused),
		Net: e.net,
	}
	e.mu.Unlock()
}

// agentProcs finds running agents and forgets dismissals of processes that are gone.
func (e *Engine) agentProcs(procs []proc, cfg Config) []AgentProc {
	e.mu.Lock()
	dis := make(map[int]uint64, len(e.dismissed))
	for k, v := range e.dismissed {
		dis[k] = v
	}
	e.mu.Unlock()

	aps := findAgents(procs, cfg.AgentProcs, cfg.IgnoreArgs, dis)

	alive := make(map[int]uint64, len(procs))
	for _, p := range procs {
		alive[p.pid] = p.start
	}
	e.mu.Lock()
	for pid, st := range e.dismissed {
		if alive[pid] != st {
			delete(e.dismissed, pid)
		}
	}
	e.lastAgents = aps
	e.mu.Unlock()
	return aps
}

// DismissName ignores every currently running process of the named agent until it exits.
func (e *Engine) DismissName(name string) (int, error) {
	e.mu.Lock()
	n := 0
	for _, a := range e.lastAgents {
		if a.Name == name {
			e.dismissed[a.PID] = a.start
			n++
		}
	}
	e.mu.Unlock()
	if n == 0 {
		return 0, fmt.Errorf("no running agent named %q", name)
	}
	e.log("info", fmt.Sprintf("Ignoring %s until it exits.", name))
	e.kickNow()
	return n, nil
}

func (e *Engine) trackAgents(agents []string) {
	now := map[string]bool{}
	for _, a := range agents {
		now[a] = true
		if !e.prevAgents[a] {
			e.log("info", "Agent started: "+a+".")
		}
	}
	for a := range e.prevAgents {
		if !now[a] {
			e.log("info", "Agent finished: "+a+".")
		}
	}
	e.prevAgents = now
}

func describe(cfg Config, hold, lid, cooling, lowBat bool, agents []string, temp, batt int) (state, head, detail string) {
	switch {
	case cfg.Mode == "off":
		return "off", "Sahran is off.", "Your laptop sleeps normally when you close the lid."
	case cooling:
		return "cooling", "Too hot, so the laptop is sleeping.",
			fmt.Sprintf("The CPU reached %d°C. Sahran steps back in once it drops below %d°C.", temp, cfg.ResumeTemp)
	case lowBat:
		return "cooling", "Battery is low, so the laptop is sleeping.", "Plug in and sahran picks up where it left off."
	case hold && lid && len(agents) > 0:
		return "awake", "Lid closed. Your agents are still working.", "Running: " + strings.Join(agents, ", ") + "."
	case hold && lid:
		return "awake", "Lid closed. Staying awake.", "Always awake is on, so nothing will put this laptop to sleep."
	case hold && len(agents) > 0:
		return "awake", "Awake. You can close the lid any time.", "Running: " + strings.Join(agents, ", ") + "."
	case hold:
		return "awake", "Awake. You can close the lid any time.", "Always awake is on."
	default:
		return "watching", "Waiting for an agent to start.", "Until then the laptop sleeps like normal."
	}
}

func (e *Engine) sleepNow(cfg Config) {
	e.releaseInhibit()
	e.resumeApps(cfg, true)
	go func() { _ = exec.Command("systemctl", "suspend").Run() }()
}

// ---- inhibitor ----

func (e *Engine) acquireInhibit() {
	if e.inh != nil && !e.inh.dead.Load() {
		return
	}
	e.inh = nil
	if time.Now().Before(e.inhBackoff) {
		return
	}
	path, err := exec.LookPath("systemd-inhibit")
	if err != nil {
		if !e.warnedInh {
			e.warnedInh = true
			e.log("warn", "systemd-inhibit not found. Sahran cannot block sleep on this system.")
		}
		return
	}
	// If the lock keeps dying right away (e.g. logind is unavailable), stop hammering it.
	now := time.Now()
	recent := e.inhStarts[:0]
	for _, t := range e.inhStarts {
		if now.Sub(t) < 30*time.Second {
			recent = append(recent, t)
		}
	}
	e.inhStarts = append(recent, now)
	if len(e.inhStarts) > 3 {
		e.inhBackoff = now.Add(time.Minute)
		e.inhStarts = nil
		e.log("warn", "Sleep lock keeps failing. Is systemd-logind running? Retrying in a minute.")
		return
	}
	cmd := exec.Command(path, "--what=handle-lid-switch:sleep", "--who=sahran",
		"--why=AI agents are running", "--mode=block", "sleep", "infinity")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		e.log("warn", "Could not block sleep: "+err.Error())
		return
	}
	in := &inhibitor{cmd: cmd}
	go func() { _ = cmd.Wait(); in.dead.Store(true) }()
	e.inh = in
	if !e.inhLogged {
		e.inhLogged = true
		e.log("info", "Sleep is now blocked.")
	}
}

func (e *Engine) releaseInhibit() {
	if e.inh == nil {
		return
	}
	if !e.inh.dead.Load() && e.inh.cmd.Process != nil {
		_ = syscall.Kill(-e.inh.cmd.Process.Pid, syscall.SIGTERM)
	}
	e.inh = nil
	if e.inhLogged {
		e.inhLogged = false
		e.log("info", "Sleep is allowed again.")
	}
}

// ---- pausing apps / power profile ----

func (e *Engine) pauseApps(cfg Config, procs []proc) {
	want := map[string]bool{}
	for _, a := range cfg.PauseApps {
		want[a] = true
	}
	me := os.Getpid()
	before := len(e.paused)
	for _, p := range procs {
		if want[p.comm] && p.pid != me && !e.paused[p.pid] {
			if syscall.Kill(p.pid, syscall.SIGSTOP) == nil {
				e.paused[p.pid] = true
			}
		}
	}
	if before == 0 && len(e.paused) > 0 {
		e.log("info", fmt.Sprintf("Paused %d background processes to save power.", len(e.paused)))
	}
	if cfg.PowerSaver && !e.saverOn {
		if p, err := exec.LookPath("powerprofilesctl"); err == nil {
			if out, err := exec.Command(p, "get").Output(); err == nil {
				e.prevProfile = strings.TrimSpace(string(out))
				if exec.Command(p, "set", "power-saver").Run() == nil {
					e.saverOn = true
				}
			}
		}
	}
}

func (e *Engine) resumeApps(cfg Config, announce bool) {
	n := len(e.paused)
	for pid := range e.paused {
		_ = syscall.Kill(pid, syscall.SIGCONT)
	}
	e.paused = map[int]bool{}
	if e.saverOn {
		if p, err := exec.LookPath("powerprofilesctl"); err == nil && e.prevProfile != "" {
			_ = exec.Command(p, "set", e.prevProfile).Run()
		}
		e.saverOn = false
	}
	if n > 0 && announce {
		e.log("info", "Background processes resumed.")
	}
}

// resumeAll is used by `sahran resume` as a safety net after a crash.
func resumeAll(cfg Config) int {
	want := map[string]bool{}
	for _, a := range cfg.PauseApps {
		want[a] = true
	}
	n := 0
	for _, p := range scanProcs() {
		if want[p.comm] && syscall.Kill(p.pid, syscall.SIGCONT) == nil {
			n++
		}
	}
	return n
}

// ---- network fallback ----

func nmcli(timeout time.Duration, args ...string) (string, error) {
	p, err := exec.LookPath("nmcli")
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, p, args...).Output()
	if ee, ok := err.(*exec.ExitError); ok && len(ee.Stderr) > 0 {
		err = fmt.Errorf("%s", strings.TrimSpace(string(ee.Stderr)))
	}
	return strings.TrimSpace(string(out)), err
}

// splitNM splits a terse nmcli line "NAME:TYPE" where NAME may contain escaped colons.
func splitNM(line string) (name, typ string) {
	i := strings.LastIndex(line, ":")
	if i < 0 {
		return unescapeNM(line), ""
	}
	return unescapeNM(line[:i]), line[i+1:]
}

func unescapeNM(s string) string {
	s = strings.ReplaceAll(s, `\:`, ":")
	return strings.ReplaceAll(s, `\\`, `\`)
}

func activeConns() []string {
	out, err := nmcli(4*time.Second, "-t", "-f", "NAME,TYPE", "connection", "show", "--active")
	if err != nil {
		return nil
	}
	var names []string
	for _, l := range strings.Split(out, "\n") {
		if n, t := splitNM(l); n != "" && t != "loopback" && t != "bridge" {
			names = append(names, n)
		}
	}
	return names
}

type Conn struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

func listConns() []Conn {
	out, err := nmcli(4*time.Second, "-t", "-f", "NAME,TYPE", "connection", "show")
	conns := []Conn{}
	if err != nil {
		return conns
	}
	for _, l := range strings.Split(out, "\n") {
		n, t := splitNM(l)
		if n != "" && (t == "802-11-wireless" || t == "wifi" || t == "gsm" || t == "bluetooth" || t == "802-3-ethernet" || t == "ethernet") {
			conns = append(conns, Conn{n, t})
		}
	}
	return conns
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// onHotspot is true only when the hotspot is active and the home Wi-Fi is not.
// (Both can be listed as active, e.g. USB tethering next to Wi-Fi.)
func onHotspot(cfg Config, active []string) bool {
	if cfg.HotspotConn == "" || !contains(active, cfg.HotspotConn) {
		return false
	}
	return cfg.PreferredConn == "" || !contains(active, cfg.PreferredConn)
}

func (e *Engine) netStep(cfg Config, watch bool) {
	online := checkOnline(cfg.CheckAddr)
	active := activeConns()
	onHot := onHotspot(cfg, active)
	name := ""
	if len(active) > 0 {
		name = active[0]
	}
	if onHot {
		name = cfg.HotspotConn
	} else if cfg.PreferredConn != "" && contains(active, cfg.PreferredConn) {
		name = cfg.PreferredConn
	}
	e.net = NetInfo{Online: online, Connection: name, OnHotspot: onHot, HotspotSet: cfg.HotspotConn != ""}
	if cfg.HotspotConn == "" || !watch {
		return
	}
	if online {
		e.fails = 0
		if onHot && cfg.PreferredConn != "" && time.Since(e.lastReturn) > 5*time.Minute &&
			time.Now().Unix() >= e.netBackoff.Load() {
			e.lastReturn = time.Now()
			e.tryReturnHome(cfg)
		}
		return
	}
	e.fails++
	if e.fails >= cfg.NetFails && !onHot {
		e.fails = 0
		e.tryHotspot(cfg)
	}
}

// tryHotspot switches to the hotspot in the background so the loop keeps
// reporting fresh status. Only one switch runs at a time. It only touches
// thread-safe state (e.log, e.netBusy), like sleepNow's fire-and-forget.
func (e *Engine) tryHotspot(cfg Config) {
	if !e.netBusy.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer e.netBusy.Store(false)
		e.log("warn", "Internet is down. Switching to your hotspot, "+cfg.HotspotConn+".")
		if _, err := nmcli(30*time.Second, "connection", "up", cfg.HotspotConn); err != nil {
			e.log("warn", "Could not reach the hotspot. Turn it on from your phone.")
		} else {
			e.log("info", "Connected to "+cfg.HotspotConn+".")
		}
	}()
}

func (e *Engine) tryReturnHome(cfg Config) {
	if !e.netBusy.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer e.netBusy.Store(false)
		ssid, _ := nmcli(4*time.Second, "-g", "802-11-wireless.ssid", "connection", "show", cfg.PreferredConn)
		if ssid == "" {
			return
		}
		list, _ := nmcli(15*time.Second, "-t", "-f", "SSID", "device", "wifi", "list", "--rescan", "yes")
		for _, l := range strings.Split(list, "\n") {
			if unescapeNM(l) == ssid {
				e.log("info", "Home Wi-Fi is back. Switching to "+cfg.PreferredConn+".")
				if _, err := nmcli(30*time.Second, "connection", "up", cfg.PreferredConn); err != nil {
					e.log("warn", "Could not switch to "+cfg.PreferredConn+": "+err.Error()+". Trying again in 15 minutes.")
					e.netBackoff.Store(time.Now().Add(15 * time.Minute).Unix())
				}
				break
			}
		}
	}()
}
