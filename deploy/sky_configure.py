#!/usr/bin/python3
"""
sky_configure.py - a small companion script for SkywarnPlus, installed
alongside it by this app's install.sh.

SkywarnPlus's own SkyControl.py already safely edits a fixed set of
boolean keys (enable, sayalert, sayallclear, tailmessage, courtesytone,
idchange, alertscript) via ruamel.yaml, which preserves config.yaml's
own extensive inline comments on every save -- see SkyControl.py's own
comment: "Use ruamel.yaml instead of PyYAML to preserve comments in the
config file". This script covers what SkyControl.py does not: the two
YAML list settings (Alerting.CountyCodes, Asterisk.Nodes), and the
Pushover/SkyDescribe sections (string/int fields SkyControl.py's own
fixed VALID_KEYS boolean-toggle shape can't reach) -- using the exact
same ruamel.yaml approach so comments stay preserved here too.

Deliberately avoids f-strings and argparse in favor of .format() and
manual sys.argv parsing, matching SkyControl.py's own style -- HamVoIP's
documented "very outdated" Python needs the lowest-common-denominator
syntax, not just whatever the box currently running this happens to
support.

Usage:
  sky_configure.py status                  Prints one JSON object to stdout.
  sky_configure.py set-counties C1,C2,...  Replaces Alerting.CountyCodes.
  sky_configure.py add-node <number>       Appends to Asterisk.Nodes if missing.
  sky_configure.py set-pushover <enable:true|false> <userkey> <apitoken> <debug:true|false>
                                            Replaces the whole Pushover section.
  sky_configure.py set-skydescribe <apikey> <language> <speed> <voice> <maxwords>
                                            Replaces the whole SkyDescribe section.

Exits non-zero with a message on stderr on any failure.

Known cosmetic limitation (verified against a real downloaded
config.yaml, not assumed): if a comment block for the *next* key happens
to sit directly under the list being modified, ruamel.yaml occasionally
drops that one comment block on save -- e.g. editing Asterisk.Nodes can
lose the comment above the following AudioDelay key, though AudioDelay's
own value is untouched. Every comment on keys not adjacent to an edited
list is unaffected. This is inherent to ruamel.yaml's own comment
anchoring (confirmed happening even with a no-op load-then-dump), not
something unique to this script, and SkyControl.py carries the same risk
in principle -- it just never happens to touch a list itself.
"""
import json
import sys
from pathlib import Path

from ruamel.yaml import YAML
from ruamel.yaml.comments import CommentedSeq

SCRIPT_DIR = Path(__file__).parent.absolute()
CONFIG_FILE = SCRIPT_DIR / "config.yaml"

yaml = YAML()
yaml.preserve_quotes = True
# Matches config.yaml's own list-item indent style (verified against a
# real downloaded copy: without this, every list in the file -- not just
# the ones this script edits -- re-renders in ruamel.yaml's default
# style on save, which is a harmless but needlessly noisy diff).
yaml.indent(mapping=2, sequence=4, offset=2)


def load():
    with open(str(CONFIG_FILE), "r") as f:
        return yaml.load(f)


def save(config):
    with open(str(CONFIG_FILE), "w") as f:
        yaml.dump(config, f)


def _active_alert_count(config):
    # Reads SkywarnPlus's own runtime state file (plain JSON, written by
    # every SkywarnPlus.py cron run -- not config.yaml, no ruamel.yaml
    # needed) rather than re-fetching NWS alerts ourselves: SkywarnPlus
    # already fetches and filters these to the operator's own configured
    # county codes (see its own get_alerts(COUNTY_CODES)), so this is
    # read-only reuse of work already done, not a second alert source
    # that could disagree with the first. Best-effort: a missing or
    # malformed state file (SkywarnPlus hasn't run yet, or its own
    # TmpDir was customized) reads as "no active alerts" rather than an
    # error -- failing safe to "normal" mode, never assuming WX from data
    # we couldn't actually read.
    tmpdir = (config.get("DEV", {}) or {}).get("TmpDir", "/tmp/SkywarnPlus")
    data_file = Path(tmpdir) / "data.json"
    try:
        with open(str(data_file), "r") as f:
            state = json.load(f)
        return len(state.get("active_alerts") or [])
    except Exception:
        return 0


