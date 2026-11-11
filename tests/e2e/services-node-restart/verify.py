#!/usr/bin/env python3
"""Oracle: after node2 restarts, the follow on node1 loses the provider and gets it back.

The runner restarts node2 between driver and oracle (test.toml `restart`). The
provider app on node2 and the consumer app on node1 are the processes the driver
started (lib/apps.py), still running; node1 was not restarted, so the consumer's
follow is the one stream that spans the restart. Claims:

- the harness restarted node2 once and node1 not at all, and node2 came back on
  the endpoint the provider dials;
- the provider process is the same one and the consumer process is the same one,
  both alive, and the follow never ended;
- the follow, in the events after the driver's last count, removed the provider's
  offering when node2 went away (a Removed event naming it) and then delivered
  the same provider's offering available again, in that order;
- the provider's own record agrees: the node completed another registration of
  its handler and acknowledged another advertise binding, and asked it to
  evaluate the offering for the consumer under that new attach;
- a fresh app on node1 runs a one-shot swarm discovery and finds the same
  provider, with a complete outcome, and calls it by its identity and is
  answered `echo, <that caller>`.

How long the provider is gone depends on node2's boot, the registrar's backoff
and node1's retry of the lost member: the window is waited for and its length
is printed, not judged.

The oracle runs the callers as a program (`driver call`, `driver discover`):
astral-py speaks the pre-v1 Services wire.
"""
import json
import os
import subprocess
import sys
import time

from lib import apps
from lib.sessionio import load

PKG = "tests/e2e/services-node-restart/driver"
PROVIDER = "services-node-restart-provider"
CONSUMER = "services-node-restart-consumer"
WINDOW = 90

doc = load()
facts = doc["facts"]["services_node_restart"]
node1, node2 = doc["nodes"]["node1"], doc["nodes"]["node2"]
key = f"{facts['provider']}/{facts['service']}"


def run(command, node):
    p = subprocess.run(
        [str(apps.build_go(PKG)), *command], capture_output=True, text=True,
        env={**os.environ, "APP_NODE_ENDPOINT": node["endpoint"],
             "APP_NODE_TOKEN": node["token"]})
    sys.stderr.write(p.stderr)
    lines = [l for l in p.stdout.splitlines() if l.startswith("{")]
    return p.returncode, (json.loads(lines[-1]) if lines else None)


def removals(events):
    """Indexes of the events that remove the provider's offering."""
    return [i for i, e in enumerate(events)
            if e["kind"] == "removed" and key in e.get("detail", "").split(",")]


def returns(events, after):
    """Index of the first event after `after` that makes the offering available."""
    for i in range(after + 1, len(events)):
        e = events[i]
        if e["kind"] == "update" and e["up"] and f"{e['provider']}/{e['name']}" == key:
            return i
    return None


