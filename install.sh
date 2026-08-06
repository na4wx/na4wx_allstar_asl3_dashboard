#!/bin/bash
# Run this ON THE NODE, as root, from inside the cloned repo (Debian --
# ASL3's OS). Makes sure the tools needed to build this project are
# installed, pulls the latest code if there is any, then builds
# natively and redeploys via deploy/install.sh.
#
# Usage: sudo ./install.sh
#
# TODO (Phase 7, per the project plan): offer to install ASL3 itself
# first if it isn't already present, before any of the steps below --
# not yet implemented. Verify the real apt repo/package names against
# a node's actual state before building that step.

cat <<'EOF'
╔══════════════════════════════════════════════════════════╗
║                                                          ║
║     ███╗   ██╗ █████╗ ██╗  ██╗██╗    ██╗██╗  ██╗         ║
║     ████╗  ██║██╔══██╗██║  ██║██║    ██║╚██╗██╔╝         ║
║     ██╔██╗ ██║███████║███████║██║ █╗ ██║ ╚███╔╝          ║
║     ██║╚██╗██║██╔══██║╚════██║██║███╗██║ ██╔██╗          ║
║     ██║ ╚████║██║  ██║     ██║╚███╔███╔╝██╔╝ ██╗         ║
║     ╚═╝  ╚═══╝╚═╝  ╚═╝     ╚═╝ ╚══╝╚══╝ ╚═╝  ╚═╝         ║
║                                                          ║
║     A L L S T A R   D A S H B O A R D   ( A S L 3 )      ║
║                                                          ║
╚══════════════════════════════════════════════════════════╝
EOF

set -euo pipefail

MIN_GO_VERSION="1.22"
GO_TARBALL_VERSION="1.22.5"

# Colors only when actually printing to a terminal (never to a redirected
# log file, where raw escape codes would just show up as garbage), and
# only when the operator hasn't opted out via the standard NO_COLOR
# convention (https://no-color.org/). Same pattern as the HamVoIP app's
# own install.sh, proven there this session.
if [ -t 1 ] && [ -z "${NO_COLOR:-}" ]; then
	C_BOLD=$'\033[1m'
	C_BLUE=$'\033[34m'
	C_GREEN=$'\033[32m'
	C_YELLOW=$'\033[33m'
	C_RED=$'\033[31m'
	C_RESET=$'\033[0m'
else
	C_BOLD='' C_BLUE='' C_GREEN='' C_YELLOW='' C_RED='' C_RESET=''
fi

WARNINGS=()

log()  { printf '%s==>%s %s\n' "$C_BLUE$C_BOLD" "$C_RESET" "$*"; }
warn() { WARNINGS+=("$*"); printf '%s==> warning:%s %s\n' "$C_YELLOW$C_BOLD" "$C_RESET" "$*" >&2; }
err()  { printf '%serror:%s %s\n' "$C_RED$C_BOLD" "$C_RESET" "$*" >&2; exit 1; }

[ "$(id -u)" = "0" ] || err "run as root: sudo ./install.sh"
command -v apt-get >/dev/null 2>&1 || err "apt-get not found — this script is for Debian (ASL3's OS)"

REPO_ROOT=$(cd "$(dirname "$0")" && pwd)
cd "$REPO_ROOT"
[ -d .git ] || err "$REPO_ROOT is not a git checkout — clone the repo first"

apt_install() {
	apt-get update -qq
	apt-get install -y --no-install-recommends "$@"
}

# --- Go toolchain ---------------------------------------------------------

version_ge() { # version_ge A B => A >= B
	[ "$1" = "$2" ] && return 0
	[ "$(printf '%s\n%s\n' "$1" "$2" | sort -V | head -n1)" = "$2" ]
}

go_version() {
	command -v go >/dev/null 2>&1 || return 1
	go version | sed -n 's/^go version go\([0-9.]*\).*/\1/p'
}

