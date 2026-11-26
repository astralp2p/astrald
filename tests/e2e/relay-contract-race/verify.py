#!/usr/bin/env python3
"""Oracle: 12 provider/consumer pairs on four swarm nodes, each answered on the
first call, with no relay contract pushed.

Every step held; each consumer called each remote provider once, and every call
was answered, with no error (so no route_not_found), in under 1.5 s; each
provider served each remote consumer exactly once (no retry). Each consumer
node logged one ask for each remote provider's relay contracts and a pulled
contract for it: the push was dropped and the pull on miss routed the call.
The oracle repeats the story with fresh identities (`driver verify`).
"""
import os
import subprocess
import sys
from collections import Counter
from pathlib import Path

from lib.sessionio import load

ROOT = Path(__file__).resolve().parents[3]
NAMES = ["node1", "node2", "node3", "node4"]
STEPS = ["node3_and_node4_joined", "four_nodes_linked_pairwise",
         "follows_open_and_complete", "providers_advertised"]
BUDGET_MS = 1500

facts = load()["facts"].get("relay_contract_race", {})
steps = facts.get("steps", {})
missing = [s for s in STEPS if not steps.get(s)]
assert not missing, f"steps that did not hold: {missing}"

pairs = facts["pairs"]
want = {(c, p) for c in NAMES for p in NAMES if c != p}
got = Counter((p["consumer"], p["provider"]) for p in pairs)
assert set(got) == want and all(n == 1 for n in got.values()), \
    f"pairs called {dict(got)}; want each of the 12 ordered pairs once"
failed = [p for p in pairs if not p["answered"] or p.get("err")]
assert not failed, f"calls not answered: {failed}"
slow = [p for p in pairs if p["ms"] >= BUDGET_MS]
assert not slow, f"first calls at or over {BUDGET_MS} ms: {slow}"

assert steps.get("consumer_nodes_hold_the_contracts"), \
    f"after the calls, hosts missing: {facts['hosts_missing']}"

for name in NAMES:
    want_callers = sorted(facts["consumers"][c] for c in NAMES if c != name)
    served = sorted(facts["served"].get(name) or [])
    assert served == want_callers, \
        f"the provider on {name} served {served}; want each remote consumer once: {want_callers}"

broken = {n: t for n, t in facts["taps"].items() if t["refused"] or t["broken"]}
assert not broken, f"log taps incomplete: {broken}"
for at, by_provider in facts["logs"].items():
    for of, log in by_provider.items():
        assert len(log["asks"]) == 1, \
            f"{at} asked {len(log['asks'])} times for the provider on {of}; want 1: {log['asks']}"
        assert log["pulled"], f"{at} logged no relay contract pulled for the provider on {of}"

proc = subprocess.run(
    ["go", "run", "./tests/e2e/relay-contract-race/driver", "verify", os.environ["ASTRAL_TESTS_SESSION"]],
    cwd=ROOT, capture_output=True, text=True,
    env={**os.environ, "GOFLAGS": os.environ.get("GOFLAGS", "-buildvcs=false")},
)
sys.stderr.write(proc.stderr)
assert proc.returncode == 0, f"oracle story failed: exit {proc.returncode}"
ms = sorted(p["ms"] for p in pairs)
print(f"oracle: 12/12 pairs answered on the first call through a pull, "
      f"{ms[0]}-{ms[-1]} ms; the story held again with fresh identities")
