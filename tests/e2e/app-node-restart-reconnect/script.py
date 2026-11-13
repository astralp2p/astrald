#!/usr/bin/env python3
"""Driver: a provider app on node2 serves, and a caller app on node1 reaches it.

The provider is a long-lived process (lib/apps.py) that outlives this driver:
the harness restarts node2 after it (test.toml `restart`), and the oracle finds
the same process again. The driver starts it, waits until the node has attached
its handler, calls it once from a fresh caller app on node1, and records what
the oracle compares the restart against.
"""
import json
import os
import subprocess
import sys

from lib import apps
from lib.sessionio import load, write_facts

PKG = "tests/e2e/app-node-restart-reconnect/driver"
NAME = "app-node-restart-reconnect-provider"

doc = load()
node1, node2 = doc["nodes"]["node1"], doc["nodes"]["node2"]

binary = apps.build_go(PKG)
provider = apps.spawn(NAME, [binary, "serve"], env={
    "APP_NODE_ENDPOINT": node2["endpoint"], "APP_NODE_TOKEN": node2["token"],
    "APP_HOST_IP": node2["host_ip"]})
before = provider.wait_state(lambda s: s.get("attaches", 0) >= 1, timeout=30)

p = subprocess.run(
    [str(binary), "call", before["identity"], "60"], capture_output=True,
    text=True, env={**os.environ, "APP_NODE_ENDPOINT": node1["endpoint"],
         "APP_NODE_TOKEN": node1["token"]})
sys.stderr.write(p.stderr)
lines = [l for l in p.stdout.splitlines() if l.startswith("{")]
if p.returncode != 0 or not lines:
    sys.exit(f"driver: the caller on node1 got no answer from the provider "
             f"(exit {p.returncode})")
first = json.loads(lines[-1])
after = provider.state()

write_facts({"app_node_restart_reconnect": {
    "provider": before["identity"],
    "pid": provider.pid,
    "node2_endpoint": node2["endpoint"],
    "node2_restarts_before": node2.get("restarts") or 0,
    "node1_restarts_before": node1.get("restarts") or 0,
    "attaches_before": after["attaches"],
    "served_before": after["served"],
    "first_reply": first["reply"],
}})
print(f"driver: provider {before['identity'][:12]}… (pid {provider.pid}) on node2 "
      f"served the caller on node1 ({first['attempts']} attempt(s)); it keeps "
      "running through node2's restart")
