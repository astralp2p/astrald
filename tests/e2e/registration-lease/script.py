#!/usr/bin/env python3
"""Driver: an external describer registers, goes away, and its lease lapses.

The hole this closes: an external registration used to outlive the process that
made it. `objects.register_describer` answered an `ack` and bound the registrant
until the node stopped, and only a failing authorization check ever removed an
entry -- so an app that crashed while still authorized stayed in the fan-out,
and every later `objects.describe` dialled an identity with nothing behind it.

The driver registers as a describer and then closes the connection, which is
what a crashed app looks like from the node's side: a registration with no
process behind it. It registers with a short lease so the lapse happens in
seconds rather than in the node's default hour.

Two registrants, so that expiry and renewal are separated:

  `gone`   registers, closes its connection, and is never renewed. Two describes
           bracket its lease: the node dials it while the lease is live, and
           does not dial it once the lease has lapsed.
  `kept`   registers and re-registers before its lease ends, which is what
           renewal is. A describe after its original lease would have ended
           still reaches it, so re-registering refreshed the entry rather than
           being ignored or adding a second one.

The evidence is what the node did, read from its own log, not what the calls
cost. A departed registrant is **cheap** to dial -- the route is simply not
there -- so timings do not separate these cases and are not asked to. What
separates them is whether the node reached for the registrant at all.

The SDK-side renewal loop is not exercised here; `gone` and `kept` both drive
the op directly, so what this test judges is the node's own lease and refresh.
"""
import asyncio
import json
import time

import astral
from astral.types import Duration, ObjectID

from lib.sessionio import load, write_facts

# why short: the node clamps a request down, never up, so a small lease is
# granted exactly as asked, and the lapse fits inside a test's budget.
LEASE_SECONDS = 6

# The second describe has to land in the window where `gone` has expired and
# `kept` has not, which is what makes the two registrants differ:
#
#   t=0   both register, leases end at t=6
#   t=4   `kept` renews, its lease now ends at t=10
#   t=6   `gone` expires; this world's 1s sweep removes and logs it by t=7
#   t=8   the second describe: `gone` is two seconds gone, `kept` has two left
#
# Both margins are two seconds, so neither side is a race.
RENEW_AT = 4
SECOND_DESCRIBE_AT = 4

# why a minted identity and not the node's token: the op refuses to register the
# node as a describer of its own objects (ErrExternalRegistrationSelf), and the
# node's token authenticates as the node. Registering is also gated on
# serve_objects, which apphost.register grants in the call that mints.
SERVE_OBJECTS = "mod.auth.serve_objects_action"

# Any well-formed id. Nothing has to resolve: the fan-out reaches every
# registered describer whatever the object is, and the fan-out is the subject.
TARGET = ObjectID.parse(
    "data1aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
)


def _docs(raw: bytes) -> list:
    """The objects an op streamed, unwrapped, with keys lowered."""
    out = []
    for line in raw.decode().splitlines():
        if not line.strip():
            continue
        obj = json.loads(line).get("Object")
        if isinstance(obj, dict):
            out.append({k.lower(): v for k, v in obj.items()})
    return out


async def mint_describer(node) -> dict:
    """A fresh identity holding serve_objects, which is what registering needs.

    why call_raw: `apphost.register`'s typed client takes no op arguments, so
    grant_permits has to go on the query string by hand.
    """
    return _docs(await node.call_raw(
        f"apphost.register?grant_permits={SERVE_OBJECTS}&out=json"))[0]


async def timed_describe(client) -> float:
    """Run one objects.describe to completion and report what it cost."""
    started = time.monotonic()
    await client.objects.describe(TARGET)
    return time.monotonic() - started


async def main():
    n1 = load()["nodes"]["node1"]
    facts = {"lease_seconds": LEASE_SECONDS}

    async with await astral.connect(n1["endpoint"], token=n1["token"]) as node:
        gone = await mint_describer(node)
        kept = await mint_describer(node)

    facts["gone_identity"] = gone["identity"]
    facts["kept_identity"] = kept["identity"]
    print(f"driver: gone={gone['identity'][:8]} kept={kept['identity'][:8]}")

    # `gone` registers and leaves. why the raw op and not `serve()` +
    # `add_describer`: the SDK's provider helper installs a renewal task, which
    # is the opposite of what this half needs. Registering directly leaves
    # nothing to renew, which is exactly a registrant that has gone away.
    async with await astral.connect(n1["endpoint"], token=gone["token"]) as c:
        lease = await c.objects.register_describer(
            Duration(LEASE_SECONDS * 1_000_000_000))
        facts["granted_seconds"] = float(lease.duration) / 1e9
        facts["expires_at"] = str(lease.expires_at)
        print(f"driver: gone registered, lease {facts['granted_seconds']}s")

    # `kept` stays and renews. Its connection is held for the whole of the
    # window below, so the only thing keeping its registration alive is the
    # re-registration in the middle of it.
    async with await astral.connect(n1["endpoint"], token=kept["token"]) as keeper:
        await keeper.objects.register_describer(
            Duration(LEASE_SECONDS * 1_000_000_000))

        # the departed registrant's lease is still live: the node has no way to
        # know nobody is listening, so it dials.
        async with await astral.connect(n1["endpoint"],
                                        token=n1["token"]) as c:
            facts["leased_seconds"] = await timed_describe(c)
            print(f"driver: describe while leased took "
                  f"{facts['leased_seconds']:.3f}s")

        # renew before the lease ends, which is what an SDK's keeper does
        await asyncio.sleep(RENEW_AT)
        renewed = await keeper.objects.register_describer(
            Duration(LEASE_SECONDS * 1_000_000_000))
        facts["renewed_expires_at"] = str(renewed.expires_at)
        print("driver: kept renewed")

        # into the window where `gone` has expired and `kept` has not
        await asyncio.sleep(SECOND_DESCRIBE_AT)

        async with await astral.connect(n1["endpoint"],
                                        token=n1["token"]) as c:
            facts["lapsed_seconds"] = await timed_describe(c)
            print(f"driver: describe after the lapse took "
                  f"{facts['lapsed_seconds']:.3f}s")

    write_facts(facts)


asyncio.run(main())
