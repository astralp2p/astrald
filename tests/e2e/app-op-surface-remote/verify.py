#!/usr/bin/env python3
"""Oracle: what crossed the link arrived as sent.

The driver records what each call returned: a caller app on node1 queried a
provider app on node2 by the provider's app identity. The oracle holds the
values that were sent (the typed arguments, 50 integers, the 1 MiB pattern)
and compares them with what the driver recorded, byte for byte.
"""
import hashlib

from lib.sessionio import load

FACTS = "app_op_surface_remote"
ITEMS = 50
BULK = 1 << 20

TEXT = "Héllo Wörld & 日本語 = 100% + more; /slash\\ ?query #hash 'q' \"dq\" <tag> a,b\ttab"
RAW = bytes([0, 1, 2, 0xFB, 0xFF, 0xFE, ord("="), ord("+"), ord("/"), 0, 0x80])


def pattern(n):
    # the driver's pattern: byte(i*31 + i>>8 + i>>16)
    return bytes((i * 31 + (i >> 8) + (i >> 16)) & 0xFF for i in range(n))


problems = []


def check(ok, message):
    if not ok:
        problems.append(message)


session = load()
facts = session["facts"].get(FACTS, {})
failures = facts.get("failures", {})
node1, node2 = session["nodes"]["node1"], session["nodes"]["node2"]


def need(name):
    assert facts.get(name) is True, f"{name} did not hold"


def call(name):
    # a call that failed is named with the driver's reason
    if name in failures:
        problems.append(f"{name} failed: {failures[name]}")
    obs = facts.get(name)
    assert obs, f"{name} was not run"
    return obs


need("provider_serves")
need("node1_knows_provider")
provider, caller = facts["provider"], facts["caller"]
assert provider and caller and provider != caller, f"apps {provider!r} {caller!r}"
for ident in (provider, caller):
    assert ident not in (node1["identity"], node2["identity"]), \
        f"app identity {ident} is a node's identity"

# opsurface_echo: every typed argument arrives as it was sent
echo = call("echo")
assert echo["sent_who"], "the driver recorded no identity it sent"
want = {
    "text": TEXT,
    "small": -(1 << 63),
    "big": (1 << 64) - 1,
    "ratio": -0.000123456789,
    "on": True,
    "off": False,
    "raw": RAW.hex(),
    "who": echo["sent_who"],
    "note": "<unset>",
    "caller": caller,
    "target": provider,
    "eos": True,
}
altered = {k: (echo.get(k), v) for k, v in want.items() if echo.get(k) != v}
check(not altered, "the arguments arrived altered (got, want): " + repr(altered))

# opsurface_count: 50 items, in order, then EOS
count = call("count")
check(count.get("items") == list(range(ITEMS)),
      f"the stream arrived as {count.get('items')}, want 0..{ITEMS - 1}")
check(count.get("eos") is True, "the stream ended without EOS")

# opsurface_bulk: 1 MiB of raw bytes to the caller
data = pattern(BULK)
digest = hashlib.sha256(data).hexdigest()
bulk = call("bulk")
check((bulk.get("length"), bulk.get("sha256")) == (BULK, digest),
      f"the bytes to the caller arrived as {bulk.get('length')} bytes, sha256 {bulk.get('sha256')}; want {BULK}, {digest}")

# opsurface_sink: 1 MiB from the caller, as the provider received it
sink = call("sink")
check((sink.get("length"), sink.get("sha256")) == (BULK, digest),
      f"the provider received {sink.get('length')} bytes, sha256 {sink.get('sha256')}; want {BULK}, {digest}")
check(sink.get("eos") is True, "the sink answer ended without EOS")

# why one assert at the end: a failure names every call that went wrong.
assert not problems, "; ".join(dict.fromkeys(problems))

print("oracle: typed arguments, a 50-item stream and 1 MiB each way crossed the link from node1 to node2 intact")
