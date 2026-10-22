#!/bin/sh
# release-linux.sh — build the downloadable Linux bundles, amd64 and arm64.
#
# Produces .build/dist/fileregister-linux-<arch>.tar.gz: `register` plus the
# Linux `fileanchor` engine and `grubber` in the layout register expects
# (libexec/), ready to attach to a GitHub release. All three are static Go
# binaries, so one bundle per architecture runs on any distribution that meets
# the requirements in INSTALL.txt.
#
# This runs on the Mac, which cannot execute Linux binaries. The version check
# therefore reads what each binary was built with (`go version -m`) instead of
# asking it; the Makefile and the fileanchor and grubber releases stamp the
# version there.
#
# Env: FILEANCHOR_VERSION (tag, default 1.5.0); FILEANCHOR_LOCAL (a fileanchor
# checkout after `make linux`, used instead of the release assets, for a trial
# run before the tag exists; then FILEANCHOR_VERSION is what that build says).
set -eu
cd "$(dirname "$0")/.."

FILEANCHOR_VERSION="${FILEANCHOR_VERSION:-1.5.0}"
GRUBBER_VERSION="${GRUBBER_VERSION:-v0.20.0}"
OUT="$(pwd)/.build/dist"
VERSION="$(git describe --tags --dirty --always | sed 's/^v//')"
[ -n "$VERSION" ] || { echo "release-linux.sh: no version from git describe — run it in a checkout" >&2; exit 1; }
mkdir -p "$OUT"

# stamped <binary> <ldflags fragment>: the version a Go binary was built with.
stamped() {
	go version -m "$1" | grep -q -- "$2" || {
		echo "release-linux.sh: $(basename "$1") is not stamped with '$2':" >&2
		go version -m "$1" | grep -E 'ldflags|GOOS|GOARCH' >&2
		exit 1
	}
	echo "    $(basename "$1"): $2"
}

for ARCH in amd64 arm64; do
	STAGE="$OUT/fileregister-linux-$ARCH"
	rm -rf "$STAGE" "$STAGE.tar.gz"
	mkdir -p "$STAGE/libexec" "$STAGE/licenses"

	echo "==> register $VERSION (linux/$ARCH)"
	CGO_ENABLED=0 GOOS=linux GOARCH="$ARCH" go build -ldflags="-s -w -X main.registerVersion=$VERSION" -o "$STAGE/register" ./cmd/register

	echo "==> fileanchor @ $FILEANCHOR_VERSION (linux/$ARCH)"
	if [ -n "${FILEANCHOR_LOCAL:-}" ]; then
		cp "$FILEANCHOR_LOCAL/fileanchor-linux-$ARCH" "$STAGE/libexec/fileanchor"
		cp "$FILEANCHOR_LOCAL/LICENSE" "$STAGE/licenses/fileanchor.txt"
	else
		curl -fsSL -o "$STAGE/libexec/fileanchor" \
			"https://github.com/rhsev/fileanchor/releases/download/$FILEANCHOR_VERSION/fileanchor-linux-$ARCH"
		curl -fsSL -o "$STAGE/licenses/fileanchor.txt" \
			"https://raw.githubusercontent.com/rhsev/fileanchor/$FILEANCHOR_VERSION/LICENSE"
	fi
	chmod 755 "$STAGE/libexec/fileanchor"

	echo "==> grubber @ $GRUBBER_VERSION (published release binary)"
	curl -fsSL -o "$STAGE/libexec/grubber" \
		"https://github.com/rhsev/grubber/releases/download/$GRUBBER_VERSION/grubber-linux-$ARCH"
	chmod 755 "$STAGE/libexec/grubber"
	curl -fsSL -o "$STAGE/licenses/grubber.txt" \
		"https://raw.githubusercontent.com/rhsev/grubber/$GRUBBER_VERSION/LICENSE"
	cp LICENSE "$STAGE/licenses/fileregister.txt"

	echo "==> versions"
	stamped "$STAGE/register" "main.registerVersion=$VERSION"
	stamped "$STAGE/libexec/fileanchor" "main.version=$FILEANCHOR_VERSION"
	stamped "$STAGE/libexec/grubber" "main.version=${GRUBBER_VERSION#v}"
	for b in "$STAGE/register" "$STAGE/libexec/fileanchor" "$STAGE/libexec/grubber"; do
		go version -m "$b" | grep -q "GOARCH=$ARCH" || { echo "release-linux.sh: $(basename "$b") is not linux/$ARCH" >&2; exit 1; }
	done

	cat > "$STAGE/INSTALL.txt" <<NOTE
fileregister for Linux ($ARCH)

Contents
  register            the CLI
  libexec/fileanchor  the metadata engine register calls at runtime
  libexec/grubber     reads the Markdown layer (the notes and their blocks)
  licenses/           register and fileanchor: PolyForm Noncommercial 1.0.0;
                      grubber: MIT

Requirements
  Linux kernel 6.6 or later. The binaries are static and need no libraries.
  Registered files live on a file system with user extended attributes: ext4,
  btrfs or xfs. FAT, exFAT, NTFS and NFS are not supported; an SMB mount works
  when the server keeps extended attributes (Samba with ea support).
  Registered files lie below one of the engine's roots: the lines of
  ~/.config/fileanchor/roots, one folder per line, or the home folder when that
  file does not exist. A moved file is found again only below a root.
  File names are precomposed (NFC); add refuses a decomposed name.
  Copy registered files only with their attributes: cp --preserve=xattr,
  rsync -X.

Install (copy all three, keeping the libexec/ layout, onto your PATH):
  mkdir -p ~/bin/libexec
  cp register ~/bin/register
  cp libexec/fileanchor libexec/grubber ~/bin/libexec/

register finds each helper via its variable (\$FILEANCHOR, \$GRUBBER_BIN), then
on PATH, then in <dir-of-register>/libexec/, so the layout above works as is.
Point register at a notes directory first: export GRUBBER_NOTES=~/notes (README).

To move a register from a Mac: register snapshot there, register restore here
(README, "Back up the whole register").
Tested with fileanchor $FILEANCHOR_VERSION and grubber $GRUBBER_VERSION.
NOTE

	tar -C "$OUT" -czf "$STAGE.tar.gz" "fileregister-linux-$ARCH"
	echo "==> $STAGE.tar.gz"
	shasum -a 256 "$STAGE.tar.gz"
done
