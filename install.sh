#!/bin/sh
# sahran installer: downloads the latest release, verifies its checksum, installs the service.
#   curl -fsSL https://raw.githubusercontent.com/Mina-Sayed/sahran/main/install.sh | sh
# Options (environment): SAHRAN_VERSION=v1.2.3  SAHRAN_NO_SERVICE=1  SAHRAN_BASE_URL=<mirror>
set -eu

REPO="${SAHRAN_REPO:-Mina-Sayed/sahran}"
VERSION="${SAHRAN_VERSION:-latest}"

die() { echo "sahran installer: $*" >&2; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || die "missing required tool: $1"; }

[ "$(uname -s)" = "Linux" ] || die "sahran supports Linux only."
case "$(uname -m)" in
  x86_64 | amd64) ARCH=amd64 ;;
  aarch64 | arm64) ARCH=arm64 ;;
  *) die "unsupported CPU architecture: $(uname -m)" ;;
esac

need tar
need sha256sum
if command -v curl >/dev/null 2>&1; then
  fetch() { curl -fsSL "$1" -o "$2"; }
  fetch_stdout() { curl -fsSL "$1"; }
elif command -v wget >/dev/null 2>&1; then
  fetch() { wget -qO "$2" "$1"; }
  fetch_stdout() { wget -qO- "$1"; }
else
  die "need curl or wget"
fi

if [ "$VERSION" = "latest" ]; then
  VERSION=$(fetch_stdout "https://api.github.com/repos/$REPO/releases/latest" |
    sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -n1)
  [ -n "$VERSION" ] || die "could not find the latest release of $REPO"
fi
NUM="${VERSION#v}"
FILE="sahran_${NUM}_linux_${ARCH}.tar.gz"
BASE="${SAHRAN_BASE_URL:-https://github.com/$REPO/releases/download/$VERSION}"

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

echo "Downloading sahran $VERSION ($ARCH)..."
fetch "$BASE/$FILE" "$TMP/$FILE"
fetch "$BASE/checksums.txt" "$TMP/checksums.txt"

echo "Verifying checksum..."
(cd "$TMP" && grep " $FILE\$" checksums.txt | sha256sum -c -) || die "checksum mismatch, aborting."

tar -xzf "$TMP/$FILE" -C "$TMP" sahran

if [ "${SAHRAN_NO_SERVICE:-0}" = "1" ]; then
  mkdir -p "$HOME/.local/bin"
  install -m 755 "$TMP/sahran" "$HOME/.local/bin/sahran"
  echo "Installed to ~/.local/bin/sahran (service not started)."
else
  "$TMP/sahran" install
fi
