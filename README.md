# ASL3 Config GUI

A browser-based configuration dashboard for AllStarLink 3 (ASL3) nodes —
a port of [na4wx_allstar_dashboard](https://github.com/na4wx/na4wx_allstar_dashboard),
which targets the older HamVoIP distribution instead.

**Status: early scaffold.** Auth, template rendering, and the fully
platform-agnostic packages (`internal/auth`, `internal/tts`,
`internal/sounds`, `internal/soundschedule`, `internal/sa818`,
`internal/skywarnplus`, `internal/nodedb`, all of `web/`) are ported and
working. Everything Asterisk-config-dependent (node management, the
System page, the Cloud Sync agent) is still being built — see the
phased plan this is built against for what's next.

ASL3 is a genuinely different platform from HamVoIP: Debian instead of
Arch, a native systemd Asterisk service instead of `safe_asterisk`, a
non-root `asterisk` user, NetworkManager instead of
wpa_supplicant+hostapd+dnsmasq, and a structurally different Asterisk
config format (template inheritance, plus HTTP-based node registration
in `rpt_http_registration.conf` instead of IAX2 peers in `iax.conf`).
This is why it's a separate application rather than a branch of the
HamVoIP one.

## Building

```
make build   # native
make amd64   # cross-compile for Debian amd64
make arm64   # cross-compile for Debian arm64
```

## Installing on a real ASL3 node

```
sudo ./install.sh
```

Installs Go if needed, builds, and deploys via `deploy/install.sh`
(installs the binary + a systemd unit, `asl3-gui.service`).
