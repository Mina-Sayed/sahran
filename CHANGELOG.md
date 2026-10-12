# Changelog

## 0.3.0
- `t3code` and `claude-desktop` are watched by default.
- Helper daemons that outlive the session (`codex app-server`, `opencode serve`) are ignored by default. If a `serve` session should keep the laptop awake, remove `serve` from `ignore_args` in Settings.

## 0.2.3
- Agents that already exited no longer count: zombie and stopped processes are skipped.
- New `ignore_args` setting to skip helper processes (e.g. `app-server`), a × button in the dashboard and `sahran ignore <name>` to ignore an agent until it exits.
- New `sahran agents` command shows exactly which processes are counted.

## 0.2.2
- Settings now refuse the same connection for both hotspot and home Wi-Fi, with a clear message.

## 0.2.1
- Fix: "Home Wi-Fi is back" repeated every 5 minutes when the hotspot and home Wi-Fi were both listed as active.
- A failed switch back to home Wi-Fi is now reported and retried after 15 minutes.
- `sahran install` restarts the service, so re-running it upgrades a running install.

## 0.2.0
- Rewritten in Go: one static binary, no dependencies.
- Dashboard (web), terminal view and Waybar module.
- Auto / Always awake / Off modes.
- Thermal and low-battery protection, hotspot fallback with automatic return to home Wi-Fi.
- systemd user service installer.

## 0.1.0
- First Bash prototype.
