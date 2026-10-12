package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

// version is set at release time: -ldflags "-X main.version=1.2.3"
var version = "dev"

const help = `sahran - keep your AI agents running with the laptop lid closed

Usage:
  sahran install        install and start the background service
  sahran uninstall      remove the service
  sahran ui             open the dashboard
  sahran tui            live view in the terminal
  sahran status [-j]    show what sahran is doing
  sahran agents           list the processes counted as agents (pid, state, command)
  sahran ignore <name>    ignore a running agent until it exits
  sahran toggle         switch between Off and Auto
  sahran mode <m>       set mode: auto | on | off   (also: sahran on|off|auto)
  sahran waybar         JSON for a Waybar custom module
  sahran resume         un-freeze any paused apps (safety net)
  sahran daemon         run in the foreground (used by the service)
`

func main() {
	cmd := "help"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	switch cmd {
	case "daemon":
		fatal(runDaemon())
	case "install":
		fatal(runInstall())
	case "uninstall":
		fatal(runUninstall())
	case "ui":
		fatal(openDashboard())
	case "tui":
		fatal(runTUI())
	case "status":
		fatal(cmdStatus(len(os.Args) > 2 && (os.Args[2] == "-j" || os.Args[2] == "--json")))
	case "mode":
		if len(os.Args) < 3 {
			fatal(fmt.Errorf("usage: sahran mode auto|on|off"))
		}
		fatal(setMode(os.Args[2]))
	case "auto", "on", "off":
		fatal(setMode(cmd))
	case "agents":
		fatal(cmdAgents())
	case "ignore":
		if len(os.Args) < 3 {
			fatal(fmt.Errorf("usage: sahran ignore <agent name>"))
		}
		var out struct {
			Ignored int `json:"ignored"`
		}
		fatal(api("POST", "/api/dismiss", map[string]string{"name": os.Args[2]}, &out))
		fmt.Printf("Ignoring %d %s process(es) until they exit.\n", out.Ignored, os.Args[2])
	case "toggle":
		s, err := fetchStatus()
		fatal(err)
		next := "off"
		if s.Mode == "off" {
			next = "auto"
		}
		fatal(setMode(next))
	case "waybar":
		cmdWaybar()
	case "resume":
		fmt.Printf("resumed %d processes\n", resumeAll(loadConfig()))
	case "version", "--version", "-v":
		fmt.Println("sahran", version)
	default:
		fmt.Print(help)
	}
}

func fatal(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "sahran:", err)
		os.Exit(1)
	}
}

func setMode(m string) error {
	if err := api("POST", "/api/mode", map[string]string{"mode": m}, nil); err != nil {
		return err
	}
	names := map[string]string{"auto": "Auto", "on": "Always awake", "off": "Off"}
	fmt.Println("Mode:", names[m])
	return nil
}

func cmdStatus(asJSON bool) error {
	s, err := fetchStatus()
	if err != nil {
		return err
	}
	if asJSON {
		return json.NewEncoder(os.Stdout).Encode(s)
	}
	fmt.Println(s.Headline)
	fmt.Println(s.Detail)
	fmt.Printf("mode=%s lid=%v cpu=%d°C sleep-blocked=%v agents=%s\n",
		s.Mode, map[bool]string{true: "closed", false: "open"}[s.LidClosed], s.TempC, s.Inhibiting, strings.Join(s.Agents, ","))
	return nil
}

func cmdAgents() error {
	s, err := fetchStatus()
	if err != nil {
		return err
	}
	if len(s.AgentProcs) == 0 {
		fmt.Println("No agent processes found.")
		return nil
	}
	fmt.Printf("%-8s %-14s %-5s %s\n", "PID", "AGENT", "STATE", "COMMAND")
	for _, a := range s.AgentProcs {
		fmt.Printf("%-8d %-14s %-5s %s\n", a.PID, a.Name, a.State, a.Cmd)
	}
	fmt.Println("\nNot really working? `sahran ignore <agent>` ignores it until it exits, or add an argument to \"ignore_args\" in Settings.")
	return nil
}

func cmdWaybar() {
	out := map[string]string{}
	s, err := fetchStatus()
	if err != nil {
		out = map[string]string{"text": "\uf070", "class": "offline", "tooltip": "sahran service is not running"}
	} else {
		icon := "\uf070"
		if s.State == "awake" {
			icon = "\uf06e"
		}
		tip := s.Headline + "\n" + s.Detail + fmt.Sprintf("\nCPU %d°C", s.TempC)
		out = map[string]string{"text": icon, "class": s.State, "alt": s.Mode, "tooltip": tip}
	}
	_ = json.NewEncoder(os.Stdout).Encode(out)
}

func runDaemon() error {
	cfg := loadConfig()
	e := newEngine(cfg)
	addr := fmt.Sprintf("127.0.0.1:%d", cfg.Port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("cannot listen on %s (already running?): %w", addr, err)
	}
	srv := e.server(addr)
	go func() { _ = srv.Serve(ln) }()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	done := make(chan struct{})
	go func() { e.loop(ctx); close(done) }()
	fmt.Printf("sahran %s running. Dashboard: http://%s/\n", version, addr)

	<-ctx.Done()
	<-done
	sctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = srv.Shutdown(sctx)
	e.shutdown()
	return nil
}
