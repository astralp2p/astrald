#!/usr/bin/env python3
"""Oracle: a type nobody shipped crosses the link, and only once its schema does.

The oracle is a fresh process, so its blueprint registry starts empty — it has
never heard of the driver's type. That makes the negative a control it performs
on itself rather than a claim it inherits: read the object first and fail, pull
the schema over the link, read it again and succeed. One process, one
connection, one difference.

Everything is asked through node2, and node2 is never taught the type. Its
registry is checked before and after and does not hold the type either time:
nothing replicates a blueprint between nodes, and nothing needs to — node2
forwards the query and carries the answer back as opaque bytes, decoding
neither. What has to learn the schema is whoever decodes, and here that is
this process.
"""
import asyncio

import astral
from astral.errors import BlueprintNotFound

from lib.sessionio import load

# The schema the driver registered, as this oracle expects to find it.
WANT_FIELDS = (("Label", "string16"), ("Serial", "uint64"))


async def main():
    doc = load()
    n1, n2 = doc["nodes"]["node1"], doc["nodes"]["node2"]
    facts = doc["facts"]
    type_name = facts["probe_type"]
    object_id = facts["probe_object_id"]

    assert "already registered" in facts["reregistered"], (
        f"registering {type_name} twice answered {facts['reregistered']!r} — a "
        "registry that takes the second definition lets any caller redefine a "
        "live type's wire shape")

    async with await astral.connect(n2["endpoint"], token=n2["token"]) as c2:
        before = await c2.objects.blueprints()
        assert type_name not in before, (
            f"node2 knows {type_name} before anything was pulled — the schema "
            "reached it by some route this test does not model")

        # 1. No schema, no reading. This is the negative, and it is this
        #    process's own state that makes it true.
        # why BlueprintNotFound alone: StreamCorrupted is also what a
        # genuinely truncated frame raises, so catching it here would swallow a
        # real framing fault as the expected negative.
        try:
            got = await c2.objects.load(object_id, target=n1["identity"])
        except BlueprintNotFound as e:
            assert type_name in str(e), (
                f"the decode failed with {e!r}, which does not name "
                f"{type_name} — that is a different failure wearing the same "
                "exception")
        else:
            raise AssertionError(
                f"decoded {object_id[:16]}… as {got!r} without ever holding a "
                f"schema for {type_name} — the blueprint is not what makes the "
                "type readable")

        # 2. Pull the schema across the link. node2 has no idea what this type
        #    is; node1 does, and the query is routed to it.
        learned = await c2.objects.learn(type_name, target=n1["identity"])
        shape = tuple((f.name, getattr(f.spec, "primitive_type", ""))
                      for f in learned.fields)
        assert shape == WANT_FIELDS, (
            f"the schema pulled for {type_name} is {shape}, not {WANT_FIELDS}")

        # 3. The same read, now that the schema is here.
        decoded = await c2.objects.load(object_id, target=n1["identity"])

        after = await c2.objects.blueprints()

    assert decoded.ASTRAL_TYPE == type_name, (
        f"{object_id[:16]}… decoded as {decoded.ASTRAL_TYPE}, not {type_name}")
    assert decoded.get("Label") == facts["probe_label"], (
        f"Label decoded as {decoded.get('Label')!r}, not "
        f"{facts['probe_label']!r}")
    assert int(decoded.get("Serial")) == facts["probe_serial"], (
        f"Serial decoded as {decoded.get('Serial')!r}, not "
        f"{facts['probe_serial']}")

    assert type_name not in after, (
        f"node2 learned {type_name} in the course of relaying it — the object "
        "crossed, so the node in the middle was supposed to stay ignorant")

    print(f"oracle: {type_name} was unreadable here, pulled over the link from "
          f"node1, and then decoded {object_id[:16]}… as "
          f"{facts['probe_label']!r}/{facts['probe_serial']} — node2 never "
          "held the type")


asyncio.run(main())
