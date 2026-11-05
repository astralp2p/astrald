#!/usr/bin/env python3
"""The capture capture-ether starts in a guest: UDP 8822 on every interface, then a record in the node's tree.

Runs as a transient systemd unit, as root, with the node's apphost token in
its environment. It captures for --for seconds, then writes one JSON record
as a string32 to /e2e/capture/<name> in the node's tree, with the guest's own
astral-query, and to /var/lib/astral-tests/capture-<name>.json:

    {"for", "start", "end",            # the window, guest clock, epoch seconds
     "own": [ip, ...],                 # the guest's IPv4 addresses at the start
     "sent": [{"t", "src", "dst", "len", "line"}, ...],   # from an own address
     "sent_total", "heard": {src: {"first", "last", "count"}}, "unparsed"}

why by source address and not tcpdump's direction column: the column is
printed only with `-i any` on tcpdump 4.99 and later, and a datagram the
guest sends carries one of its own addresses whatever prints it.
why the guest clock: the node logs on the same clock, so an oracle can set a
datagram against a log line without a clock the host shares.
"""
import argparse
import json
import re
import subprocess
import time
from pathlib import Path

DIR = Path("/var/lib/astral-tests")
LINE = re.compile(r"^(?P<t>\d+\.\d+)\s+(?:\S+\s+(?:In|Out|B|M|P)\s+)?IP\s+"
                  r"(?P<src>\d+\.\d+\.\d+\.\d+)\.\d+\s+>\s+"
                  r"(?P<dst>\d+\.\d+\.\d+\.\d+)\.\d+:\s+UDP,\s+length\s+(?P<len>\d+)")
SENT_CAP = 500


def own_addresses() -> list:
    out = subprocess.run(["ip", "-o", "-4", "addr", "show"], capture_output=True,
                         text=True, check=True).stdout
    return sorted({f.split("/")[0] for line in out.splitlines()
                   for f in line.split()[3:4]})


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--for", dest="span", type=int, required=True)
    ap.add_argument("--name", required=True)
    args = ap.parse_args()

    raw, err = DIR / f"capture-{args.name}.txt", DIR / f"capture-{args.name}.err"
    ready, done = DIR / f"capture-{args.name}.ready", DIR / f"capture-{args.name}.json"
    own = own_addresses()
    with raw.open("w") as out, err.open("w") as er:
        p = subprocess.Popen(["tcpdump", "-i", "any", "-n", "-tt", "-l",
                              "udp port 8822"], stdout=out, stderr=er)
        start = time.time()
        for _ in range(100):
            if "listening on" in err.read_text(errors="replace"):
                break
            time.sleep(0.1)
        else:
            # note: no ready file, so capture-ether fails rather than return
            p.terminate()
            raise SystemExit(f"tcpdump never listened: {err.read_text(errors='replace')}")
        ready.write_text(f"{start}\n")
        try:
            p.wait(timeout=args.span)
        except subprocess.TimeoutExpired:
            p.terminate()
            p.wait()
        end = time.time()

    sent, heard, unparsed = [], {}, 0
    for line in raw.read_text(errors="replace").splitlines():
        m = LINE.match(line)
        if not m:
            unparsed += bool(line.strip())
            continue
        t, src = float(m.group("t")), m.group("src")
        if src in own:
            sent.append({"t": t, "src": src, "dst": m.group("dst"),
                         "len": int(m.group("len")), "line": line})
        else:
            h = heard.setdefault(src, {"first": t, "last": t, "count": 0})
            h["last"], h["count"] = t, h["count"] + 1
    record = {"for": args.span, "start": start, "end": end, "own": own,
              "sent": sent[:SENT_CAP], "sent_total": len(sent), "heard": heard,
              "unparsed": unparsed}
    done.write_text(json.dumps(record, indent=2) + "\n")

    envelope = {"Type": "string32", "Object": json.dumps(record)}
    subprocess.run(["astral-query", "tree.set", "-path", f"/e2e/capture/{args.name}",
                    "-in", "json", "-out", "json", "-eos"],
                   input=json.dumps(envelope) + "\n", text=True, check=True,
                   capture_output=True, timeout=30)


if __name__ == "__main__":
    main()
