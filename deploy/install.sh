#!/bin/sh
# Installs asl3-gui on an ASL3 node. Run this ON THE NODE, as root (or
# via sudo), from the directory containing the cross-compiled binary
# (see the Makefile's `amd64`/`arm64` targets).
#
# Usage: sudo ./install.sh [path-to-binary]
# If no path is given, picks bin/asl3-gui-amd64 or bin/asl3-gui-arm64
# next to this script based on `uname -m`.

set -e

SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
REPO_ROOT=$(cd "$SCRIPT_DIR/.." && pwd)

BINARY="$1"
if [ -z "$BINARY" ]; then
	case "$(uname -m)" in
		aarch64|arm64)
			BINARY="$REPO_ROOT/bin/asl3-gui-arm64"
			;;
		*)
			BINARY="$REPO_ROOT/bin/asl3-gui-amd64"
			;;
	esac
fi

if [ ! -f "$BINARY" ]; then
	echo "error: binary not found at $BINARY" >&2
	echo "Build it first with 'make amd64' or 'make arm64' on your dev machine," >&2
	echo "copy it to this node, then re-run: sudo ./install.sh /path/to/binary" >&2
	exit 1
fi

if [ "$(id -u)" != "0" ]; then
	echo "error: this script must be run as root (sudo ./install.sh)" >&2
	exit 1
fi

echo "Installing $BINARY -> /usr/local/bin/asl3-gui"
install -m 0755 "$BINARY" /usr/local/bin/asl3-gui

echo "Installing systemd unit"
install -m 0644 "$SCRIPT_DIR/asl3-gui.service" /etc/systemd/system/asl3-gui.service

mkdir -p /etc/asl3-gui

systemctl daemon-reload
systemctl enable asl3-gui
systemctl restart asl3-gui

PORT=8089

echo "Allowing port $PORT through the firewall (if one is active)"
if command -v ufw >/dev/null 2>&1 && ufw status | grep -q "^Status: active"; then
	ufw allow "$PORT/tcp" >/dev/null
	echo "  ufw: allowed $PORT/tcp"
elif command -v firewall-cmd >/dev/null 2>&1 && systemctl is-active --quiet firewalld 2>/dev/null; then
	firewall-cmd --permanent --add-port="$PORT/tcp" >/dev/null
	firewall-cmd --reload >/dev/null
	echo "  firewalld: allowed $PORT/tcp"
else
	echo "  no active firewall found (checked ufw, firewalld) -- nothing to do"
fi

iface_ip() {
	ip -4 -o addr show dev "$1" 2>/dev/null | awk '{print $4}' | cut -d/ -f1 | head -n1
}

ETH0_IP=$(iface_ip eth0)
WLAN0_IP=$(iface_ip wlan0)

echo
echo "Installed and started. Visit this URL to finish setup:"
if [ -n "$ETH0_IP" ] && [ -n "$WLAN0_IP" ]; then
	echo "  http://$ETH0_IP:$PORT/setup   (Ethernet)"
	echo "  http://$WLAN0_IP:$PORT/setup   (WiFi)"
elif [ -n "$ETH0_IP" ]; then
	echo "  http://$ETH0_IP:$PORT/setup"
elif [ -n "$WLAN0_IP" ]; then
	echo "  http://$WLAN0_IP:$PORT/setup"
else
	echo "  http://<this-node-ip>:$PORT/setup"
fi
