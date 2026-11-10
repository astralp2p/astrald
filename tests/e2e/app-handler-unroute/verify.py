#!/usr/bin/env python3
"""Oracle: every leg held, and the story holds again on its own.

The oracle repeats the whole story with fresh identities (`driver verify`). A
visitor app calls other apps on node1 by their app identity. For each way an
app's session can end, the call reaches the handler before and fails with
route_not_found after, and keeps failing:

- the bind session closes while the endpoint still listens (bind cleanup);
- an app served through the SDK stops;
- an app process is killed;
- a process holding only the bind session is killed while the endpoint listens
  (bind cleanup on a dropped connection);
- a handler with no bind session whose endpoint closes is removed by the failed
  dial, and an endpoint that comes back at its address is not routed to again.

A bystander app on node1 answers after every leg.
"""
import os
import subprocess
import sys
from pathlib import Path

from lib.sessionio import load

ROOT = Path(__file__).resolve().parents[3]
STEPS = ["clean_bind_close_unroutes", "clean_app_stop_unroutes",
         "crash_app_unroutes", "crash_bind_drop_unroutes",
         "failed_dial_removes", "bystander_unaffected"]

facts = load()["facts"].get("app_handler_unroute", {})
missing = [s for s in STEPS if not facts.get(s)]
assert not missing, f"legs that did not hold: {missing}"

proc = subprocess.run(
    ["go", "run", "./tests/e2e/app-handler-unroute/driver", "verify", os.environ["ASTRAL_TESTS_SESSION"]],
    cwd=ROOT, capture_output=True, text=True,
    env={**os.environ, "GOFLAGS": os.environ.get("GOFLAGS", "-buildvcs=false")},
)
sys.stderr.write(proc.stderr)
assert proc.returncode == 0, f"oracle story failed: exit {proc.returncode}"
print("oracle: node1 stops routing to an app's handler when its session ends, on a clean stop and on a crash")
