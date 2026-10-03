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
# register album reads the Markdown layer through grubber, so the bundle carries
# it the way it carries the engine. Same pin as the Makefile. The published
# release binary, not a source build: a build of the v0.18.0 tag reports 0.16.0.
GRUBBER_VERSION="${GRUBBER_VERSION:-v0.18.0}"
ARCH="arm64"
OUT="$(pwd)/.build/dist"
STAGE="$OUT/fileregister-macos-$ARCH"
rm -rf "$OUT"
mkdir -p "$STAGE/libexec"

# The version comes from the tag, as in the Makefile: what --version prints is
# what was tagged, or visibly not (1.4.0-2-g1a2b3c4-dirty).
VERSION="$(git describe --tags --dirty --always | sed 's/^v//')"
echo "==> register $VERSION (Go, $ARCH)"
CGO_ENABLED=0 GOOS=darwin GOARCH="$ARCH" go build -ldflags="-X main.registerVersion=$VERSION" -o "$STAGE/register" ./cmd/register

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

echo "==> grubber @ $GRUBBER_VERSION (published release binary)"
curl -fsSL -o "$STAGE/libexec/grubber" \
	"https://github.com/rhsev/grubber/releases/download/$GRUBBER_VERSION/grubber-macos-$ARCH"
chmod 755 "$STAGE/libexec/grubber"

# Three components, two licences: register and the engine are PolyForm
# Noncommercial, grubber is MIT — and MIT asks for its notice to travel with
# every copy. Each component's own file, so it is plain which applies to what.
mkdir -p "$STAGE/licenses"
cp LICENSE "$STAGE/licenses/fileregister.txt"
cp "$SRC/LICENSE" "$STAGE/licenses/fileanchor.txt"
curl -fsSL -o "$STAGE/licenses/grubber.txt" \
	"https://raw.githubusercontent.com/rhsev/grubber/$GRUBBER_VERSION/LICENSE"

cat > "$STAGE/INSTALL.txt" <<'NOTE'
fileregister — macOS, Apple Silicon (arm64)

Contents
  register            the CLI
  libexec/fileanchor  the metadata engine register calls at runtime
  libexec/grubber     reads the Markdown layer (captions, the album name, order)
  licenses/           register and fileanchor: PolyForm Noncommercial 1.0.0;
                      grubber: MIT

Install (copy all three, keeping the libexec/ layout, onto your PATH):
  mkdir -p ~/bin/libexec
  cp register ~/bin/register
  cp libexec/fileanchor libexec/grubber ~/bin/libexec/

register finds each helper via its variable ($FILEANCHOR, $GRUBBER_BIN), then
on PATH, then in <dir-of-register>/libexec/ — so the layout above just works.
Without grubber, register is the index: the lifecycle commands run, but
`register album` does not.

Gatekeeper: these are command-line tools, not apps. Fetched with curl/wget they
run straight away. If you downloaded through a browser and macOS says the
developer cannot be verified, clear the quarantine flag once:
  xattr -dr com.apple.quarantine register libexec

Point register at a notes directory first: export GRUBBER_NOTES=~/notes (README).
Intel Mac? Build from source instead (go install + `make fileanchor` +
`make grubber`).
Tested with fileanchor 1.2.0 and grubber v0.18.0.
NOTE

TARBALL="$OUT/fileregister-macos-$ARCH.tar.gz"
tar -C "$OUT" -czf "$TARBALL" "fileregister-macos-$ARCH"
echo "==> $TARBALL"
shasum -a 256 "$TARBALL"
file "$STAGE/register" "$STAGE/libexec/fileanchor" "$STAGE/libexec/grubber"
