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
╔═══════════════════════════════════════════════════════════╗
║                                                           ║
║       ███╗   ██╗ █████╗ ██╗  ██╗██╗    ██╗██╗  ██╗        ║
║       ████╗  ██║██╔══██╗██║  ██║██║    ██║╚██╗██╔╝        ║
║       ██╔██╗ ██║███████║███████║██║ █╗ ██║ ╚███╔╝         ║
║       ██║╚██╗██║██╔══██║╚════██║██║███╗██║ ██╔██╗         ║
║       ██║ ╚████║██║  ██║     ██║╚███╔███╔╝██╔╝ ██╗        ║
║       ╚═╝  ╚═══╝╚═╝  ╚═╝     ╚═╝ ╚══╝╚══╝ ╚═╝  ╚═╝        ║
║                                                           ║
║      A L L S T A R   D A S H B O A R D   ( A S L 3 )      ║
║                                                           ║
╚═══════════════════════════════════════════════════════════╝
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

# fetch URL OUTPUT_PATH -- every network download in this script goes
# through here rather than a bare `curl -fsSL`, for two reasons hit for
# real on a slow/stalled connection: silent (-s) mode shows nothing at
# all while it runs, so a slow-but-working download and a truly wedged
# connection look identical from the terminal; and plain curl has no
# timeout of its own, so a connection that stalls mid-transfer (a
# firewall silently dropping packets, a proxy that never completes the
# handshake) hangs forever with no way to tell it's stuck versus just
# slow. --connect-timeout bounds only the initial connection;
# --max-time bounds the whole transfer, generous enough for a large
# file (the Piper voice model below) over a slow connection.
fetch() {
	curl -fL --connect-timeout 15 --max-time 300 -o "$2" "$1"
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
		fetch "https://go.dev/dl/$TARBALL" "$TMP/$TARBALL"
		rm -rf /usr/local/go
		tar -C /usr/local -xzf "$TMP/$TARBALL"
		rm -rf "$TMP"
		ln -sf /usr/local/go/bin/go /usr/local/bin/go
		ln -sf /usr/local/go/bin/gofmt /usr/local/bin/gofmt
		v=$(go_version) || err "go install from go.dev failed — check manually"
		log "Installed go $v to /usr/local/go"
	fi
fi

# --- Piper (text-to-speech, for the "Create from text" sound generator) ----
#
# Piper's current, actively maintained project (OHF-Voice/piper1-gpl) only
# ships as a pip wheel with a different, incompatible "python3 -m piper -m
# ... -f ..." CLI -- this app's internal/tts package already shells out to
# the OLD project's (rhasspy/piper, archived) standalone
# "piper --model ... --output_file ..." binary, confirmed against that
# release specifically. That repo is frozen (no updates since Oct 2025),
# but for an offline-only local tool with no network exposure that's an
# acceptable tradeoff -- same reasoning and same pinned release the
# original HamVoIP app's install.sh used, proven there this session.
#
# ASL3 only targets amd64/arm64 (see the Go toolchain section above, which
# already hard-errors on anything else) -- no 32-bit ARM case here, unlike
# HamVoIP's own Pi Zero/1-inclusive install.sh.

log "Checking Piper (text-to-speech)"

PIPER_RELEASE_VERSION="2023.11.14-2"
PIPER_INSTALL_DIR="/usr/local/lib/piper"
PIPER_VOICES_DIR="/etc/asl3-gui/piper-voices"

# Default voices downloaded below, one male/one female so an operator has
# an actual choice out of the box rather than just whatever shipped
# first -- "name:huggingface-path" pairs, path relative to
# rhasspy/piper-voices' own repo root, without the .onnx/.onnx.json
# extension (confirmed against the real repo layout, e.g.
# en/en_US/hfc_female/medium/en_US-hfc_female-medium.onnx).
PIPER_DEFAULT_VOICES=(
	"en_US-lessac-medium:en/en_US/lessac/medium/en_US-lessac-medium"
	"en_US-hfc_female-medium:en/en_US/hfc_female/medium/en_US-hfc_female-medium"
)

PIPER_ARCH=""
case "$(uname -m)" in
	aarch64|arm64)
		PIPER_ARCH="aarch64" ;;
	x86_64|amd64)
		PIPER_ARCH="x86_64" ;;
	*)
		log "Piper has no known build for $(uname -m) — skipping text-to-speech setup. The app will use espeak-ng as the fallback if it's installed."
		;;
esac

