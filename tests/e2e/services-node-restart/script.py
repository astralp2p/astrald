#!/usr/bin/env python3
"""Driver: a provider app on node2 advertises a service that a follow on node1 holds.

Both apps are long-lived processes (lib/apps.py) that outlive this driver: the
harness restarts node2 after it (test.toml `restart`), and the oracle finds the
same processes again. The provider (node2) advertises the service and serves an
op. The consumer (node1) follows the service with reach=swarm and records every
event it receives in its state file, so the oracle can read what the follow saw
across the restart. The driver waits until the follow holds the provider, calls
the provider once from a fresh app on node1, and records what the oracle
compares the restart against.
"""
import json
import os
import subprocess
import sys

from lib import apps
from lib.sessionio import load, write_facts

PKG = "tests/e2e/services-node-restart/driver"
PROVIDER = "services-node-restart-provider"
CONSUMER = "services-node-restart-consumer"
SERVICE = "services-node-restart-echo"

doc = load()
node1, node2 = doc["nodes"]["node1"], doc["nodes"]["node2"]


def env(node):
    return {"APP_NODE_ENDPOINT": node["endpoint"], "APP_NODE_TOKEN": node["token"],
            "APP_HOST_IP": node["host_ip"]}


binary = apps.build_go(PKG)
provider = apps.spawn(PROVIDER, [binary, "provide"], env=env(node2))
before = provider.wait_state(
    lambda s: s.get("attaches", 0) >= 1 and s.get("advertised", 0) >= 1, timeout=60)

consumer = apps.spawn(CONSUMER, [binary, "follow"], env=env(node1))
key = f"{before['identity']}/{SERVICE}"
held = consumer.wait_state(
    lambda s: s.get("initial") and key in s.get("offerings", []), timeout=60)

p = subprocess.run(
    [str(binary), "call", before["identity"], "60"], capture_output=True,
    text=True, env={**os.environ, **env(node1)})
sys.stderr.write(p.stderr)
lines = [l for l in p.stdout.splitlines() if l.startswith("{")]
if p.returncode != 0 or not lines:
    sys.exit(f"driver: a caller on node1 got no answer from the provider "
             f"(exit {p.returncode})")
first = json.loads(lines[-1])

# why read again: the call is later than the state read, and the oracle reads
# the follow's events after this count
after_provider, after_consumer = provider.state(), consumer.state()
write_facts({"services_node_restart": {
    "service": SERVICE,
    "provider": before["identity"],
    "provider_pid": provider.pid,
    "consumer": held["identity"],
    "consumer_pid": consumer.pid,
    "node2_endpoint": node2["endpoint"],
    "node2_restarts_before": node2.get("restarts") or 0,
    "node1_restarts_before": node1.get("restarts") or 0,
    "attaches_before": after_provider["attaches"],
    "advertised_before": after_provider["advertised"],
    "events_before": len(after_consumer["events"]),
    "first_reply": first["reply"],
}})
print(f"driver: provider {before['identity'][:12]}… on node2 advertises {SERVICE}; "
      f"the follow on node1 holds it after {len(after_consumer['events'])} event(s) "
      "and it answered a call; both apps keep running through node2's restart")