need_go_install=1
if v=$(go_version); then
	if version_ge "$v" "$MIN_GO_VERSION"; then
		log "go $v already installed (>= $MIN_GO_VERSION)"
		need_go_install=0
	else
		log "go $v is installed but too old (need >= $MIN_GO_VERSION)"
	fi
fi

if [ "$need_go_install" = "1" ]; then
	# Debian's own golang-go package (bookworm/trixie) is often behind
	# current Go releases too -- try apt first in case yours is current
	# enough, fall back to installing the official upstream release
	# directly if not (same pattern as the HamVoIP app's own install.sh).
	log "Trying apt's golang-go package"
	apt_install golang-go || true
	v=$(go_version || echo "0")
	if ! version_ge "$v" "$MIN_GO_VERSION"; then
		log "apt's go ($v) is still too old — installing go $GO_TARBALL_VERSION from go.dev directly"
		case "$(uname -m)" in
			aarch64|arm64)
				GOARCH_TARBALL="arm64" ;;
			x86_64|amd64)
				GOARCH_TARBALL="amd64" ;;
			*)
				err "unrecognized architecture $(uname -m) — ASL3 only supports amd64/arm64; install Go manually from https://go.dev/dl/ if this is unexpected" ;;
		esac
		TARBALL="go${GO_TARBALL_VERSION}.linux-${GOARCH_TARBALL}.tar.gz"
		TMP=$(mktemp -d)
		curl -fsSL -o "$TMP/$TARBALL" "https://go.dev/dl/$TARBALL"
		rm -rf /usr/local/go
		tar -C /usr/local -xzf "$TMP/$TARBALL"
		rm -rf "$TMP"
		ln -sf /usr/local/go/bin/go /usr/local/bin/go
		ln -sf /usr/local/go/bin/gofmt /usr/local/bin/gofmt
		v=$(go_version) || err "go install from go.dev failed — check manually"
		log "Installed go $v to /usr/local/go"
	fi
fi

# TODO (later phases, per the project plan):
#   - Piper (TTS) setup -- internal/tts is already ported, but install.sh
#     doesn't yet fetch the piper binary/voice model on Debian.
#   - SkywarnPlus setup -- internal/skywarnplus is ported, same gap.
#   - NetworkManager verification -- internal/wifi's ASL3 port (Phase 4)
#     will need this script to confirm NetworkManager is active, not
#     install a competing network stack.

# --- pull latest ---------------------------------------------------------

log "Fetching latest from git"
git fetch origin

BRANCH=$(git rev-parse --abbrev-ref HEAD)
LOCAL=$(git rev-parse @)
REMOTE=$(git rev-parse "@{u}" 2>/dev/null) || err "branch $BRANCH has no upstream — check out a branch that tracks origin"

if [ "$LOCAL" = "$REMOTE" ]; then
	log "Already up to date ($LOCAL) — building and deploying the current checkout"
else
	if [ -n "$(git status --porcelain)" ]; then
		err "working tree has uncommitted changes and origin/$BRANCH has new commits — commit or stash, then re-run"
	fi
	log "Updating $BRANCH: $LOCAL -> $REMOTE"
	git pull --ff-only origin "$BRANCH"
fi

# --- build and deploy -----------------------------------------------------

log "Building"
make build

log "Deploying"
./deploy/install.sh "$REPO_ROOT/bin/asl3-gui"

echo
printf '%s%s✓ Install complete%s\n' "$C_GREEN" "$C_BOLD" "$C_RESET"
echo

if [ "${#WARNINGS[@]}" -gt 0 ]; then
	printf '%s%s%d item(s) need attention:%s\n' "$C_YELLOW" "$C_BOLD" "${#WARNINGS[@]}" "$C_RESET"
	for w in "${WARNINGS[@]}"; do
		printf '  %s-%s %s\n' "$C_YELLOW" "$C_RESET" "$w"
	done
	echo
fi

echo "Re-run this script anytime to update to the latest version from git."
echo
