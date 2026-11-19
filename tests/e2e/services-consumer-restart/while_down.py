#!/usr/bin/env python3
"""While node1 is stopped: start provider B on node2.

The runner runs this between stopping node1 and starting it again. node2
runs, so provider B registers and advertises there while the consumer's node
is down: no follow open on node1 can see B, only a reopened one. Checks on the
way:

- the consumer process is the driver's and alive;
- its Watcher saw its follow end (Stale turned true) and keeps provider A,
  marked stale, rather than dropping the set;
- provider B's advertise binding is acknowledged by node2.
"""
from lib import apps
from lib.sessionio import load

PKG = "tests/e2e/services-consumer-restart/driver"
BIN = "services-consumer-restart-driver"
PROVIDER_B = "services-consumer-restart-provider-b"
CONSUMER = "services-consumer-restart-consumer"

doc = load()
facts = doc["facts"]["services_consumer_restart"]
node2 = doc["nodes"]["node2"]

consumer = apps.get(CONSUMER)
assert consumer.pid == facts["consumer_pid"] and consumer.alive(), (
    f"the consumer process (pid {facts['consumer_pid']}) is gone:\n{consumer.log_tail()}")
try:
    down = consumer.wait_state(
        lambda s: s["ends"] > facts["ends_before"] and s["stale"], timeout=30)
except apps.AppError as e:
    raise AssertionError(f"the Watcher on node1 never turned stale while node1 was down: {e}")
assert facts["provider_a"] in down["offerings"], (
    f"the Watcher dropped provider A when its follow ended: {down['offerings']}")
assert not down["err"], f"the Watcher stopped: {down['err']}"

binary = apps.build_go(PKG, BIN)
provider = apps.spawn(PROVIDER_B, [binary, "provide"], env={
    "APP_NODE_ENDPOINT": node2["endpoint"], "APP_NODE_TOKEN": node2["token"]})
b = provider.wait_state(lambda s: s.get("advertised"), timeout=60)
print(f"while_down: the Watcher holds provider A stale after {down['ends']} follow "
      f"end(s); provider {b['identity'][:12]}… advertises on node2 while node1 is down")
