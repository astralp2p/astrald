#!/usr/bin/env python3
"""Driver: teach node1 a type nothing was built against, and store one.

A blueprint is a runtime type schema. `objects.register_blueprint` adds one to
a node and the whole point is that a type no SDK shipped can still travel — so
this registers a type that exists nowhere but this file, stores an object of
it on node1, and leaves the crossing to the oracle.

Nothing is done to node2. That is the test: the schema is per-node state and
nothing replicates it, so node2 stays ignorant of the type and the oracle has
to pull the schema over the link before it can read anything.

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
    n1, n2 = doc["nodes"]["node1"], doc["nodes"]["node2"]

    async with await astral.connect(n1["endpoint"],
                                    token=doc["facts"]["user_token"]) as c1:
        blueprint_id = str((await c1.objects.register_blueprint(
            probe_blueprint()))[0])

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

    async with await astral.connect(n2["endpoint"], token=n2["token"]) as c2:
        known_to_node2 = TYPE in await c2.objects.blueprints()

    assert not known_to_node2, (
        f"node2 already knows {TYPE} before anything crossed — this driver "
        "registered it on node1 alone, so the test has nothing left to prove")

    write_facts({
        "type": TYPE,
        "label": LABEL,
        "serial": SERIAL,
        "object_id": object_id,
        "blueprint_id": blueprint_id,
        "reregistered": reregistered,
    })
    print(f"driver: node1 learned {TYPE} and holds {object_id[:16]}…; "
          f"node2 knows the type: {known_to_node2}; "
          f"re-registration {reregistered.split(':')[0]}")


asyncio.run(main())
