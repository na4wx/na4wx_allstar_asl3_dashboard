#!/bin/bash
# Run this ON THE NODE, as root, from inside the cloned repo (Debian --
# ASL3's OS). Optionally bootstraps ASL3 itself if it isn't already
# present, makes sure the tools needed to build this project are
# installed, pulls the latest code if there is any, then builds
# natively and redeploys via deploy/install.sh.
#
# Usage: sudo ./install.sh

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

# Recorded so the running app itself can find this checkout later --
# see internal/server/update.go, which reads this to power the System
# page's "Check for updates" button (git fetch/compare, then re-run
# this exact script to actually update). Written early and
# unconditionally, before anything below can fail, so even an
# interrupted first-time install still leaves the app able to find its
# own source for next time.
mkdir -p /etc/asl3-gui
echo "$REPO_ROOT" > /etc/asl3-gui/repo-dir

# This script always runs as root (checked above), but the checkout is
# normally cloned by whatever regular user ran `sudo ./install.sh` --
# without this, every git command below (this run's own "pull latest"
# section, and every future run of this same script triggered by the
# System page's "Check for updates" -> "Run update now" button, which
# re-invokes this script the exact same way) fails outright with git's
# own "dubious ownership" refusal (exit 128) the moment root touches a
# repo it doesn't own. --global (root's own ~/.gitconfig) rather than
# scoped to one command, since install.sh itself has no single call
# site to attach a one-off override to the way internal/server/update.go's
# own read-only git calls do.
git config --global --add safe.directory "$REPO_ROOT"

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

# --- ASL3 (AllStarLink 3) itself, if not already present --------------------
#
# This dashboard configures an ASL3 node; it doesn't require ASL3 to live on
# this exact machine unless this IS meant to be that node. Bonus feature per
# the project plan: offer to install ASL3 itself first, gated behind an
# explicit yes/no prompt like the SkywarnPlus section below -- a much bigger,
# more consequential action (adds a third-party apt repo, replaces/installs
# Asterisk, builds a DAHDI kernel module via dkms) than anything else this
# script does, so it never runs silently.
#
# Real repo/package names confirmed against AllStarLink's own install docs
# (https://allstarlink.github.io/install/debian/install/): a one-time .deb
# adds the repo (a separate signed package per Debian release, since it
# carries the release-specific apt source line + GPG key), then the "asl3"
# metapackage pulls in asl3-asterisk(-config/-doc/-modules), the DAHDI
# kernel module (dahdi-dkms/dahdi-linux/dahdi-source) and its build tooling,
# and asl3-menu.

ASL3_REPO_PKG_BASE="https://repo.allstarlink.org/public/asl-apt-repos"

if dpkg -s asl3-asterisk >/dev/null 2>&1; then
	log "ASL3 (asl3-asterisk) already installed"
elif [ ! -t 0 ]; then
	log "Skipping ASL3 install prompt (no interactive terminal attached)"
