#!/usr/bin/env python3
"""Oracle: the app's own types were synced to node2 and decoded where decoding happens.

Two parts. The facts the driver recorded say what the consumer saw: the
typed reply and the typed offering info, decoded from the schemas, and that
the provider had synced before it advertised. The nodes themselves say what
they hold: node2 the blueprints the app pushed at start, node1 the ones the
operator taught it, each with the shape the app declared.
"""
import asyncio

import astral

from lib.sessionio import load

OFFER = "app_blueprint_sync.offer"
READING = "app_blueprint_sync.reading"
SHAPES = {
    OFFER: (("Label", "string16"), ("Tier", "uint8")),
    READING: (("Label", "string16"), ("Serial", "uint64")),
}


async def shapes_on(endpoint: str, token: str, where: str) -> dict:
    """The shape of each app type that `where` holds, or the node's refusal."""
    async with await astral.connect(endpoint, token=token) as c:
        names = await c.objects.blueprints()
        held = {}
        for t in SHAPES:
            assert t in names, f"{where} does not hold {t}"
            bp = await c.objects.get_blueprint(t)
            held[t] = tuple((f.name, getattr(f.spec, "primitive_type", ""))
                            for f in bp.fields)
    return held


async def main():
    doc = load()
    n1, n2 = doc["nodes"]["node1"], doc["nodes"]["node2"]
    facts = doc["facts"]
    f = facts["app_blueprint_sync"]

    # 1. What the consumer saw.
    assert f.get("types_held_before_advertise") is True, (
        "node2 did not list both app types when the provider's advertise hook "
        f"ran after its sync hook: {f.get('types_held_before_advertise')!r}")

    assert not f.get("error"), f"the flow stopped: {f['error']}"

    err = f.get("reply_before_learning_error", "")
    assert "blueprint not found" in err and READING in err, (
        f"the consumer's decode of the reply before it held the schema failed "
        f"with {err!r}, not with a missing blueprint for {READING}")
    assert f.get("node1_held_types_at_reply") is False, (
        "node1 already held an app type when the reply was decoded, so the "
        "decode in the consumer's process was not shown to need only its own schema")
    assert f.get("reply_error") == "<nil>", (
        f"the reply did not decode once the consumer held the schema: {f.get('reply_error')!r}")
    assert (f.get("reply_type"), f.get("reply_label"), f.get("reply_serial")) == (
        READING, "typed reply of its own", "20261110"), (
        f"the reply decoded as {f.get('reply_type')!r} {f.get('reply_label')!r} "
        f"{f.get('reply_serial')!r}")

    assert f.get("found_the_provider") is True, (
        "the consumer's discovery did not find the provider alone with a "
        "complete outcome once node1 held the schemas")
    assert (f.get("info_type"), f.get("info_label"), f.get("info_tier")) == (
        OFFER, "a type no SDK ships", "7"), (
        f"the offering info decoded as {f.get('info_type')!r} "
        f"{f.get('info_label')!r} {f.get('info_tier')!r}")

    # 2. What the nodes hold, as the User. node2 is asked as a sibling of the
    # User, as blueprints-two-node does: the node's own token is not the User.
    async with await astral.connect(n2["endpoint"], token=n2["token"]) as c2:
        sibling = await c2.apphost.create_token(facts["user_id"])
    on_node2 = await shapes_on(n2["endpoint"], sibling.token, "node2")
    on_node1 = await shapes_on(n1["endpoint"], facts["user_token"], "node1")
    for where, held in (("node2", on_node2), ("node1", on_node1)):
        assert held == SHAPES, f"{where} holds {held}, not {SHAPES}"

    print(f"oracle: the app synced {OFFER} and {READING} to node2 before it "
          "advertised; the reply decoded in the consumer's process while node1 "
          "held neither, and the offering info decoded once node1 held both "
          f"(discovery with the schemas in the process only found the provider: "
          f"{f.get('discover_with_types_in_process_only_found')})")


asyncio.run(main())
