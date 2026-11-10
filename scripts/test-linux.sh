#!/bin/sh
# test-linux.sh — run the tests on a Linux machine, from the Mac.
#
#   scripts/test-linux.sh <ssh-host>
#
# The Mac cannot execute Linux binaries, and the parts that differ on Linux
# (volumes, the Trash, the engine's roots) are only tested where they run. So
# the two test binaries are built here for the host's architecture, copied to
# a temp folder there together with the golden files, the engine and grubber,
# run, and removed again. The host needs sshd, sh, tar and a file system with
# user extended attributes under its temp folder; no Go, nothing installed.
#
# The engine and grubber are the pinned release binaries, as in CI. To test
# against a checkout instead:
#
# Env: FILEANCHOR_LOCAL, GRUBBER_LOCAL (checkouts, built here for the host);
#      FILEANCHOR_VERSION, GRUBBER_VERSION (release tags, default as below);
#      TEST_ARGS (passed to both test binaries, e.g. "-test.run TestVolume -test.v").
set -eu
cd "$(dirname "$0")/.."

[ $# -eq 1 ] || { echo "Usage: scripts/test-linux.sh <ssh-host>" >&2; exit 2; }
HOST="$1"
FILEANCHOR_VERSION="${FILEANCHOR_VERSION:-1.5.1}"
GRUBBER_VERSION="${GRUBBER_VERSION:-v0.20.1}"

# The login shell there may be fish or anything else: every remote command
# is handed to sh.
remote() { ssh -o BatchMode=yes "$HOST" sh -s; }

case "$(echo 'uname -sm' | remote)" in
	"Linux x86_64") ARCH=amd64 ;;
	"Linux aarch64" | "Linux arm64") ARCH=arm64 ;;
	*) echo "test-linux.sh: $HOST is not a Linux machine on amd64 or arm64" >&2; exit 1 ;;
esac

STAGE="$(mktemp -d)"
REMOTE=""
cleanup() {
	rm -rf "$STAGE"
	[ -z "$REMOTE" ] || echo "rm -rf '$REMOTE'" | remote || true
}
trap cleanup EXIT

export GOOS=linux GOARCH="$ARCH" CGO_ENABLED=0
mkdir -p "$STAGE/bin" "$STAGE/src/cmd/register" "$STAGE/src/internal/index"
echo "building the tests for linux/$ARCH"
go test -c -o "$STAGE/src/cmd/register/register.test" ./cmd/register
go test -c -o "$STAGE/src/internal/index/index.test" ./internal/index
for pkg in cmd/register internal/index; do
	[ ! -d "$pkg/testdata" ] || cp -R "$pkg/testdata" "$STAGE/src/$pkg/"
done

# fetch <tool> <local checkout or ""> <package in the checkout> <release url>
fetch() {
	if [ -n "$2" ]; then
		echo "building $1 from $2"
		(cd "$2" && go build -o "$STAGE/bin/$1" "$3")
	else
		echo "fetching $4"
		curl -fsSL -o "$STAGE/bin/$1" "$4"
	fi
	chmod 755 "$STAGE/bin/$1"
}
fetch fileanchor "${FILEANCHOR_LOCAL:-}" ./linux \
	"https://github.com/rhsev/fileanchor/releases/download/$FILEANCHOR_VERSION/fileanchor-linux-$ARCH"
fetch grubber "${GRUBBER_LOCAL:-}" . \
	"https://github.com/rhsev/grubber/releases/download/$GRUBBER_VERSION/grubber-linux-$ARCH"

REMOTE="$(echo 'mktemp -d' | remote)"
echo "copying to $HOST:$REMOTE"
tar -C "$STAGE" -cf - . | ssh -o BatchMode=yes "$HOST" "sh -c 'tar -C \"$REMOTE\" -xf -'"

# The tests look for the engine and grubber in FILEANCHOR and GRUBBER_BIN, and
# take the temp folder as the engine's root (main_test.go).
remote <<EOF2
set -u
export FILEANCHOR='$REMOTE/bin/fileanchor' GRUBBER_BIN='$REMOTE/bin/grubber'
status=0
cd '$REMOTE/src/cmd/register' && ./register.test -test.count=1 ${TEST_ARGS:-} || status=1
cd '$REMOTE/src/internal/index' && ./index.test -test.count=1 ${TEST_ARGS:-} || status=1
exit \$status
EOF2