provider, consumer = apps.get(PROVIDER), apps.get(CONSUMER)
try:
    restarts = (node2.get("restarts") or 0) - facts["node2_restarts_before"]
    assert restarts == 1, (
        f"node2 was restarted {restarts} times between driver and oracle, not once")
    assert (node1.get("restarts") or 0) == facts["node1_restarts_before"], (
        "node1 was restarted; only node2 may be")
    assert node2["endpoint"] == facts["node2_endpoint"], (
        f"node2 came back on {node2['endpoint']}, not on {facts['node2_endpoint']} "
        "the provider dials: the harness moved the node, not astrald")

    assert provider.pid == facts["provider_pid"] and provider.alive(), (
        f"the provider process (pid {facts['provider_pid']}) is gone; the harness "
        f"or the app died:\n{provider.log_tail()}")
    assert consumer.pid == facts["consumer_pid"] and consumer.alive(), (
        f"the consumer process (pid {facts['consumer_pid']}) is gone; the harness "
        f"or the app died:\n{consumer.log_tail()}")

    # the follow's story: removed, then available again, both after the driver
    start = time.monotonic()
    seen = {}

    def story(s):
        events = s.get("events", [])
        late = events[facts["events_before"]:]
        gone = removals(late)
        seen.update(late=late, gone=gone)
        if not gone:
            return False
        back = returns(late, gone[0])
        seen["back"] = back
        return back is not None

    try:
        state = consumer.wait_state(story, timeout=WINDOW)
    except apps.AppError:
        late = seen.get("late", [])
        what = ("never removed the provider's offering when node2 went away"
                if not seen.get("gone") else
                "removed the provider's offering but never delivered it again")
        raise AssertionError(
            f"within {WINDOW}s the follow on node1 {what}; its events after the "
            f"driver's {facts['events_before']}: {late}")
    late = state["events"][facts["events_before"]:]
    gone = removals(late)[0]
    back = returns(late, gone)
    gap = (late[back]["at"] - late[gone]["at"]) / 1000
    assert not state["err"], f"the follow ended: {state['err']}"
    assert late[back]["provider"] == facts["provider"], (
        f"the offering came back from {late[back]['provider']}, not from the "
        f"same provider {facts['provider']}")
    assert key in state["offerings"], (
        f"the follow does not hold {key} now: {state['offerings']}")

    # the provider's own record: it registered and advertised again, and the
    # node evaluated the offering for the consumer under that new attach. Its
    # state file is written behind the app's answers, so the record is waited
    # for, briefly, and then judged.
    def asked_again(s):
        return [e for e in s.get("evaluations", [])
                if e["attach"] > facts["attaches_before"] and e["caller"] == facts["consumer"]]

    try:
        final = provider.wait_state(
            lambda s: s["attaches"] > facts["attaches_before"]
            and s["advertised"] > facts["advertised_before"] and asked_again(s),
            timeout=20)
    except apps.AppError:
        final = provider.state()
    assert final["pid"] == facts["provider_pid"], "the provider's state is from another process"
    assert final["attaches"] > facts["attaches_before"], (
        f"the node completed {final['attaches']} registrations of the provider's "
        f"handler, not another after the {facts['attaches_before']} before the restart")
    assert final["advertised"] > facts["advertised_before"], (
        f"the node acknowledged {final['advertised']} advertise bindings, not "
        f"another after the {facts['advertised_before']} before the restart")
    assert asked_again(final), (
        f"the node never asked the re-registered provider to evaluate the "
        f"offering for the consumer {facts['consumer']}: {final['evaluations']}")

    # a fresh discovery and a call, apart from the follow
    code, found = run(["discover", str(WINDOW)], node1)
    assert code == 0 and found, f"a fresh swarm discovery on node1 failed (exit {code})"
    assert found["complete"] and found["providers"] == [facts["provider"]], (
        f"a fresh swarm discovery on node1 found {found['providers']} "
        f"(complete {found['complete']}); want {facts['provider']} alone")
    code, called = run(["call", facts["provider"], str(WINDOW)], node1)
    assert code == 0 and called, (
        f"a caller on node1 got no answer from the provider after node2 "
        f"restarted, within {WINDOW}s (exit {code})")
    assert called["reply"] == f"echo, {called['caller']}", (
        f"the provider answered {called['reply']!r}, not an echo naming the "
        f"caller {called['caller']}")

    print(f"oracle: node2 restarted once; the follow on node1 removed "
          f"{facts['provider'][:12]}…/{facts['service']} and held it again "
          f"{gap:.1f}s later; the provider re-registered "
          f"({facts['attaches_before']} -> {final['attaches']} attaches), a fresh "
          f"discovery found it, and a caller got {called['reply'][:24]}…")
    print(f"note: events the follow recorded since the driver: "
          f"{[(e['kind'], e['up'], e.get('detail', '')) for e in late]}")
    print(f"note: provider registrar errors at disconnect: "
          f"{final['disconnects'] or '(none)'}; first call error: "
          f"{called['first_error'] or '(none)'}")
finally:
    consumer.stop()
    provider.stop()
