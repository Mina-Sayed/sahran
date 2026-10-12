# Security policy

## Reporting a vulnerability

Please do **not** open a public issue. Use GitHub's private vulnerability reporting
(Security tab > Report a vulnerability). We aim to reply within 7 days.

## What sahran touches

- It runs as your user (never root) and only sends signals to your own processes.
- The dashboard listens on `127.0.0.1` only, rejects unknown `Host` headers, and requires a custom header on writes.
- It runs `systemd-inhibit`, `systemctl suspend`, `nmcli`, `powerprofilesctl` and `notify-send`, always with fixed arguments (no shell).
- The installer downloads release archives over HTTPS and verifies them against `checksums.txt`.
