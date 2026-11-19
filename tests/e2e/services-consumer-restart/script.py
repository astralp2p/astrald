#!/usr/bin/env python3
"""Driver: a consumer app on node1 follows a service with a reconnecting Watcher.

Both apps are long-lived processes (lib/apps.py) that outlive this driver: the
harness restarts node1 after it (test.toml `restart`), and while_down.py and
the oracle find the same processes again. Provider A on node2 advertises the
service; the consumer on node1 follows it with reach=swarm through the SDK's
Watcher started with WithReconnect, and records the Watcher's offerings,
Stale, Synced and the number of follow ends in its state file. The driver
waits until the Watcher holds provider A after a boundary, not stale, and
records what while_down.py and the oracle compare against.
"""
from lib import apps
from lib.sessionio import load, write_facts

PKG = "tests/e2e/services-consumer-restart/driver"
BIN = "services-consumer-restart-driver"
PROVIDER_A = "services-consumer-restart-provider-a"
CONSUMER = "services-consumer-restart-consumer"

doc = load()
node1, node2 = doc["nodes"]["node1"], doc["nodes"]["node2"]


def env(node):
    return {"APP_NODE_ENDPOINT": node["endpoint"], "APP_NODE_TOKEN": node["token"]}


binary = apps.build_go(PKG, BIN)
provider = apps.spawn(PROVIDER_A, [binary, "provide"], env=env(node2))
a = provider.wait_state(lambda s: s.get("advertised"), timeout=60)

consumer = apps.spawn(CONSUMER, [binary, "watch"], env=env(node1))
held = consumer.wait_state(
    lambda s: s.get("initial") and s.get("synced", 0) >= 1 and not s.get("stale")
    and a["identity"] in s.get("offerings", []), timeout=60)

write_facts({"services_consumer_restart": {
    "provider_a": a["identity"],
    "provider_a_pid": provider.pid,
    "consumer": held["identity"],
    "consumer_pid": consumer.pid,
    "node1_endpoint": node1["endpoint"],
    "node1_restarts_before": node1.get("restarts") or 0,
    "node2_restarts_before": node2.get("restarts") or 0,
    "synced_before": held["synced"],
    "ends_before": held["ends"],
}})
print(f"driver: the Watcher on node1 holds provider {a['identity'][:12]}… on node2 "
      f"after {held['synced']} boundary(ies), not stale ({held['initial']}); both "
      "apps keep running through node1's restart")
