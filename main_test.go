package main

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSanitize(t *testing.T) {
	c := defaultConfig()
	c.Mode = "banana"
	c.MaxTemp = 500
	c.ResumeTemp = 500
	c.AgentProcs = []string{" claude ", "", "claude", "codex"}
	c.CheckAddr = "8.8.8.8"
	c.sanitize()
	if c.Mode != "auto" || c.MaxTemp != 105 || c.ResumeTemp >= c.MaxTemp {
		t.Fatalf("bad clamp: %+v", c)
	}
	if len(c.AgentProcs) != 2 || c.AgentProcs[0] != "claude" {
		t.Fatalf("bad list: %v", c.AgentProcs)
	}
	if c.CheckAddr != "8.8.8.8:443" {
		t.Fatalf("bad addr: %s", c.CheckAddr)
	}
}

func TestDescribe(t *testing.T) {
	c := defaultConfig()
	cases := []struct {
		mode            string
		hold, lid, cool bool
		agents          []string
		want            string
	}{
		{"off", false, false, false, nil, "off"},
		{"auto", false, false, false, nil, "watching"},
		{"auto", true, true, false, []string{"claude"}, "awake"},
		{"auto", false, true, true, []string{"claude"}, "cooling"},
	}
	for _, tc := range cases {
		c.Mode = tc.mode
		got, _, _ := describe(c, tc.hold, tc.lid, tc.cool, false, tc.agents, 60, 80)
		if got != tc.want {
			t.Errorf("%+v: got %s want %s", tc, got, tc.want)
		}
	}
}

func TestSplitNM(t *testing.T) {
	n, ty := splitNM(`Cafe\: Guest:802-11-wireless`)
	if n != "Cafe: Guest" || ty != "802-11-wireless" {
		t.Fatalf("got %q %q", n, ty)
	}
}

func TestRenderTUI(t *testing.T) {
	s := Status{Mode: "auto", State: "awake", Headline: "Awake.", Detail: "x", TempC: 50, MaxTemp: 85, Agents: []string{"claude"}}
	out := renderTUI(s, nil)
	if !strings.Contains(out, "claude") || !strings.Contains(out, "Always awake") {
		t.Fatal("render missing content")
	}
}

func TestOnHotspot(t *testing.T) {
	cfg := defaultConfig()
	cfg.HotspotConn, cfg.PreferredConn = "MyPhone", "HomeWiFi"
	cases := []struct {
		active []string
		want   bool
	}{
		{[]string{"MyPhone"}, true},
		{[]string{"HomeWiFi"}, false},
		{[]string{"MyPhone", "HomeWiFi"}, false}, // both listed: we are home
		{[]string{"lo"}, false},
		{nil, false},
	}
	for _, tc := range cases {
		if got := onHotspot(cfg, tc.active); got != tc.want {
			t.Errorf("%v: got %v want %v", tc.active, got, tc.want)
		}
	}
	cfg.PreferredConn = ""
	if !onHotspot(cfg, []string{"MyPhone"}) {
		t.Error("hotspot without a preferred connection should count as on hotspot")
	}
}

func TestSanitizeSameConnection(t *testing.T) {
	c := defaultConfig()
	c.HotspotConn, c.PreferredConn = "X", "X"
	c.sanitize()
	if c.PreferredConn != "" {
		t.Fatal("preferred must differ from hotspot")
	}
}

func TestParseStat(t *testing.T) {
	// comm may contain spaces and parentheses
	line := "4242 (my (weird) agent) S 1 4242 4242 34816 4242 4194304 100 0 0 0 1 2 0 0 20 0 1 0 987654 1000 100 18446744073709551615"
	comm, state, start, ok := parseStat(line)
	if !ok || comm != "my (weird) agent" || state != 'S' || start != 987654 {
		t.Fatalf("got %q %c %d %v", comm, state, start, ok)
	}
	if _, _, _, ok := parseStat("garbage"); ok {
		t.Fatal("garbage should not parse")
	}
}

