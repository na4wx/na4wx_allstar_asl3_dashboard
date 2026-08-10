# NA4WX Allstar ASL3 Dashboard

A browser-based configuration dashboard for [AllStarLink 3](https://allstarlink.github.io/) (ASL3) nodes — a from-scratch port of [na4wx_allstar_dashboard](https://github.com/na4wx/na4wx_allstar_dashboard), which targets the older HamVoIP distribution instead. Single static Go binary, no database, no Node/npm build step — it reads and writes ASL3's own Asterisk config files directly and talks to `asterisk -rx` for live status.

## Get started

On a real ASL3 node (Debian 12/13, amd64 or arm64), as root, from a clone of this repo:

```bash
sudo apt install -y git
git clone https://github.com/na4wx/na4wx_allstar_asl3_dashboard.git
cd na4wx_allstar_asl3_dashboard
sudo ./install.sh
```

This one script installs everything needed and gets you running:

- **Installed automatically, no prompt:** the Go toolchain (if missing/too old), [Piper](https://github.com/rhasspy/piper) plus three starter voices, [espeak-ng](https://github.com/espeak-ng/espeak-ng) (Piper's fallback), and [sox](https://sourceforge.net/projects/sox/) — everything the Sounds & Tones tab's "Create from text" and file-upload features need.
- **Offered with a yes/no prompt, since they're bigger changes:** [AllStarLink 3](https://allstarlink.github.io/) itself, if this machine doesn't already have it (adds AllStarLink's apt repo, installs Asterisk + app_rpt + the DAHDI kernel module); and [SkywarnPlus](https://github.com/Mason10198/SkywarnPlus), a third-party weather-alert add-on — skip either one and re-run the script later to install it.
- Finally, it builds `asl3-gui` and installs it as a systemd service (`asl3-gui.service`) listening on port `8089`.

`install.sh` is also how you update later — re-run it any time to pull the latest code and redeploy; it skips anything already installed.

When it finishes, it prints the URL to open (e.g. `http://<node-ip>:8089/setup`). The first visit walks you through creating the admin account; every page after that requires login. If you installed SkywarnPlus, finish its setup on the node's own SkywarnPlus tab (pick county codes, register the node).

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

## Credits

This dashboard just configures and displays what these projects already do — full credit to their own authors and maintainers:

- **[AllStarLink 3](https://www.allstarlink.org/) / [app_rpt](https://github.com/AllStarLink/asl3-app_rpt)** — the Asterisk-based radio-linking platform this entire app is a control panel for. `install.sh` can install it for you; see [AllStarLink's own install docs](https://allstarlink.github.io/install/debian/install/).
- **[na4wx_allstar_dashboard](https://github.com/na4wx/na4wx_allstar_dashboard)** — the original dashboard this project is a from-scratch ASL3 port of.
- **[SkywarnPlus](https://github.com/Mason10198/SkywarnPlus)** by Mason10198 — the third-party National Weather Service alert automation the SkywarnPlus tab configures. Optional, installed only if you say yes at the `install.sh` prompt; this app doesn't modify or redistribute it.
- **[Piper](https://github.com/rhasspy/piper)** by the Rhasspy project — the offline text-to-speech engine behind "Create from text" on the Sounds & Tones tab, with **[espeak-ng](https://github.com/espeak-ng/espeak-ng)** as its fallback when Piper isn't available. Voice models come from the [rhasspy/piper-voices](https://huggingface.co/rhasspy/piper-voices) collection.
- **[SoX](https://sourceforge.net/projects/sox/)** — does the audio format conversion behind every sound upload and generated prompt.

## License

GPLv3 — see [LICENSE](LICENSE).
