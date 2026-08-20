#!/usr/bin/env python3
"""Check every example manifest against the published schema.

A separate file rather than a heredoc inside the workflow: a heredoc's
terminator has to sit at column zero, and everything inside a YAML block
scalar is indented, so the two do not mix.

This is not the real gate. These examples are built AND RUN by the
Multipass server's own test suite before they are ever mirrored here, and
a Go contract test holds the schema to the validator that actually
enforces it. This catches a badly assembled mirror.
"""
import glob
import json
import sys


def main() -> int:
    schema = json.load(open("manifest.schema.json", encoding="utf-8"))
    props = schema["properties"]
    required = set(schema["required"])
    families = set(props["provides"]["items"]["enum"])
    events = set(props["events"]["items"]["enum"])
    caps = set(props["capabilities"]["properties"])
    versions = props["apiVersion"]["enum"]

    paths = sorted(glob.glob("examples/*/manifest.json"))
    if not paths:
        print("no example manifests found")
        return 1

    bad = 0
    for path in paths:
        m = json.load(open(path, encoding="utf-8"))
        missing = required - set(m)
        if missing:
            print(f"{path}: missing {sorted(missing)}")
            bad += 1
        for f in m.get("provides", []):
            if f not in families:
                print(f"{path}: unknown family {f!r}")
                bad += 1
        for e in m.get("events", []):
            if e not in events:
                print(f"{path}: unknown event {e!r}")
                bad += 1
        for c in m.get("capabilities", {}):
            if c not in caps:
                print(f"{path}: unknown capability {c!r}")
                bad += 1
        if m.get("apiVersion") not in versions:
            print(f"{path}: apiVersion {m.get('apiVersion')!r} is not published")
            bad += 1
        # An owner reads this before deciding to install.
        if not m.get("description"):
            print(f"{path}: no description")
            bad += 1

    print(f"checked {len(paths)} manifest(s)")
    return 1 if bad else 0


if __name__ == "__main__":
    sys.exit(main())