if [ -n "$PIPER_ARCH" ]; then
	if [ -x "$PIPER_INSTALL_DIR/piper" ]; then
		log "Piper already installed at $PIPER_INSTALL_DIR/piper"
	else
		log "Installing Piper ($PIPER_ARCH)"
		TMP=$(mktemp -d)
		if fetch "https://github.com/rhasspy/piper/releases/download/$PIPER_RELEASE_VERSION/piper_linux_${PIPER_ARCH}.tar.gz" "$TMP/piper.tar.gz"; then
			# The tarball's own top-level directory is "piper/", which is
			# also PIPER_INSTALL_DIR's basename -- extracting straight into
			# its parent lands it exactly where it needs to be, no rename.
			rm -rf "$PIPER_INSTALL_DIR"
			tar -C "$(dirname "$PIPER_INSTALL_DIR")" -xzf "$TMP/piper.tar.gz"
			# piper needs the .so files and espeak-ng-data/ that ship
			# alongside it in the same directory (it locates them via an
			# $ORIGIN-relative rpath, confirmed present in the binary) -- so
			# this symlinks just the executable, not a copy, keeping it next
			# to everything it depends on.
			ln -sf "$PIPER_INSTALL_DIR/piper" /usr/local/bin/piper
			log "Installed Piper to $PIPER_INSTALL_DIR (symlinked to /usr/local/bin/piper)"
		else
			warn "couldn't download Piper (offline?) — skipping. Re-run this script with network access to pick it up, or set up text-to-speech manually later."
		fi
		rm -rf "$TMP"
	fi

	PIPER_READY=0
	if [ -x "$PIPER_INSTALL_DIR/piper" ]; then
		set +e
		PIPER_CHECK_OUTPUT=$("$PIPER_INSTALL_DIR/piper" --help 2>&1)
		PIPER_CHECK_STATUS=$?
		set -e
		if [ "$PIPER_CHECK_STATUS" = "0" ]; then
			PIPER_READY=1
		else
			warn "Piper is installed but cannot run on this system; skipping text-to-speech voice setup."
			log "Piper check output: ${PIPER_CHECK_OUTPUT//$'\n'/ | }"
			log "The app will fall back to espeak-ng for \"Create from text\" where available."
		fi
	fi

	if [ "$PIPER_READY" = "1" ]; then
		mkdir -p "$PIPER_VOICES_DIR"
		for entry in "${PIPER_DEFAULT_VOICES[@]}"; do
			voice="${entry%%:*}"
			voice_path="${entry#*:}"
			if [ -f "$PIPER_VOICES_DIR/$voice.onnx" ]; then
				log "Voice $voice already downloaded"
				continue
			fi
			log "Downloading voice: $voice"
			# Staged as .tmp and only renamed into place once both files
			# succeed, so a connection drop mid-download can never leave a
			# half-downloaded .onnx file that a re-run would mistake for
			# "already downloaded".
			if fetch "https://huggingface.co/rhasspy/piper-voices/resolve/main/$voice_path.onnx" "$PIPER_VOICES_DIR/$voice.onnx.tmp" \
				&& fetch "https://huggingface.co/rhasspy/piper-voices/resolve/main/$voice_path.onnx.json" "$PIPER_VOICES_DIR/$voice.onnx.json"; then
				mv "$PIPER_VOICES_DIR/$voice.onnx.tmp" "$PIPER_VOICES_DIR/$voice.onnx"
				log "Downloaded voice $voice to $PIPER_VOICES_DIR (more voices at https://huggingface.co/rhasspy/piper-voices)"
			else
				rm -f "$PIPER_VOICES_DIR/$voice.onnx.tmp" "$PIPER_VOICES_DIR/$voice.onnx.json"
				warn "couldn't download the $voice Piper voice (offline?) — re-run this script with network access, or see https://huggingface.co/rhasspy/piper-voices"
			fi
		done
	fi
fi

# --- espeak-ng (text-to-speech fallback) ------------------------------------
#
# Installed unconditionally (cheap, small, in Debian's own repo) as a
# same-page fallback for whenever Piper isn't installed/working above --
# see internal/server/sounds.go's resolveTTSBackend, which already prefers
# Piper and falls back to this automatically.

log "Checking espeak-ng (text-to-speech fallback)"
if command -v espeak-ng >/dev/null 2>&1; then
	log "espeak-ng already installed"
else
	apt_install espeak-ng || warn "couldn't install espeak-ng — the \"Create from text\" sound generator will have no fallback if Piper isn't working"
fi

# --- sox (audio conversion) --------------------------------------------------
#
# Both a manual sound upload and "Create from text" (Piper/espeak-ng
# output) go through internal/sounds.Store.Upload's sox conversion step
# before landing in the sound library -- without it, generating a sound
# succeeds but saving it fails with a clear "sox: executable file not
# found" error rather than silently doing nothing, but it's cheap and in
# Debian's own repo, so just install it upfront instead of making that
# the first thing an operator hits.

log "Checking sox (audio conversion for sound uploads and \"Create from text\")"
if command -v sox >/dev/null 2>&1; then
	log "sox already installed"
else
	apt_install sox || warn "couldn't install sox — sound file upload and \"Create from text\" saving will fail until it's installed"
fi

# TODO (later phases, per the project plan):
#   - SkywarnPlus setup -- internal/skywarnplus is ported, but install.sh
#     doesn't yet fetch it.
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
