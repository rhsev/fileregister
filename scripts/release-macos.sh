#!/bin/sh
# release-macos.sh — build the downloadable macOS (Apple Silicon / arm64) bundle.
#
# Produces .build/dist/fileregister-macos-arm64.tar.gz: `register` (Go) plus the
# `fileanchor` engine (Swift) in the layout register expects (libexec/fileanchor),
# ready to attach to a GitHub release. No signing/notarization — these are CLIs,
# so Gatekeeper only nags on browser downloads; the bundled INSTALL.txt says how
# to clear it.
#
# arm64 only: a universal / x86_64 bundle needs `fileanchor` built for x86_64,
# and the multi-arch `swift build --arch arm64 --arch x86_64` uses xcbuild, which
# requires FULL Xcode. With only the Command Line Tools we build the host arch.
# register itself (pure Go) cross-compiles freely; fileanchor is the constraint.
#
# Env: FILEANCHOR_VERSION (tag, default 1.2.0), FILEANCHOR_SRC (local checkout)
set -eu
cd "$(dirname "$0")/.."

# Keep this in step with the engine the README says register is tested against.
# The bundle carries the engine, so a stale default ships an engine that cannot
# do what register expects of it — audit withholds its dead verdict then, which
# is correct but a poor thing to publish.
FILEANCHOR_VERSION="${FILEANCHOR_VERSION:-1.2.0}"
FILEANCHOR_REPO="${FILEANCHOR_REPO:-https://github.com/rhsev/fileanchor.git}"
ARCH="arm64"
OUT="$(pwd)/.build/dist"
STAGE="$OUT/fileregister-macos-$ARCH"
rm -rf "$OUT"
mkdir -p "$STAGE/libexec"

echo "==> register (Go, $ARCH)"
CGO_ENABLED=0 GOOS=darwin GOARCH="$ARCH" go build -o "$STAGE/register" ./cmd/register

echo "==> fileanchor (Swift, $ARCH) @ $FILEANCHOR_VERSION"
if [ -n "${FILEANCHOR_SRC:-}" ]; then
	SRC="$FILEANCHOR_SRC"
	echo "    (local checkout: $SRC)"
else
	SRC="$OUT/fileanchor-src"
	git clone --depth 1 --branch "$FILEANCHOR_VERSION" "$FILEANCHOR_REPO" "$SRC"
fi
( cd "$SRC" && swift build -c release )
ENGINE="$(find "$SRC/.build" -type f -name fileanchor -perm -111 | head -1)"
[ -n "$ENGINE" ] || { echo "error: built fileanchor binary not found" >&2; exit 1; }
cp "$ENGINE" "$STAGE/libexec/fileanchor"

cat > "$STAGE/INSTALL.txt" <<'NOTE'
fileregister — macOS, Apple Silicon (arm64)

Contents
  register            the CLI
  libexec/fileanchor  the metadata engine register calls at runtime

Install (copy both, keeping the libexec/ layout, onto your PATH):
  mkdir -p ~/bin/libexec
  cp register ~/bin/register
  cp libexec/fileanchor ~/bin/libexec/fileanchor

register finds the engine via $FILEANCHOR, then `fileanchor` on PATH, then
<dir-of-register>/libexec/fileanchor — so the layout above just works.

Gatekeeper: these are command-line tools, not apps. Fetched with curl/wget they
run straight away. If you downloaded through a browser and macOS says the
developer cannot be verified, clear the quarantine flag once:
  xattr -dr com.apple.quarantine register libexec/fileanchor

Point register at a notes directory first: export GRUBBER_NOTES=~/notes (README).
Intel Mac? Build from source instead (go install + `make fileanchor`).
Tested with fileanchor 1.1.0 and grubber v0.16.0.
NOTE

TARBALL="$OUT/fileregister-macos-$ARCH.tar.gz"
tar -C "$OUT" -czf "$TARBALL" "fileregister-macos-$ARCH"
echo "==> $TARBALL"
shasum -a 256 "$TARBALL"
file "$STAGE/register" "$STAGE/libexec/fileanchor"
