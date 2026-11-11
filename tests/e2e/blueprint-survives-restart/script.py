#!/usr/bin/env python3
"""Driver: an app with types of its own syncs their blueprints to node1.

The app is a long-lived process (lib/apps.py) that outlives this driver: the
harness restarts node1 after it (test.toml `restart`), and the oracle finds the
same process again. The driver starts it, waits for its first sync, and records
what node1 holds because of it (and, as a control, a type the User
registered itself): both type names in `objects.blueprints`, their
schemas from `objects.get_blueprint`, and an object of one type, stored in
node1's `local` repository and read back with the schema learned from node1.

What node1 did is recorded, not asserted here: the oracle judges it.
"""
import asyncio

import astral
from astral.blueprint import Blueprint, Field, PrimitiveSpec, RuntimeRecord

from lib import apps
from lib.sessionio import load, write_facts

PKG = "tests/e2e/blueprint-survives-restart/driver"
NAME = "blueprint-survives-restart-app"
NOTE = "blueprint_survives_restart.note"
TAG = "blueprint_survives_restart.tag"
LABEL = "an app's own type"
SERIAL = 20261111
# registered on node1 by the User and by no app: the control for the question
# whether node1 keeps a blueprint over a restart without any app syncing it
CONTROL = "blueprint_survives_restart.control"


def shape(bp) -> list:
    return [[f.name, getattr(f.spec, "primitive_type", "")] for f in bp.fields]


async def observe(node1, user_token) -> dict:
    async with await astral.connect(node1["endpoint"], token=user_token) as c:
        await c.objects.register_blueprint(Blueprint(
            type=CONTROL, underlying="",
            fields=[Field(name="N", spec=PrimitiveSpec(primitive_type="uint8"))]))
        names = await c.objects.blueprints()
        out = {"listed": {t: t in names for t in (NOTE, TAG)}, "shapes": {},
               "control_listed": CONTROL in names}
        for t in (NOTE, TAG):
            if out["listed"][t]:
                out["shapes"][t] = shape(await c.objects.get_blueprint(t))
        if not all(out["listed"].values()):
            return out
        bp = await c.objects.learn(NOTE)
        record = RuntimeRecord(bp)
        record.set("Label", LABEL)
        record.set("Serial", SERIAL)
        out["object_id"] = str((await c.objects.store(record, repo="local"))[0])
        back = await c.objects.load(out["object_id"])
        out["decoded"] = [back.ASTRAL_TYPE, back.get("Label"), int(back.get("Serial"))]
        return out


doc = load()
node1 = doc["nodes"]["node1"]

binary = apps.build_go(PKG)
app = apps.spawn(NAME, [binary, "serve"], env={
    "APP_NODE_ENDPOINT": node1["endpoint"], "APP_NODE_TOKEN": node1["token"]})
state = app.wait_state(lambda s: s.get("syncs", 0) >= 1, timeout=60)

seen = asyncio.run(observe(node1, doc["facts"]["user_token"]))
write_facts({"blueprint_survives_restart": {
    "app": state["identity"],
    "pid": app.pid,
    "node1_endpoint": node1["endpoint"],
    "node1_restarts_before": node1.get("restarts") or 0,
    "syncs_before": app.state()["syncs"],
    "label": LABEL,
    "serial": SERIAL,
    **seen,
}})
print(f"driver: app {state['identity'][:12]}… (pid {app.pid}) synced; node1 lists "
      f"{seen['listed']}; stored {seen.get('object_id', '(nothing)')[:16]}…; "
      "the app keeps running through node1's restart")