func TestFindAgents(t *testing.T) {
	args := map[int][]string{
		1: {"codex"},
		2: {"codex", "app-server"},
		3: {"opencode"},
		4: {"node", "/usr/lib/node_modules/.bin/gemini", "--x"},
		5: {"claude"},
		6: {"aider"},
	}
	readArgs = func(pid int) []string { return args[pid] }
	defer func() { readArgs = cmdArgs }()

	procs := []proc{
		{1, "codex", 'S', 10},
		{2, "codex", 'S', 20},    // helper: ignored by argument
		{3, "opencode", 'Z', 30}, // zombie: nothing is running
		{4, "node", 'S', 40},     // interpreter running gemini
		{5, "claude", 'T', 50},   // stopped with Ctrl-Z
		{6, "aider", 'S', 60},    // dismissed by the user
		{7, "bash", 'S', 70},
	}
	names := []string{"claude", "codex", "opencode", "gemini", "aider"}
	got := findAgents(procs, names, []string{"app-server"}, map[int]uint64{6: 60})
	if len(got) != 2 || got[0].Name != "codex" || got[0].PID != 1 || got[1].Name != "gemini" {
		t.Fatalf("unexpected: %+v", got)
	}
	// a reused pid with a different start time is not dismissed
	got = findAgents([]proc{{6, "aider", 'S', 61}}, names, nil, map[int]uint64{6: 60})
	if len(got) != 1 {
		t.Fatalf("reused pid must count again: %+v", got)
	}
	if n := agentNames(findAgents(procs, names, nil, nil)); len(n) != 3 || n[0] != "codex" {
		t.Fatalf("names: %v", n)
	}
}

func testEngine(t *testing.T) *Engine {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", dir) // keep save() off the real config
	cfg := defaultConfig()
	cfg.Notify = false
	return newEngine(cfg)
}

func doReq(e *Engine, method, target, host, body string, header bool) *httptest.ResponseRecorder {
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, target, nil)
	} else {
		r = httptest.NewRequest(method, target, strings.NewReader(body))
	}
	r.Host = host
	if header && method == http.MethodPost {
		r.Header.Set("X-Sahran", "1")
	}
	w := httptest.NewRecorder()
	e.server("127.0.0.1:0").Handler.ServeHTTP(w, r)
	return w
}

func errOf(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("content-type = %q, want JSON", ct)
	}
	var m map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatalf("body is not JSON: %v (%q)", err, w.Body.String())
	}
	return m["error"]
}

func TestGuardRejectsBadHost(t *testing.T) {
	e := testEngine(t)
	w := doReq(e, "GET", "/api/status", "evil.com", "", false)
	if w.Code != 403 || errOf(t, w) != "forbidden host" {
		t.Fatalf("got %d %q", w.Code, w.Body.String())
	}
	w = doReq(e, "GET", "/api/status", "127.0.0.1:7878", "", false)
	if w.Code != 200 {
		t.Fatalf("loopback must pass, got %d", w.Code)
	}
}

func TestGuardRequiresHeaderOnPost(t *testing.T) {
	e := testEngine(t)
	w := doReq(e, "POST", "/api/mode", "127.0.0.1:7878", `{"mode":"on"}`, false)
	if w.Code != 403 || errOf(t, w) != "missing X-Sahran header" {
		t.Fatalf("got %d %q", w.Code, w.Body.String())
	}
}

func TestModeAPI(t *testing.T) {
	e := testEngine(t)
	w := doReq(e, "POST", "/api/mode", "localhost:7878", `{"mode":"banana"}`, true)
	if w.Code != 400 || errOf(t, w) == "" {
		t.Fatalf("bad mode must fail with a reason, got %d %q", w.Code, w.Body.String())
	}
	w = doReq(e, "POST", "/api/mode", "localhost:7878", `not json`, true)
	if w.Code != 400 {
		t.Fatalf("invalid JSON must fail, got %d", w.Code)
	}
	w = doReq(e, "POST", "/api/mode", "localhost:7878", `{"mode":"on"}`, true)
	if w.Code != 200 {
		t.Fatalf("got %d %q", w.Code, w.Body.String())
	}
	// Status mirrors the last tick, like the real daemon: tick once (fast,
	// CheckAddr points at a closed port) and the new mode must show up.
	e.mu.Lock()
	e.cfg.CheckAddr = "127.0.0.1:9"
	e.mu.Unlock()
	e.tick()
	w = doReq(e, "GET", "/api/status", "localhost:7878", "", false)
	var s Status
	if err := json.Unmarshal(w.Body.Bytes(), &s); err != nil || s.Mode != "on" {
		t.Fatalf("status = %+v, err = %v", s, err)
	}
}