else
	echo
	echo "AllStarLink 3 (ASL3 -- Asterisk with app_rpt) doesn't appear to be installed"
	echo "on this system. This dashboard only needs to run somewhere that can reach"
	echo "an ASL3 node's /etc/asterisk -- normally that's this same machine."
	read -r -p "Install ASL3 now (adds AllStarLink's apt repo, installs the asl3 metapackage)? [y/N] " ASL3_ANSWER
	case "$ASL3_ANSWER" in
		[yY]|[yY][eE][sS])
			CODENAME=$(. /etc/os-release && echo "$VERSION_CODENAME")
			case "$CODENAME" in
				bookworm) ASL3_REPO_SUFFIX="deb12_all.deb" ;;
				trixie)   ASL3_REPO_SUFFIX="deb13_all.deb" ;;
				*)
					warn "ASL3's apt repo only publishes packages for Debian 12 (bookworm) and 13 (trixie); this system reports \"$CODENAME\" -- skipping ASL3 install. See https://allstarlink.github.io/install/debian/install/ to install it manually."
					ASL3_REPO_SUFFIX=""
					;;
			esac
			if [ -n "$ASL3_REPO_SUFFIX" ]; then
				log "Adding AllStarLink's apt repo ($CODENAME)"
				TMP=$(mktemp -d)
				REPO_DEB="asl-apt-repos.$ASL3_REPO_SUFFIX"
				if fetch "$ASL3_REPO_PKG_BASE.$ASL3_REPO_SUFFIX" "$TMP/$REPO_DEB" && dpkg -i "$TMP/$REPO_DEB"; then
					apt-get update -qq
					log "Installing the asl3 metapackage (this builds the DAHDI kernel module via dkms and can take several minutes)"
					# Deliberately not apt_install (which strips
					# --no-install-recommends) -- the asl3 metapackage's own
					# DAHDI/build-tooling pieces need to actually land, not
					# just its hard Depends.
					if apt-get install -y asl3; then
						log "Installed ASL3"
						if dpkg -s asl3-asterisk >/dev/null 2>&1; then
							warn "ASL3 was just installed -- if the DAHDI kernel module (dahdi-dkms) failed to load, a reboot usually fixes it. Check with: systemctl status asterisk"
						fi
					else
						warn "installing the asl3 metapackage failed -- check apt's own output above, or see https://allstarlink.github.io/install/debian/install/"
					fi
				else
					warn "couldn't add AllStarLink's apt repo (offline, or dpkg -i failed) -- re-run this script with network access, or see https://allstarlink.github.io/install/debian/install/"
				fi
				rm -rf "$TMP"
			fi
			;;
		*)
			log "Skipping ASL3 install"
			;;
	esac
fi

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

