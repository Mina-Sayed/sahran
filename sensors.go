package main

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func readTrim(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// SAHRAN_FAKE_LID / SAHRAN_FAKE_TEMP exist for testing without hardware.
func lidClosed() bool {
	if f := os.Getenv("SAHRAN_FAKE_LID"); f != "" {
		return strings.Contains(readTrim(f), "closed")
	}
	files, _ := filepath.Glob("/proc/acpi/button/lid/*/state")
	for _, f := range files {
		if strings.Contains(readTrim(f), "closed") {
			return true
		}
	}
	return false
}

func cpuTemp() int {
	if f := os.Getenv("SAHRAN_FAKE_TEMP"); f != "" {
		v, _ := strconv.Atoi(readTrim(f))
		return v
	}
	zones, _ := filepath.Glob("/sys/class/thermal/thermal_zone*")
	best := 0
	for _, z := range zones {
		v, err := strconv.Atoi(readTrim(filepath.Join(z, "temp")))
		if err != nil {
			continue
		}
		c := v / 1000
		if readTrim(filepath.Join(z, "type")) == "x86_pkg_temp" {
			return c
		}
		if c > best && c < 150 {
			best = c
		}
	}
	return best
}

func battery() (pct int, onBattery, ok bool) {
	dirs, _ := filepath.Glob("/sys/class/power_supply/BAT*")
	for _, d := range dirs {
		v, err := strconv.Atoi(readTrim(filepath.Join(d, "capacity")))
		if err != nil {
			continue
		}
		return v, readTrim(filepath.Join(d, "status")) == "Discharging", true
	}
	return 0, false, false
}

type proc struct {
	pid   int
	comm  string
	state byte   // R, S, D, T, Z...
	start uint64 // start time in clock ticks, identifies a process even if its pid is reused
}

// parseStat reads comm, state and start time from the content of /proc/<pid>/stat.
func parseStat(s string) (comm string, state byte, start uint64, ok bool) {
	i := strings.IndexByte(s, '(')
	j := strings.LastIndexByte(s, ')')
	if i < 0 || j < i {
		return
	}
	f := strings.Fields(s[j+1:])
	if len(f) < 20 || f[0] == "" {
		return
	}
	start, _ = strconv.ParseUint(f[19], 10, 64)
	return s[i+1 : j], f[0][0], start, true
}

// scanProcs lists processes owned by the current user (including zombies and stopped ones).
func scanProcs() []proc {
	uid := os.Getuid()
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	var out []proc
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		fi, err := os.Stat("/proc/" + e.Name())
		if err != nil {
			continue
		}
		if st, ok := fi.Sys().(*syscall.Stat_t); !ok || int(st.Uid) != uid {
			continue
		}
		b, err := os.ReadFile("/proc/" + e.Name() + "/stat")
		if err != nil {
			continue
		}
		if comm, state, start, ok := parseStat(string(b)); ok && comm != "" {
			out = append(out, proc{pid, comm, state, start})
		}
	}
	return out
}

func cmdArgs(pid int) []string {
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	if err != nil {
		return nil
	}
	return strings.Split(strings.TrimRight(string(b), "\x00"), "\x00")
}

var readArgs = cmdArgs // replaced in tests

var interpreters = map[string]bool{"node": true, "bun": true, "deno": true, "python": true, "python3": true, "ruby": true}

// AgentProc is one running process that counts as an agent.
type AgentProc struct {
	Name  string `json:"name"`
	PID   int    `json:"pid"`
	State string `json:"state"`
	Cmd   string `json:"cmd"`
	start uint64
}

func toSet(list []string) map[string]bool {
	m := make(map[string]bool, len(list))
	for _, x := range list {
		m[x] = true
	}
	return m
}

// findAgents returns the processes that count as running agents. It skips zombies and stopped
// processes (nothing is working there), anything whose arguments match ignoreArgs (helpers like
// "app-server"), and processes the user dismissed (matched by pid + start time).
func findAgents(procs []proc, names, ignoreArgs []string, dismissed map[int]uint64) []AgentProc {
	want, ignore := toSet(names), toSet(ignoreArgs)
	var out []AgentProc
	for _, p := range procs {
		switch p.state {
		case 'Z', 'X', 'x', 'T', 't':
			continue
		}
		name := ""
		var args []string
		if want[p.comm] {
			name = p.comm
		} else if interpreters[p.comm] { // e.g. `node /usr/bin/gemini`
			args = readArgs(p.pid)
			for i := 0; i < len(args) && i < 3; i++ {
				if b := filepath.Base(args[i]); want[b] {
					name = b
					break
				}
			}
		}
		if name == "" {
			continue
		}
		if args == nil {
			args = readArgs(p.pid)
		}
		skip := false
		for _, a := range args {
			if ignore[a] {
				skip = true
				break
			}
		}
		if skip {
			continue
		}
		if st, ok := dismissed[p.pid]; ok && st == p.start {
			continue
		}
		cmd := strings.Join(args, " ")
		if len(cmd) > 160 {
			cmd = cmd[:160] + "..."
		}
		out = append(out, AgentProc{name, p.pid, string(p.state), cmd, p.start})
	}
	order := map[string]int{}
	for i, n := range names {
		order[n] = i
	}
	sort.Slice(out, func(i, j int) bool {
		if order[out[i].Name] != order[out[j].Name] {
			return order[out[i].Name] < order[out[j].Name]
		}
		return out[i].PID < out[j].PID
	})
	return out
}

// agentNames returns the distinct agent names, in the configured order.
func agentNames(aps []AgentProc) []string {
	names := []string{}
	seen := map[string]bool{}
	for _, a := range aps {
		if !seen[a.Name] {
			seen[a.Name] = true
			names = append(names, a.Name)
		}
	}
	return names
}

func checkOnline(addr string) bool {
	c, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		return false
	}
	c.Close()
	return true
}