func TestDismissAPI(t *testing.T) {
	e := testEngine(t)
	w := doReq(e, "POST", "/api/dismiss", "127.0.0.1:7878", `{"name":"nope"}`, true)
	if w.Code != 400 || errOf(t, w) == "" {
		t.Fatalf("unknown agent must fail with a reason, got %d %q", w.Code, w.Body.String())
	}
}

func TestConfigAPIRejectsSameConnection(t *testing.T) {
	e := testEngine(t)
	w := doReq(e, "POST", "/api/config", "127.0.0.1:7878",
		`{"hotspot_conn":"X","preferred_conn":"X"}`, true)
	if w.Code != 400 || errOf(t, w) == "" {
		t.Fatalf("same connection must fail with a reason, got %d %q", w.Code, w.Body.String())
	}
}

func TestNetStepOnlineStatus(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	e := testEngine(t)
	cfg := defaultConfig()
	cfg.CheckAddr = ln.Addr().String()
	e.netStep(cfg, true)
	if !e.net.Online {
		t.Fatal("local listener must count as online")
	}
}

func TestNetStepFailoverIsBackground(t *testing.T) {
	e := testEngine(t)
	cfg := defaultConfig()
	cfg.Notify = false
	cfg.HotspotConn = "TestPhone"
	cfg.CheckAddr = "127.0.0.1:9" // closed: refused at once, no 2s wait
	cfg.NetFails = 1
	start := time.Now()
	e.netStep(cfg, true)
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("netStep blocked the loop for %v", d)
	}
	if e.net.Online || !e.net.HotspotSet {
		t.Fatalf("bad net status: %+v", e.net)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		found := false
		for _, ev := range e.Snapshot().Events {
			if strings.Contains(ev.Text, "Switching to your hotspot") {
				found = true
				break
			}
		}
		if found {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("background failover never logged")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestDefaultAgentKnowledge(t *testing.T) {
	c := defaultConfig()
	for _, n := range []string{"claude", "codex", "opencode", "t3code", "claude-desktop"} {
		if !contains(c.AgentProcs, n) {
			t.Errorf("default agent_procs is missing %q", n)
		}
	}
	// helpers that outlive the session must never count by default
	for _, a := range []string{"app-server", "serve"} {
		if !contains(c.IgnoreArgs, a) {
			t.Errorf("default ignore_args is missing %q", a)
		}
	}
}

func TestFindAgentsSkipsHelpers(t *testing.T) {
	args := map[int][]string{
		1: {"/home/mina/.opencode/bin/opencode"},
		2: {"/home/mina/.opencode/bin/opencode", "serve", "--service"},
		3: {"/home/mina/.opencode/bin/opencode", "serve", "--hostname=127.0.0.1", "--port=45071"},
		4: {"codex"},
		5: {"/home/mina/.codex/packages/app-server-daemon/releases/0.162.1/bin/codex", "app-server", "--managed-daemon"},
		6: {"/usr/lib/t3code-nightly/t3code", "--ozone-platform=wayland"},
		7: {"/usr/lib/claude-desktop/claude-desktop", "--type=renderer"},
	}
	readArgs = func(pid int) []string { return args[pid] }
	defer func() { readArgs = cmdArgs }()

	c := defaultConfig()
	procs := []proc{
		{1, "opencode", 'S', 11},
		{2, "opencode", 'S', 12}, // lingering serve daemon
		{3, "opencode", 'S', 13}, // app-spawned serve backend
		{4, "codex", 'S', 14},
		{5, "codex", 'S', 15}, // lingering app-server daemon
		{6, "t3code", 'S', 16},
		{7, "claude-desktop", 'S', 17},
	}
	got := findAgents(procs, c.AgentProcs, c.IgnoreArgs, nil)
	names := map[string]bool{}
	for _, a := range got {
		names[a.Name] = true
	}
	for _, want := range []string{"opencode", "codex", "t3code", "claude-desktop"} {
		if !names[want] {
			t.Errorf("%s should be detected", want)
		}
	}
	if len(got) != 4 {
		t.Fatalf("helpers must not count, got %+v", got)
	}
}