# Default voices downloaded below so an operator has an actual choice
# out of the box rather than just whatever shipped first -- "name:
# huggingface-path" pairs, path relative to rhasspy/piper-voices' own
# repo root, without the .onnx/.onnx.json extension (confirmed against
# the real repo layout for each of these before adding it here). The
# defaultTTSVoiceName constant in internal/server/sounds.go picks
# en_US-hfc_female-medium as the pre-selected option in the "Create
# from text" voice dropdown -- update both places together if the
# preferred default ever changes.
PIPER_DEFAULT_VOICES=(
	"en_US-lessac-medium:en/en_US/lessac/medium/en_US-lessac-medium"
	"en_US-hfc_female-medium:en/en_US/hfc_female/medium/en_US-hfc_female-medium"
	"en_US-hfc_male-medium:en/en_US/hfc_male/medium/en_US-hfc_male-medium"
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

# --- WiFi hotspot fallback ---------------------------------------------------
#
# Backs internal/wifi's automatic hotspot fallback (see that package's
# own doc comment) -- unlike the original HamVoIP app, ASL3 only ever
# talks to NetworkManager (via nmcli's own built-in hotspot support, no
# hostapd/dnsmasq needed), so the one thing actually required here is
# iptables, for the captive-portal redirect's own rules (see
# internal/wifi/captive_portal.go and dns.go): this app's dashboard
# already owns its own port, so those rules funnel just the *hotspot
# interface's* own port-80/port-53 traffic to it, rather than needing
# it to bind those ports directly.

log "Checking iptables (captive-portal redirect for the WiFi hotspot fallback)"
if command -v iptables >/dev/null 2>&1; then
	log "iptables already installed"
else
	apt_install iptables || warn "couldn't install iptables — the WiFi fallback hotspot will still come up if triggered, but joining it won't automatically pop up a sign-in prompt (browsing directly to the dashboard's address still works)"
fi

if systemctl is-active --quiet NetworkManager; then
	log "NetworkManager is active — WiFi hotspot fallback is available"
else
	warn "NetworkManager isn't active on this system — the WiFi fallback hotspot (internal/wifi.DetectBackend) won't be available until it is. If this node uses a different network stack (systemd-networkd, ifupdown, ...), switch it to NetworkManager to get this feature; ASL3 has no other supported backend."
fi

# --- SkywarnPlus (optional weather-alert automation) ------------------------
#
# A third-party, no-longer-maintained tool
# (https://github.com/Mason10198/SkywarnPlus) that announces National
# Weather Service alerts over the repeater. Unlike everything else this
# script sets up, this is entirely optional -- not everyone wants it -- so
# it's the one thing here (besides ASL3 itself, above) that asks first
# rather than just doing it.
#
# Installing it from here (rather than a button in the running web app)
# matches its own upstream installer's own shape: the real swp-install is
# heavily interactive and was never meant to be triggered from a web
# server's HTTP handler. What's below reimplements only the non-interactive
# parts it actually needs (dependencies, download, cron); the app's own
# SkywarnPlus tab configures whatever this installs (county codes, which
# node announces, feature on/off toggles) via deploy/sky_configure.py and
# SkywarnPlus's own SkyControl.py -- see internal/skywarnplus's package doc.
#
# Adapted from the original HamVoIP app's own install.sh for Debian/apt
# instead of Arch/pacman, and for ASL3's modern Debian 12/13 Python 3
# (which needs none of that script's HamVoIP-specific "bootstrap pip for a
# very outdated Python 3.5" fallback -- just python3-pip plus
# --break-system-packages, since Debian 12+ blocks a bare pip install
# outside a venv per PEP 668).

SKYWARN_DIR="/usr/local/bin/SkywarnPlus"
SKYWARN_RELEASE_VERSION="v0.8.1"

if [ -x "$SKYWARN_DIR/SkywarnPlus.py" ]; then
	log "SkywarnPlus already installed at $SKYWARN_DIR"
elif [ ! -t 0 ]; then
	log "Skipping SkywarnPlus prompt (no interactive terminal attached)"
else
	echo
	read -r -p "Install SkywarnPlus weather-alert automation? [y/N] " SKYWARN_ANSWER
	case "$SKYWARN_ANSWER" in
		[yY]|[yY][eE][sS])
			log "Installing SkywarnPlus dependencies"
			apt_install ffmpeg unzip python3-pip

			if pip3 install --quiet --break-system-packages requests python-dateutil pydub ruamel.yaml; then
				log "Installed Python dependencies via pip3"
			else
				warn "couldn't install Python dependencies for SkywarnPlus -- install them manually: pip3 install --break-system-packages requests python-dateutil pydub ruamel.yaml (see https://github.com/Mason10198/SkywarnPlus#installation)"
			fi

			log "Downloading SkywarnPlus $SKYWARN_RELEASE_VERSION"
			TMP=$(mktemp -d)
			if fetch "https://github.com/Mason10198/SkywarnPlus/releases/download/$SKYWARN_RELEASE_VERSION/SkywarnPlus.zip" "$TMP/SkywarnPlus.zip"; then
				rm -rf "$SKYWARN_DIR"
				unzip -q "$TMP/SkywarnPlus.zip" -d "$(dirname "$SKYWARN_DIR")"
				chmod +x "$SKYWARN_DIR"/*.py

				cp "$REPO_ROOT/deploy/sky_configure.py" "$SKYWARN_DIR/"
				chmod +x "$SKYWARN_DIR/sky_configure.py"

				PYTHON3_BIN=$(command -v python3 || echo /usr/bin/python3)
				echo "* * * * * root $PYTHON3_BIN $SKYWARN_DIR/SkywarnPlus.py" > /etc/cron.d/SkywarnPlus
				log "Installed SkywarnPlus to $SKYWARN_DIR, scheduled via /etc/cron.d/SkywarnPlus (every 60s)"
				log "Finish setup on the node's SkywarnPlus tab: pick your county codes and register this node."
			else
				warn "couldn't download SkywarnPlus (offline?) -- re-run this script with network access to finish installing it."
			fi
			rm -rf "$TMP"
			;;
		*)
			log "Skipping SkywarnPlus"
			;;
	esac
fi

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

# Same check the install-decision step above uses ([ -x
# ".../SkywarnPlus.py" ], not just [ -d "$SKYWARN_DIR" ]) -- so this only
# offers the "re-run to install it" tip when it's actually not installed
# (declined, no interactive terminal to ask, or a failed download), never
# when it's already there.
if [ -x "$SKYWARN_DIR/SkywarnPlus.py" ]; then
	echo "Finish SkywarnPlus setup on the node's SkywarnPlus tab (pick your county codes and register this node)."
else
	echo "Tip: SkywarnPlus (weather-alert automation) was skipped -- re-run this script anytime to install it."
fi
echo "Re-run this script anytime to update to the latest version from git."
echo
