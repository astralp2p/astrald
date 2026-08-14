#!/usr/bin/env python3
"""Driver: an app on node1 answers a query node2 issues, and leaves a trace.

This is the platform's core promise and the tree's largest hole: an app extends
the node that hosts it, registering a handler through `apphost.bind` +
`apphost.register_handler`, and astrald routes queries to it under the caller's
identity. No test called any of those ops.

The app answers with the ObjectID of what it was sent, and stores those bytes
on node1 as it goes. One answer therefore carries two claims the oracle can
check independently: the id is a content hash the oracle recomputes for itself,
and the object is either in node1's repository afterwards or it is not.

The service is closed before this driver exits, so by the time the oracle runs
the app is gone. That is deliberate — the oracle judges what the app left
behind, never a live process it could interrogate.
"""
import asyncio

import astral

from lib.sessionio import load, write_facts

OP = "test.app_query.absorb"
PAYLOAD = "app-query-sibling-asked-and-the-app-answered-0xC0FFEE"


async def main():
    doc = load()
    n1, n2 = doc["nodes"]["node1"], doc["nodes"]["node2"]

    # why n1's own token and not user_token: apphost.register_handler
    # registers under the query's caller, so the identity this connection
    # authenticates as is the identity the handler answers for. node2 targets
    # node1, so the handler has to sit under node1's identity — serving as the
    # User would register an app the sibling's query never resolves to.
    async with await astral.connect(n1["endpoint"], token=n1["token"]) as host:

        async def absorb(q):
            """Store what the caller sent; answer the id those bytes hash to."""
            payload = q.params["payload"].encode()
            async with host.objects.create() as w:
                await w.write(payload)
                object_id = await w.commit()
            async with await q.accept() as stream:
                await stream.send(object_id)
                await stream.send_eos()

        # why: serve() returns only once the first registration cycle has
        # completed, so there is no window to sleep through here. A wait added
        # to compensate for a race the SDK already closes would make this test
        # green for the wrong reason.
        async with await host.serve() as svc:
            svc.mount(OP, absorb)

            # node2 asks as itself. `user_token` is node1's apphost token and
            # authenticates nowhere else; a sibling node is the caller this
            # test wants anyway — a swarm member reaching an app on a peer.
            async with await astral.connect(n2["endpoint"],
                                            token=n2["token"]) as sibling:
                # why an explicit deadline: a broken route does not answer,
                # it hangs, and the client's default is 60 s. Failing in 15
                # gives the same verdict four times sooner.
                answered = await sibling.call_one(
                    f"{OP}?payload={PAYLOAD}", target=n1["identity"],
                    timeout=15)

    write_facts({
        "payload": PAYLOAD,
        "answered_id": str(answered),
    })
    print(f"driver: node2 queried {OP} on node1; the app answered "
          f"{str(answered)[:16]}…")


asyncio.run(main())
