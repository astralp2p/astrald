#!/usr/bin/env python3
"""Driver: an app on node1 reads an object only node2 holds, through node1.

The app (the Player) asks node1 alone, with no target. node1 authorizes the app
through an evaluator rule, misses the object on its own device, and its
`siblings` repository asks node2 for it under node1's identity. Three phases:

  allowed   the evaluator acks; the read returns node2's bytes, and node1's
            device repository still lacks the object afterwards.
  refused   the evaluator answers anything but an ack; the read is rejected.
  unlinked  both nodes stop dialling and listening, and node1 closes its links
            to node2; the read fails within UNLINKED_BUDGET. Linking is
            restored and the link rebuilt before this driver exits, whatever
            happened.

The bytes of node2's log that each of the first two phases spans are recorded,
so the oracle can check that the allowed read reached node2 and the refused
one did not.

The evaluator records every question it answers, as the hex of the action's
payload, so the oracle can see which actor and object each question named.
"""
import asyncio
import time
from pathlib import Path

import astral
from astral.object import Ack
from astral.types import ObjectID

from lib.sessionio import load, write_facts

SEE = "mod.auth.see_objects_action"
EVALUATE = "auth.evaluate"

# why: an unlinked read misses without routing anything, so it answers at once.
# The sibling repository's routing timeout is 15 s; a read slower than this
# budget waited on a sibling it should not have asked.
UNLINKED_BUDGET = 10

# why an explicit deadline: a broken route hangs rather than answering, and the
# client's default is 60 s.
DEADLINE = 30

# The knobs that stop a node from building a link to a sibling. Each is turned
# off on both nodes for the unlinked phase: dialling, so neither node's link
# maintenance dials the other, and listening, so neither accepts a link the
# other built anyway. tor only dials here; its onion service stays as it was.
LINK_KNOBS = [f"/mod/{m}/settings/{k}" for m in ("tcp", "kcp")
              for k in ("dial", "listen")] + ["/mod/tor/settings/dial"]


async def attempt(coro) -> dict:
    """Run one read, and report its outcome and how long it took."""
    started = time.monotonic()
    try:
        got = await coro
        return {"ok": True, "bytes": len(got), "data": got.hex(),
                "seconds": time.monotonic() - started}
    except astral.QueryRejected as e:
        return {"ok": False, "rejected": True, "code": e.code,
                "seconds": time.monotonic() - started}
    except astral.AstralError as e:
        return {"ok": False, "rejected": False, "error": repr(e),
                "seconds": time.monotonic() - started}


async def links_to(node, peer: str) -> list:
    return [ln for ln in await node.nodes.links(experimental=True)
            if ln.remote_identity is not None and ln.remote_identity.hex() == peer]


async def set_link_knobs(n, value: str) -> None:
    async with await astral.connect(n["endpoint"], token=n["token"]) as c:
        for path in LINK_KNOBS:
            await c.tree.set_text(path, value, type="bool")


async def unlink(n1, n2) -> int:
    """Stop both nodes linking, close node1's links to node2, and return how many
    links were closed before node1 held none for a whole second."""
    await set_link_knobs(n1, "false")
    await set_link_knobs(n2, "false")
    closed = 0
    async with await astral.connect(n1["endpoint"], token=n1["token"]) as c:
        # why: a close and a relink race; the read must start after a quiet
        # second, so the phase tests an absent link and not a closing one.
        for _ in range(10):
            links = await links_to(c, n2["identity"])
            if not links and closed:
                return closed
            for ln in links:
                await c.nodes.close_link(ln.id, experimental=True)
                closed += 1
            await asyncio.sleep(1)
    raise RuntimeError("node1 still links to node2 after its links were closed")


async def relink(n1, n2) -> bool:
    """Restore linking on both nodes and rebuild the link node1 holds to node2."""
    await set_link_knobs(n1, "true")
    await set_link_knobs(n2, "true")
    async with await astral.connect(n1["endpoint"], token=n1["token"]) as c:
        for _ in range(30):
            if await links_to(c, n2["identity"]):
                return True
            try:
                await c.nodes.new_link(n2["identity"], experimental=True,
                                       timeout=5)
            except astral.AstralError:
                pass
            await asyncio.sleep(1)
    return False


async def main():
    doc = load()
    n1, n2 = doc["nodes"]["node1"], doc["nodes"]["node2"]
    facts = doc["facts"]
    object_id = facts["peer_object_id"]
    node2_log = Path(n2["root"]) / "astrald.log"

    async with await astral.connect(n1["endpoint"], token=facts["user_token"]) as u:
        player = await u.apphost.register()
        evaluator = await u.apphost.register()
        await u.call_one(f"auth.set_evaluator?actor={player.identity.hex()}"
                         f"&action={SEE}&evaluator={evaluator.identity.hex()}")

    verdict = {"allow": True}
    questions = []

    async def evaluate(q):
        """Answer one question with the current verdict, and record it."""
        async with await q.accept(allow_unparsed=True) as s:
            action = await s.first(timeout=5)
            questions.append({
                "type": getattr(action, "ASTRAL_TYPE", "?"),
                "payload": getattr(action, "payload", b"").hex(),
                "allow": verdict["allow"],
            })
            if verdict["allow"]:
                await s.send(Ack())
            else:
                await s.send_eos()

    phases = {}
    relinked = None
    async with await astral.connect(n1["endpoint"], token=str(evaluator.token)) as ev:
        async with await ev.serve() as svc:
            svc.mount(EVALUATE, evaluate)

            async with await astral.connect(n1["endpoint"],
                                            token=str(player.token)) as p:
                def read():
                    return p.objects.read(object_id, zone="dvn",
                                          timeout=DEADLINE)

                log_from = node2_log.stat().st_size
                phases["allowed"] = await attempt(read())
                phases["allowed"]["node2_log"] = [log_from, node2_log.stat().st_size]

                verdict["allow"] = False
                log_from = node2_log.stat().st_size
                phases["refused"] = await attempt(read())
                # why: a read node2 answered is logged as it is routed, before
                # the reply reaches node1, so the log is complete by now.
                phases["refused"]["node2_log"] = [log_from, node2_log.stat().st_size]

                verdict["allow"] = True
                try:
                    phases["unlinked"] = {"links_closed": await unlink(n1, n2)}
                    phases["unlinked"].update(await attempt(read()))
                finally:
                    relinked = await relink(n1, n2)

    async with await astral.connect(n1["endpoint"], token=facts["user_token"]) as u:
        node1_device = await u.objects.contains("device", object_id)
        await u.call_one(f"auth.set_evaluator?actor={player.identity.hex()}"
                         f"&action={SEE}")

    write_facts({"read_via_sibling": {
        "player": player.identity.hex(),
        "evaluator": evaluator.identity.hex(),
        "object_hash": ObjectID.parse(object_id).hash.hex(),
        "phases": phases,
        "questions": questions,
        "node1_device_contains": node1_device,
        "relinked": relinked,
        "unlinked_budget": UNLINKED_BUDGET,
    }})
    print("driver: " + ", ".join(
        f"{k}: {'read ' + str(v.get('bytes')) + ' B' if v.get('ok') else 'refused'}"
        f" in {v.get('seconds', 0):.2f}s" for k, v in phases.items())
        + f"; {len(questions)} questions; relinked {relinked}")


asyncio.run(main())
