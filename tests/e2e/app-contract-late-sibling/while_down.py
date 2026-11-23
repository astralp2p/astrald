#!/usr/bin/env python3
"""While node2 is stopped: a provider app registers on node1.

The runner runs this between stopping node2 and starting it again. A mesh app
(lib/meshapps.py) serving `hello` registers on node1 and advertises
`m2-late`. node2 is not linked, so node1's registration push and its
retries name no sibling: they go to the siblings linked at the registration
(mod/apphost/src/op_register.go PushToSiblingsRetry,
mod/user/src/push_siblings_retry.go:14-29). The script waits 12 s more, past
the 10 s first retry, so node2 comes back after the registration's first two
push rounds. It writes no facts: the oracle reads the app's state.
"""
import time

from lib import apps, meshapps
from lib.sessionio import load

PREFIX = "m2-late"
# why 12 s: the first push retry runs 10 s after the registration
# (mod/user/src/push_siblings_retry.go siblingPushBackoff)
PAST_RETRY = 12.0

nodes = load()["nodes"]
meshapps.spawn(PREFIX, nodes, ["node1"], service=PREFIX, period=0)
app = apps.get(meshapps.app_name(PREFIX, "node1"))
# why not wait_ready: the follow's boundary waits on node2, which is down
state = app.wait_state(lambda s: s.get("registered_at") and s.get("advertised_at"),
                       timeout=30)
time.sleep(PAST_RETRY)
print(f"while_down: app {state['app'][:12]}… registered on node1 while node2 is "
      f"down; waited {PAST_RETRY:.0f} s past its first push retry")
