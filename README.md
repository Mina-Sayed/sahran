<p align="center">
  <img src="docs/logo/banner.svg" alt="sahran: close the lid, your agents keep working" width="720">
</p>

**Close your laptop lid. Your AI agents keep working.**

A tiny Linux daemon for developers who run Claude Code, Codex, aider and friends. It keeps your laptop awake while an agent runs, even with the lid closed, and lets it sleep the moment they stop. One static binary, no dependencies.

> *Sahran* (سهران) is Egyptian Arabic for "up late, still awake". That is what this does for your laptop.

[![CI](https://github.com/Mina-Sayed/sahran/actions/workflows/ci.yml/badge.svg)](https://github.com/Mina-Sayed/sahran/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/license-MIT-f6b73c.svg)](LICENSE)

## Install

**One line (x86_64 and arm64):**

```bash
curl -fsSL https://raw.githubusercontent.com/Mina-Sayed/sahran/main/install.sh | sh
```

The script downloads the latest release, verifies its checksum, and starts a systemd user service. Read it first if you like: [install.sh](install.sh).

<details>
<summary>Other ways to install</summary>

- **Arch / Omarchy (AUR):** `yay -S sahran` *(once published)*, or build from `packaging/aur/PKGBUILD`.
- **Debian / Ubuntu / Fedora:** download the `.deb` or `.rpm` from the [Releases](https://github.com/Mina-Sayed/sahran/releases) page, install it, then `systemctl --user enable --now sahran`.
- **Go:** `go install github.com/Mina-Sayed/sahran@latest && sahran install`
- **From source:** `git clone ... && make build && ./sahran install`

</details>

Then:

```bash
sahran ui      # open the dashboard
```

## What it does

- **Keeps the laptop awake** while an agent runs. It uses a `systemd-inhibit` lock: nothing is edited system-wide and the lock vanishes when sahran stops.
- **Pauses background apps** (browser, Slack, Discord...) while the lid is closed and switches to power saver. They resume when you open the lid, or if sahran stops.
- **Falls back to your phone hotspot** when the internet drops, and returns to home Wi-Fi when it is back.
- **Protects your laptop in a bag.** Lid closed and CPU above 85°C, or battery below 10%, means it goes to sleep. Sahran steps back in once it is safe.

## Three ways to use it

| | |
|---|---|
| **Dashboard** `sahran ui` | Local web app on `127.0.0.1:7878`. Switch modes, see what is happening, edit settings. On Omarchy it opens as an app window. |
| **Terminal** `sahran tui` | Live view. `1` auto, `2` always awake, `3` off, `space` toggle, `o` dashboard, `q` quit. |
| **Waybar** | An eye icon that glows while your laptop is being kept awake. |

### Modes

- **Auto** (default): awake only while an agent is running.
- **Always awake**: never sleeps when the lid closes.
- **Off**: a normal laptop.

### Waybar

Add to `~/.config/waybar/config.jsonc` and put `"custom/sahran"` in `modules-right`:

```jsonc
"custom/sahran": {
  "exec": "sahran waybar",
  "return-type": "json",
  "interval": 3,
  "on-click": "sahran toggle",
  "on-click-right": "sahran ui"
}
```

```css
#custom-sahran.awake   { color: #f6b73c; }
#custom-sahran.cooling { color: #ff7b5c; }
#custom-sahran.off, #custom-sahran.watching { opacity: 0.55; }
```

## Commands

```
sahran status [-j]      what is it doing right now
sahran agents           which processes count as agents
sahran ignore <name>    ignore a running agent until it exits
sahran on | auto | off  set the mode
sahran toggle           Off <-> Auto
sahran resume           un-freeze paused apps (safety net)
sahran install          install and start the user service
sahran uninstall
```

## Settings

Edit them in the dashboard or in `~/.config/sahran/config.json`. List your hotspot and Wi-Fi connection names with `nmcli connection show`. The phone hotspot must have connected once so NetworkManager knows it.

## Troubleshooting

- **Lid still suspends the laptop:** check `HandleLidSwitch` in `/etc/systemd/logind.conf`, and any Hyprland `bindl = , switch:on:Lid Switch, ...` that suspends or locks and sleeps.
- **Lid state not detected:** some machines don't expose `/proc/acpi/button/lid`. Sleep blocking still works, but apps won't be paused. Please open an issue with your laptop model.
- **Sahran says an agent is running after I closed it:** run `sahran agents` to see which process is counted (pid, state, command). Zombie and stopped processes are skipped automatically, as are helper daemons that outlive the session (`codex app-server`, `opencode serve` — both are in **Ignore agent processes with these arguments** by default). For anything else that keeps running, press the × next to the agent in the dashboard (or `sahran ignore codex`) to ignore it until it exits.
- **See every decision:** `journalctl --user -u sahran -f`.

A closed laptop has less airflow. The thermal guard helps, but avoid running heavy loads inside a bag.

## Security

Runs as your user, listens on loopback only, rejects unknown `Host` headers, and requires a custom header on every write. See [SECURITY.md](SECURITY.md).

## Contributing

Issues and PRs are welcome. Start with [CONTRIBUTING.md](CONTRIBUTING.md). Maintainers: [docs/PUBLISHING.md](docs/PUBLISHING.md).

## License

[MIT](LICENSE)

## Credits

Inspired by a post from Krish Garg about a macOS app that keeps agents running with the lid closed. The logo text is converted to outlines from DM Serif Display, Amiri and Lato (SIL Open Font License). See [docs/logo](docs/logo).
