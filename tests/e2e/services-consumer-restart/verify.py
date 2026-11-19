#!/usr/bin/env python3
"""Oracle: the consumer's Watcher reopens its follow after node1 restarts.

The runner stopped node1, ran while_down.py (which started provider B on node2
and saw the Watcher turn stale), and started node1 again. The consumer app on
node1 is the process the driver started, still running; nothing in it but the
SDK Watcher (WithReconnect) reopens the follow. Claims:

- the harness restarted node1 once and node2 not at all, and node1 came back
  on the endpoint the consumer dials;
- the consumer and both providers are the same processes, alive;
- within 60 s the Watcher lists provider B, which advertised while node1 was
  down, with provider A, after a further boundary, and Stale() is false;
- the Watcher never stopped (Err stays empty) and saw at least one follow end.
"""
import time

from lib import apps
from lib.sessionio import load

PROVIDER_A = "services-consumer-restart-provider-a"
PROVIDER_B = "services-consumer-restart-provider-b"
CONSUMER = "services-consumer-restart-consumer"
WINDOW = 60

doc = load()
facts = doc["facts"]["services_consumer_restart"]
node1, node2 = doc["nodes"]["node1"], doc["nodes"]["node2"]

consumer, pa, pb = apps.get(CONSUMER), apps.get(PROVIDER_A), apps.get(PROVIDER_B)
try:
    restarts = (node1.get("restarts") or 0) - facts["node1_restarts_before"]
    assert restarts == 1, f"node1 was restarted {restarts} times, not once"
    assert (node2.get("restarts") or 0) == facts["node2_restarts_before"], (
        "node2 was restarted; only node1 may be")
    assert node1["endpoint"] == facts["node1_endpoint"], (
        f"node1 came back on {node1['endpoint']}, not on {facts['node1_endpoint']} "
        "the consumer dials: the harness moved the node, not astrald")
    assert consumer.pid == facts["consumer_pid"] and consumer.alive(), (
        f"the consumer process (pid {facts['consumer_pid']}) is gone:\n{consumer.log_tail()}")
    assert pa.pid == facts["provider_a_pid"] and pa.alive(), (
        f"provider A (pid {facts['provider_a_pid']}) is gone:\n{pa.log_tail()}")
    assert pb.alive(), f"provider B is gone:\n{pb.log_tail()}"
    b = pb.state()["identity"]
    a = facts["provider_a"]

    start = time.monotonic()
    try:
        state = consumer.wait_state(
            lambda s: b in s["offerings"] and not s["stale"]
            and s["synced"] > facts["synced_before"], timeout=WINDOW)
    except apps.AppError as e:
        raise AssertionError(
            f"within {WINDOW}s of node1's return the Watcher on node1 did not list "
            f"provider B, after a boundary, with Stale() false: {e}")
    took = time.monotonic() - start
    assert a in state["offerings"], (
        f"the Watcher lost provider A, which never left: {state['offerings']}")
    assert not state["err"], f"the Watcher stopped: {state['err']}"
    assert state["ends"] > facts["ends_before"], (
        "the Watcher never saw its follow end; node1's restart did not reach it")
    print(f"oracle: node1 restarted once; {took:.1f}s after its return the Watcher "
          f"lists provider B {b[:12]}… with provider A, not stale, after "
          f"{state['synced'] - facts['synced_before']} more boundary(ies) and "
          f"{state['ends'] - facts['ends_before']} follow end(s)")
finally:
    consumer.stop()
    pa.stop()
    pb.stop()
