# Publishing checklist (maintainers)

1. **Create the repo and push:**
   ```bash
   git init -b main && git add . && git commit -m "Initial release"
   gh repo create Mina-Sayed/sahran --public --source=. --push
   ```
2. **Repo settings:** upload `docs/logo/social-preview.png` as the social preview, add topics (`linux`, `ai-agents`, `omarchy`, `hyprland`, `systemd`, `claude-code`, `go`), and turn on private vulnerability reporting.
3. **Tag a release.** GitHub Actions builds the binaries, checksums, `.deb`, `.rpm` and an Arch package:
   ```bash
   git tag v0.2.0 && git push origin v0.2.0
   ```
4. **Check the one-line installer** on a clean machine:
   ```bash
   curl -fsSL https://raw.githubusercontent.com/Mina-Sayed/sahran/main/install.sh | sh
   ```
5. **AUR (optional):** copy `packaging/aur/PKGBUILD` to a new AUR git repo, run `updpkgsums` and `makepkg --printsrcinfo > .SRCINFO`, push.
6. **Add a screenshot or GIF** of the dashboard to `docs/` and show it under the banner in the README. It is the best thing you can do for adoption.
