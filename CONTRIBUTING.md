# Contributing

Thanks for helping. sahran is small on purpose: standard library only, one static binary.

## Quick start

```bash
git clone https://github.com/Mina-Sayed/sahran && cd sahran
make test
make build
SAHRAN_FAKE_LID=/tmp/lid SAHRAN_FAKE_TEMP=/tmp/temp ./sahran daemon   # simulate hardware
echo "state: closed" > /tmp/lid; echo 90 > /tmp/temp                        # temp in °C
```

Then open http://127.0.0.1:7878 or run `./sahran tui`.

## Ground rules

- **No third-party Go dependencies** unless there is a strong reason. Open an issue first.
- Run `gofmt`, `go vet` and `go test ./...` before opening a PR (CI checks them).
- Keep the dashboard a single `web/index.html` with no external requests (no CDNs, no fonts).
- Anything that can freeze a process or block sleep must be undone on shutdown. Test that path.
- Plain, specific UI text. Errors say what happened and what to do.

## Good first areas

- Detecting the lid on machines without `/proc/acpi/button/lid`
- More agent process names (open an issue with the output of `ps -eo comm | sort -u`)
- Other network managers (iwd, systemd-networkd)
- Translations of the dashboard

## Commits and releases

Use short imperative commit messages ("Add iwd hotspot support"). Maintainers cut releases by pushing a `vX.Y.Z` tag; GitHub Actions builds the binaries, checksums and packages.