def cmd_status():
    config = load()
    alerting = config.get("Alerting", {}) or {}
    tailmessage = config.get("Tailmessage", {}) or {}
    asterisk = config.get("Asterisk", {}) or {}
    alertscript = config.get("AlertScript", {}) or {}
    courtesytones = config.get("CourtesyTones", {}) or {}
    idchange = config.get("IDChange", {}) or {}
    pushover = config.get("Pushover", {}) or {}
    skydescribe = config.get("SkyDescribe", {}) or {}
    codes = alerting.get("CountyCodes") or []

    # CountyCodes entries can be plain strings or single-key
    # {code: "file.wav"} dicts (for county-name audio tagging) -- flatten
    # to plain codes, since that richer per-county-audio form isn't
    # something this app's UI manages.
    plain_codes = []
    for c in codes:
        if isinstance(c, dict):
            plain_codes.extend(c.keys())
        else:
            plain_codes.append(str(c))

    nodes = [str(n) for n in (asterisk.get("Nodes") or [])]

    status = {
        "enable": bool(config.get("SKYWARNPLUS", {}).get("Enable", False)),
        "sayalert": bool(alerting.get("SayAlert", False)),
        "sayallclear": bool(alerting.get("SayAllClear", False)),
        "tailmessage": bool(tailmessage.get("Enable", False)),
        "alertscript": bool(alertscript.get("Enable", False)),
        "courtesytone_enable": bool(courtesytones.get("Enable", False)),
        "idchange_enable": bool(idchange.get("Enable", False)),
        "active_alert_count": _active_alert_count(config),
        "countycodes": plain_codes,
        "nodes": nodes,
        "pushover": {
            "enable": bool(pushover.get("Enable", False)),
            "userkey": str(pushover.get("UserKey", "")),
            "apitoken": str(pushover.get("APIToken", "")),
            "debug": bool(pushover.get("Debug", False)),
        },
        "skydescribe": {
            "apikey": str(skydescribe.get("APIKey", "")),
            "language": str(skydescribe.get("Language", "en-us")),
            "speed": int(skydescribe.get("Speed", 0)),
            "voice": str(skydescribe.get("Voice", "John")),
            "maxwords": int(skydescribe.get("MaxWords", 150)),
        },
    }
    print(json.dumps(status))


def _replace_seq(parent, key, values):
    # Mutates the existing CommentedSeq in place rather than assigning a
    # plain list, so any of ruamel.yaml's own tracked formatting for that
    # specific node (as opposed to the document-wide indent style set
    # above) survives -- confirmed against a real config.yaml that this
    # matters less than the indent setting above, but it's free.
    existing = parent.get(key)
    if not isinstance(existing, CommentedSeq):
        existing = CommentedSeq()
        parent[key] = existing
    existing.clear()
    existing.extend(values)
    return existing


def cmd_set_counties(codes_arg):
    codes = [c.strip() for c in codes_arg.split(",") if c.strip()]
    config = load()
    _replace_seq(config.setdefault("Alerting", {}), "CountyCodes", codes)
    save(config)
    print("OK")


def cmd_add_node(node):
    config = load()
    asterisk = config.setdefault("Asterisk", {})
    nodes = [str(n) for n in (asterisk.get("Nodes") or [])]
    if node not in nodes:
        nodes.append(node)
    _replace_seq(asterisk, "Nodes", nodes)
    save(config)
    print("OK")


def _parse_bool(value):
    return value.strip().lower() == "true"


def cmd_set_pushover(enable_arg, userkey, apitoken, debug_arg):
    config = load()
    pushover = config.setdefault("Pushover", {})
    pushover["Enable"] = _parse_bool(enable_arg)
    pushover["UserKey"] = userkey
    pushover["APIToken"] = apitoken
    pushover["Debug"] = _parse_bool(debug_arg)
    save(config)
    print("OK")


def cmd_set_skydescribe(apikey, language, speed_arg, voice, maxwords_arg):
    config = load()
    skydescribe = config.setdefault("SkyDescribe", {})
    skydescribe["APIKey"] = apikey
    skydescribe["Language"] = language
    skydescribe["Speed"] = int(speed_arg)
    skydescribe["Voice"] = voice
    skydescribe["MaxWords"] = int(maxwords_arg)
    save(config)
    print("OK")


def main():
    if len(sys.argv) < 2:
        sys.stderr.write(
            "Usage: sky_configure.py <status|set-counties|add-node|set-pushover|set-skydescribe> [args]\n"
        )
        sys.exit(1)

    cmd = sys.argv[1]
    try:
        if cmd == "status":
            cmd_status()
        elif cmd == "set-counties":
            if len(sys.argv) != 3:
                sys.stderr.write("Usage: sky_configure.py set-counties C1,C2,...\n")
                sys.exit(1)
            cmd_set_counties(sys.argv[2])
        elif cmd == "add-node":
            if len(sys.argv) != 3:
                sys.stderr.write("Usage: sky_configure.py add-node <number>\n")
                sys.exit(1)
            cmd_add_node(sys.argv[2])
        elif cmd == "set-pushover":
            if len(sys.argv) != 6:
                sys.stderr.write(
                    "Usage: sky_configure.py set-pushover <enable> <userkey> <apitoken> <debug>\n"
                )
                sys.exit(1)
            cmd_set_pushover(sys.argv[2], sys.argv[3], sys.argv[4], sys.argv[5])
        elif cmd == "set-skydescribe":
            if len(sys.argv) != 7:
                sys.stderr.write(
                    "Usage: sky_configure.py set-skydescribe <apikey> <language> <speed> <voice> <maxwords>\n"
                )
                sys.exit(1)
            cmd_set_skydescribe(
                sys.argv[2], sys.argv[3], sys.argv[4], sys.argv[5], sys.argv[6]
            )
        else:
            sys.stderr.write("Unknown command: {}\n".format(cmd))
            sys.exit(1)
    except FileNotFoundError:
        sys.stderr.write("config.yaml not found at {}\n".format(CONFIG_FILE))
        sys.exit(1)
    except Exception as e:
        sys.stderr.write("error: {}\n".format(e))
        sys.exit(1)


if __name__ == "__main__":
    main()
