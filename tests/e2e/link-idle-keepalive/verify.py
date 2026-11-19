#!/usr/bin/env python3
"""Oracle: an idle healthy link stays up and each end probes it once per N.

From the `idle_keepalive` fact:

- the links held still before the watch, so the watch starts from one state;
- at least one link id is held by both ends in every snapshot of the watch:
  the keepalive never closed the link both ends shared at the start;
- neither end logged a link to the other closed for `ping timeout`;
- each end received about one pong per link per idle interval N (2 s, this
  world's tcp and kcp link_idle_ping): between 0.5 and 1.5 per link per N.
  A frame from the peer within N defers a probe, so sibling traffic can
  only lower the count; an end that never probed reads 0.
"""
import tomllib
from pathlib import Path

from lib.sessionio import load

NAMES = ("node1", "node2")


def idle_interval() -> float:
    """tcp's link_idle_ping for node1 from test.toml, in seconds."""
    doc = tomllib.loads((Path(__file__).parent / "test.toml").read_text())
    value = doc["config"]["node1"]["tcp"]["link_idle_ping"]
    assert value.endswith("s"), f"link_idle_ping {value!r} is not in seconds"
    return float(value[:-1])


fact = load()["facts"]["idle_keepalive"]
n = idle_interval()

for name, st in fact["taps"].items():
    assert st["refused"] is None and st["broken"] is None, (
        f"{name}'s log tap is not whole ({st}); the ping count is void")

assert fact["settled"], f"the links never held still before the watch: {fact['baseline']}"
snaps = fact["snapshots"]
assert snaps, "the driver took no snapshot"
kept = set(fact["baseline"][NAMES[0]]) & set(fact["baseline"][NAMES[1]])
for s in snaps:
    kept &= set(s[NAMES[0]]) & set(s[NAMES[1]])
assert kept, (
    f"no link both ends held at the start survived the {fact['watched']} s "
    f"idle watch: baseline {fact['baseline']}, last {snaps[-1]}")

for name in NAMES:
    timeouts = [line for line in fact["closed"][name] if "ping timeout" in line]
    assert not timeouts, f"{name} closed an idle healthy link for ping timeout: {timeouts}"

for name in NAMES:
    links = sum(len(s[name]) for s in snaps) / len(snaps)
    rate = fact["pings"][name] / (links * fact["watched"] / n)
    print(f"oracle: {name} received {fact['pings'][name]} pongs over "
          f"{links:.1f} links in {fact['watched']} s: {rate:.2f} per link per N")
    assert 0.5 <= rate <= 1.5, (
        f"{name} received {fact['pings'][name]} pongs over {links:.1f} links "
        f"in {fact['watched']} s with N {n} s: {rate:.2f} per link per N, "
        "want about 1")

print(f"oracle: link {sorted(kept)} stayed up idle for {fact['watched']} s; "
      "each end probed it about once per N")
