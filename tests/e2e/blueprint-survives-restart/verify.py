#!/usr/bin/env python3
"""Oracle: after node1 restarts, the reconnecting app syncs its types again and
node1 decodes them again.

The runner restarts node1 between driver and oracle (test.toml `restart`). The
app is the process the driver started (lib/apps.py), still running. Claims:

- before the restart the sync had worked: node1 listed both types, returned
  their declared schemas, and an object of one was stored and read back
  (facts);
- node1 was restarted once, on the endpoint the app dials, and the app is the
  same process, alive: it reconnected, it was not started again;
- the app's own record shows another completed sync (`syncs` grew), and node1
  lists both types again, within a window;
- node1 returns the schemas the app declared, and a fresh process, whose own
  registry starts empty, cannot read the object stored before the restart until
  it learns the type from node1, and then decodes it to what was stored.

Recorded, not judged: whether node1 lists a type right after the restart that
no app syncs (`control`, registered by the User before the restart). That is
the open question of whether blueprints persist: they are held in the node's
in-memory registry and no persistence is known, so this build's answer is
printed. The same listing of the app's types is a clean sample only when the
note says the app had not synced again; the app reconnects within a second or
two, so it usually has.
"""
import asyncio
import time

import astral
from astral.errors import BlueprintNotFound

from lib import apps
from lib.sessionio import load

NAME = "blueprint-survives-restart-app"
CONTROL = "blueprint_survives_restart.control"
NOTE = "blueprint_survives_restart.note"
TAG = "blueprint_survives_restart.tag"
WANT = {NOTE: [["Label", "string16"], ["Serial", "uint64"]],
        TAG: [["Name", "string16"], ["Rank", "uint8"]]}
WINDOW = 90


def shape(bp) -> list:
    return [[f.name, getattr(f.spec, "primitive_type", "")] for f in bp.fields]


async def judge(doc, facts, app) -> dict:
    node1 = doc["nodes"]["node1"]
    async with await astral.connect(node1["endpoint"],
                                    token=doc["facts"]["user_token"]) as c:
        # the first look, before anything is awaited: what node1 holds at
        # once, and whether the app had synced again by the time we asked
        first = await c.objects.blueprints()
        syncs_at_first_look = app.state().get("syncs", 0)
        sample = {"listed": {t: t in first for t in (NOTE, TAG)},
                  "control_listed": CONTROL in first,
                  "clean": syncs_at_first_look <= facts["syncs_before"]}

        deadline = time.monotonic() + WINDOW
        names, busy = [], None
        while True:
            try:
                names = await c.objects.blueprints()
            except astral.errors.NodeUnavailable as e:
                # why tolerated: a loaded host once left node1's apphost
                # silent for a poll; the window, not one poll, is the claim
                busy = e
            if NOTE in names and TAG in names:
                break
            assert time.monotonic() < deadline, (
                f"node1 lists {[t for t in (NOTE, TAG) if t in names]} of the "
                f"app's types {WINDOW}s after its restart; the app synced "
                f"{app.state().get('syncs')} times ({facts['syncs_before']} "
                f"before) — the types did not come back"
                f"{f' (last poll error: {busy})' if busy else ''}")
            await asyncio.sleep(1)

        for t, want in WANT.items():
            got = shape(await c.objects.get_blueprint(t))
            assert got == want, f"node1 describes {t} as {got}, not {want}"

        # why a bare read first: this process holds no schema, so the read
        # failing is this process's own control, not an inherited claim
        try:
            got = await c.objects.load(facts["object_id"])
        except BlueprintNotFound as e:
            assert NOTE in str(e), f"the read failed with {e!r}, not on {NOTE}"
        else:
            raise AssertionError(
                f"{facts['object_id'][:16]}… decoded as {got!r} in a process "
                f"that never learned {NOTE}")
        await c.objects.learn(NOTE)
        back = await c.objects.load(facts["object_id"])
    decoded = [back.ASTRAL_TYPE, back.get("Label"), int(back.get("Serial"))]
    assert decoded == [NOTE, facts["label"], facts["serial"]], (
        f"the object stored before the restart decodes as {decoded}, not "
        f"{[NOTE, facts['label'], facts['serial']]}")
    return sample


doc = load()
facts = doc["facts"]["blueprint_survives_restart"]
node1 = doc["nodes"]["node1"]

app = apps.get(NAME)
try:
    assert all(facts["listed"].values()) and "error" not in facts, (
        f"before the restart node1 listed {facts['listed']} after the app's "
        "first sync: the sync never worked, so there is nothing to survive")
    assert facts["shapes"] == WANT, (
        f"before the restart node1 described the types as {facts['shapes']}, "
        f"not {WANT}")
    assert facts["decoded"] == [NOTE, facts["label"], facts["serial"]], (
        f"before the restart the stored object read back as {facts['decoded']}")

    restarts = (node1.get("restarts") or 0) - facts["node1_restarts_before"]
    assert restarts == 1, (
        f"node1 was restarted {restarts} times between driver and oracle, not once")
    assert node1["endpoint"] == facts["node1_endpoint"], (
        f"node1 came back on {node1['endpoint']}, not on {facts['node1_endpoint']} "
        "the app dials: the harness moved the node, not astrald")
    assert app.pid == facts["pid"] and app.alive(), (
        f"the app process (pid {facts['pid']}) is gone; the harness or the app "
        f"died, not a reconnect:\n{app.log_tail()}")

    sample = asyncio.run(judge(doc, facts, app))

    final = app.state()
    assert final["pid"] == facts["pid"], "the app's state is from another process"
    assert final["syncs"] > facts["syncs_before"], (
        f"the app completed {final['syncs']} syncs, none after the "
        f"{facts['syncs_before']} before the restart")
    assert app.alive(), f"the app died during the check:\n{app.log_tail()}"

    print(f"oracle: node1 restarted once; app pid {facts['pid']} stayed up, synced "
          f"again ({facts['syncs_before']} -> {final['syncs']}), node1 lists both "
          f"types, describes them as declared, and decodes {facts['object_id'][:16]}…")
    print(f"note: control type {CONTROL} (registered by the User, synced by no app) "
          f"listed right after the restart: {sample['control_listed']}")
    print(f"note: right after the restart node1 listed {sample['listed']} "
          f"({'before the app had synced again' if sample['clean'] else 'but the app had already synced again, so this says nothing about persistence'})")
    print(f"note: app registrar errors at disconnect: {final['disconnects'] or '(none)'}")
finally:
    app.stop()
