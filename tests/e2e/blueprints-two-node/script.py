#!/usr/bin/env python3
"""Driver: teach node1 a type nothing was built against, and store one.

A blueprint is a runtime type schema. `objects.register_blueprint` adds one to
a node and the whole point is that a type no SDK shipped can still travel — so
this registers a type that exists nowhere but this file, stores an object of
it on node1, and leaves the crossing to the oracle.

Nothing is done to node2. That is the test: the schema is per-node state and
nothing replicates it, so node2 stays ignorant of the type and the oracle has
to pull the schema over the link before it can read anything. Whether node2 is
in fact ignorant is the oracle's to assert — a driver that checked it would
report an astrald anomaly as a flow that never happened.

The re-registration check rides along because it costs one call and settles a
question the test document left open — whether registering a name twice is
refused or silently replaces the first. It is refused.
"""
import asyncio

import astral
from astral.blueprint import Blueprint, Field, PrimitiveSpec, RuntimeRecord

from lib.sessionio import load, write_facts

TYPE = "test.blueprints.probe"
LABEL = "a type the SDK was never built against"
SERIAL = 20260813


def probe_blueprint() -> Blueprint:
    """Two fields, two primitives: enough shape to decode wrongly if it can."""
    return Blueprint(
        type=TYPE,
        fields=[
            Field(name="Label", spec=PrimitiveSpec(primitive_type="string16")),
            Field(name="Serial", spec=PrimitiveSpec(primitive_type="uint64")),
        ],
        underlying="",
    )


async def main():
    doc = load()
    n1 = doc["nodes"]["node1"]

    async with await astral.connect(n1["endpoint"],
                                    token=doc["facts"]["user_token"]) as c1:
        # why tolerant: a world reused through --keep or --target stage: has
        # the type already, and a driver that died here would report the
        # world's history as a flow failure.
        try:
            await c1.objects.register_blueprint(probe_blueprint())
        except astral.errors.RemoteError as e:
            if "already registered" not in str(e):
                raise

        # Settles an open question: a name already registered is refused, not
        # replaced. A registry that silently took the second definition would
        # let any caller redefine a live type's wire shape.
        try:
            await c1.objects.register_blueprint(probe_blueprint())
            reregistered = "accepted"
        except astral.errors.RemoteError as e:
            reregistered = f"refused: {e}"

        record = RuntimeRecord(probe_blueprint())
        record.set("Label", LABEL)
        record.set("Serial", SERIAL)
        object_id = str((await c1.objects.store(record, repo="local"))[0])

    # why these key names: facts are merged flat across the whole chain
    # (tests/lib/session.py), so a bare `object_id` would overwrite the one
    # object-store saved. object-store-peer learned this first and named its
    # own `peer_object_id`.
    write_facts({
        "probe_type": TYPE,
        "probe_label": LABEL,
        "probe_serial": SERIAL,
        "probe_object_id": object_id,
        "reregistered": reregistered,
    })
    print(f"driver: node1 learned {TYPE} and holds {object_id[:16]}…; "
          f"re-registration {reregistered.split(':')[0]}")


asyncio.run(main())
