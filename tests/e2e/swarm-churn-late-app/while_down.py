#!/usr/bin/env python3
"""While node2 is down: register the `f3-late` mesh app on node1.

The app serves service `f3`, as the chain's other apps, so every app's
follow reports it. The script waits until the app registered its handler and
advertised, then LINGER seconds more, so every registration push it starts
to node2 has failed and retried once before node2 returns. It writes no
facts: the oracle reads the late app's identity from its state file.
"""
import time

from lib import apps, meshapps
from lib.sessionio import load

PREFIX = "f3"
# why 12 s: past the first retry of a registration push, 10 s after the first
# attempt (ticket qArE8Rfm7T)
LINGER = 12.0

node1 = load()["nodes"]["node1"]
argv = meshapps.serve_argv(PREFIX, period=1.0, keep_removed=True)
app = apps.spawn(meshapps.app_name(PREFIX, "late"), [meshapps.binary(), *argv],
                 env=meshapps.node_env(node1))
state = app.wait_state(lambda s: bool(s.get("registered_at") and s.get("advertised_at")),
                       timeout=30)
time.sleep(LINGER)
print(f"while_down: the late app {state['app']} registered on node1 and advertised "
      f"{LINGER:.0f} s before node2 returns")
