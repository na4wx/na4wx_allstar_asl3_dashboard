# ASL3 Config GUI

A browser-based configuration dashboard for [AllStarLink 3](https://allstarlink.github.io/) (ASL3) nodes — a from-scratch port of [na4wx_allstar_dashboard](https://github.com/na4wx/na4wx_allstar_dashboard), which targets the older HamVoIP distribution instead. Single static Go binary, no database, no Node/npm build step — it reads and writes ASL3's own Asterisk config files directly and talks to `asterisk -rx` for live status.

## Get started

On a real ASL3 node (Debian 12/13, amd64 or arm64), as root, from a clone of this repo:

```bash
git clone https://github.com/na4wx/na4wx_allstar_asl3_dashboard.git
cd na4wx_allstar_asl3_dashboard
sudo ./install.sh
```

This one script installs Go and the other tools this project needs (Piper/espeak-ng for text-to-speech, sox for audio conversion), optionally installs ASL3 itself if it isn't already present, builds the binary, and installs it as a systemd service (`asl3-gui.service`) listening on port `8089`. It's also how you update later — re-run it any time to pull the latest code and redeploy.

When it finishes, it prints the URL to open (e.g. `http://<node-ip>:8089/setup`). The first visit walks you through creating the admin account; every page after that requires login.

If you'd rather build a binary elsewhere and copy it over, see [Cross-compiling and deploying manually](#cross-compiling-and-deploying-manually) below.

## What it does

- **Home** — live connected-station table (driven over a WebSocket, no polling/reloads), quick link/unlink/monitor to another node, and a connection history log.
- **Node directory & node editor** — add/edit/delete nodes; tabs for Setup, Radio tuning (RX/TX levels, CTCSS, SA818/DRA818 programming over serial), Allstar Network (registration), Sounds & Tones (upload, or generate with Piper/espeak-ng text-to-speech), Scheduler (timed sound playback, scheduled connections), Commands (DTMF macros and function-macro definitions), and SkywarnPlus (weather-alert automation, if installed).
- **System** — hostname, admin password, Asterisk restart/reboot, WiFi (scan/connect, with an automatic fallback hotspot when the node has no network), and Cloud Sync settings.
- **Raw config editor** — view/edit the underlying `.conf` files directly for anything the structured pages don't cover yet.
- **Cloud Sync** *(optional, opt-in)* — an outbound-only WebSocket connection to a companion cloud service for remote monitoring/administration without opening any inbound ports on the node. Off by default.

Every save is applied straight to Asterisk's config files (respecting ASL3's template-inheritance format) and, where it matters, live-reloaded via `asterisk -rx` — no separate "apply" step, and a red banner appears if a change needs a full Asterisk restart to take effect.

## Requirements

- An ASL3 node running Debian 12 (bookworm) or 13 (trixie), amd64 or arm64 — ASL3 doesn't support anything else.
- Root access to install the systemd service and read/write `/etc/asterisk`.
- Everything else (Go toolchain, Piper, espeak-ng, sox) is installed for you by `install.sh`.

The dashboard doesn't have to run on the same machine that hosts Asterisk, but normally it does — `install.sh` assumes it's being run on the node itself.

## Cross-compiling and deploying manually

From a dev machine:

```bash
make amd64   # -> bin/asl3-gui-amd64
make arm64   # -> bin/asl3-gui-arm64
```

Copy the resulting binary to the node, then run:

```bash
sudo ./deploy/install.sh /path/to/asl3-gui-<arch>
```

which installs the binary to `/usr/local/bin/asl3-gui`, installs `deploy/asl3-gui.service`, and starts it.

## Building and running locally (development)

```bash
make build   # -> bin/asl3-gui (native)
make run     # build + run with default flags
make test    # go test ./...
make fmt     # gofmt -l -w .
```

Without `-asterisk-dir` pointed at a real (or fixture) config tree, most pages will error since there's nothing to read. For local development, point it at a directory containing copies of `/etc/asterisk`'s files:

```bash
go run ./cmd/asl3-gui -asterisk-dir ./testdata/asterisk -addr :8089
```

The binary is stateless aside from a handful of small local files, all under one directory by default (`/etc/asl3-gui`, overridable per-file via flags — run `asl3-gui -h` for the full list): the admin credential file, SA818 last-known-settings, the sound-playback schedule, cloud-agent settings, and WX-tone mappings.

## Architecture notes

- **No page reloads.** After the first load, navigation and form submits go over a single WebSocket (`/ws`) that replays the request through the same handlers used for plain HTTP and swaps the rendered HTML into the page — see `internal/server/ws.go`. Every link and form still has a real `href`/`action`, so the app degrades to full page loads automatically if JavaScript or the socket is unavailable.
- **Config I/O** (`internal/config`, `internal/asteriskconf`) understands ASL3's template-inheritance config format and funnels every write through a small set of wrapper functions (`internal/config/write_hooks.go`) so things like the "Asterisk needs a restart" banner only need to hook one place, not every call site.
- **Cloud Sync** (`internal/cloudagent`) is a separate, optional outbound WebSocket client — the node dials out to the cloud service, never the other way around, so nothing needs to be exposed to the internet for it to work.
- This is a clean-room rewrite for ASL3, not a fork of the HamVoIP app: different OS (Debian vs. Arch), a native systemd Asterisk service instead of `safe_asterisk`, NetworkManager instead of wpa_supplicant+hostapd+dnsmasq, and a structurally different Asterisk config format (template inheritance, HTTP-based node registration instead of IAX2 peers).

## License

GPLv3 — see [LICENSE](LICENSE).
