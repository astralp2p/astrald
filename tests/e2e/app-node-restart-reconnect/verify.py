#!/usr/bin/env python3
"""Oracle: the provider app reconnected to its restarted node and serves again.

The runner restarts node2 between driver and oracle (test.toml `restart`). The
provider app is the process the driver started (lib/apps.py), still running.
Claims:

- the harness restarted node2 once and node1 not at all, and node2 came back on
  the endpoint the app dials (a local world keeps a node's ports);
- the provider process is the same one, alive: it reconnected, it was not
  started again;
- the provider's own record says so: the node completed another registration of
  its handler after the first (`attaches` grew), on the token the app already
  held;
- a fresh caller app on node1 reaches the provider by its app identity within a
  window after the restart and is answered `echo, <that caller>`, and the
  provider's record shows the query was served under the reconnected session
  (`last_served_attach` is the new attach count).

The error the caller saw while the link and the registration came back, and
the error the provider's registrar reported at the disconnect, are printed and
not judged: how long the window is depends on backoff and on the host.

The oracle runs the caller as a program (`driver call`): astral-py speaks the
pre-v1 Services wire, and an app-to-app query by identity is not one it makes.
"""
import json
import os
import subprocess
import sys

from lib import apps
from lib.sessionio import load

PKG = "tests/e2e/app-node-restart-reconnect/driver"
NAME = "app-node-restart-reconnect-provider"
WINDOW = 90

doc = load()
facts = doc["facts"]["app_node_restart_reconnect"]
node1, node2 = doc["nodes"]["node1"], doc["nodes"]["node2"]

provider = apps.get(NAME)
try:
    restarts = (node2.get("restarts") or 0) - facts["node2_restarts_before"]
    assert restarts == 1, (
        f"node2 was restarted {restarts} times between driver and oracle, not once")
    assert (node1.get("restarts") or 0) == facts["node1_restarts_before"], (
        "node1 was restarted; only node2 may be")
    assert node2["endpoint"] == facts["node2_endpoint"], (
        f"node2 came back on {node2['endpoint']}, not on {facts['node2_endpoint']} "
        "the provider dials: the harness moved the node, not astrald")

    assert provider.pid == facts["pid"] and provider.alive(), (
        f"the provider process (pid {facts['pid']}) is gone; the harness or the "
        f"app died, not a reconnect:\n{provider.log_tail()}")

    # why the call first, with its own retry window: the registrar re-binds on
    # its own backoff, so the first queries after the restart may fail, and
    # the first error is recorded below, not judged
    p = subprocess.run(
        [str(apps.build_go(PKG)), "call", facts["provider"], str(WINDOW)],
        capture_output=True, text=True,
        env={**os.environ, "APP_NODE_ENDPOINT": node1["endpoint"],
             "APP_NODE_TOKEN": node1["token"]})
    sys.stderr.write(p.stderr)
    lines = [l for l in p.stdout.splitlines() if l.startswith("{")]
    assert p.returncode == 0 and lines, (
        f"a caller app on node1 got no answer from the provider after node2 "
        f"restarted, within {WINDOW}s (exit {p.returncode}; the provider "
        f"registered its handler {provider.state().get('attaches')} times, "
        f"{facts['attaches_before']} before the restart; its registrar "
        f"reported {provider.state().get('disconnects')})")
    called = json.loads(lines[-1])
    assert called["reply"] == f"echo, {called['caller']}", (
        f"the provider answered {called['reply']!r}, not an echo naming the "
        f"caller {called['caller']}")

    final = provider.state()
    want = facts["attaches_before"] + 1
    assert final["pid"] == facts["pid"], "the provider's state is from another process"
    assert final["attaches"] >= want, (
        f"the node completed {final['attaches']} registrations of the provider's "
        f"handler, not another after the {facts['attaches_before']} before the restart")
    assert final["served"] > facts["served_before"], (
        f"the provider's record shows no query served since the restart "
        f"(served {final['served']}, {facts['served_before']} before)")
    assert final["last_served_attach"] >= want, (
        f"the provider answered under attach {final['last_served_attach']}, "
        f"not under a re-registration (attach {want} or later)")

    print(f"oracle: node2 restarted once; provider pid {facts['pid']} stayed up, "
          f"re-registered ({facts['attaches_before']} -> {final['attaches']} attaches) and "
          f"answered a caller on node1 as {called['caller'][:12]}…")
    print(f"note: caller attempts after the restart {called['attempts']}, first "
          f"error: {called['first_error'] or '(none)'}")
    print(f"note: provider registrar errors at disconnect: "
          f"{final['disconnects'] or '(none)'}")
finally:
    provider.stop()
