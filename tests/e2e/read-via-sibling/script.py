#!/usr/bin/env python3
"""Driver: an app on node1 reads an object only node2 holds, through node1.

The app (the Player) asks node1 alone, with no target. node1 authorizes the app
through an evaluator rule, misses the object on its own device, and its
`siblings` repository asks node2 for it under node1's identity. First an app
registered on node2 while the nodes are linked must appear in node1's
apphost.hosts. Then three read phases:

  allowed   the evaluator acks; the read returns node2's bytes, and node1's
            device repository still lacks the object afterwards.
  refused   the evaluator answers anything but an ack; the read is rejected.
  unlinked  both nodes stop dialling and listening, neither accepts tcp any
            more, and node1 closes its links to node2; the read fails within
            UNLINKED_BUDGET. Linking is restored and the link rebuilt before
            this driver exits, whatever happened.

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

# why: apphost.register pushes the new app's relay contract to node1 at once,
# so a lookup that waits longer than this budget missed the push.
HOSTS_BUDGET = 10

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


async def tcp_listening(n) -> bool:
    # why: lan_endpoint is the address a peer dials, which the relink races.
    host, port = n["lan_endpoint"].removeprefix("tcp:").rsplit(":", 1)
    try:
        _, w = await asyncio.open_connection(host, int(port))
    except OSError:
        return False
    w.close()
    return True


async def await_tcp_down(n, name: str) -> None:
    """Wait until listen=false has stopped the node's tcp server."""
    # why: the setting reaches the server asynchronously; closing links before
    # it stops races a relink, so the precondition is asserted, not assumed.
    for _ in range(50):
        if not await tcp_listening(n):
            return
        await asyncio.sleep(0.1)
    raise RuntimeError(f"{name} still accepts tcp after listen=false")


async def unlink(n1, n2) -> int:
    """Stop both nodes linking, close node1's links to node2, and return how many
    links were closed before node1 held none for a whole second."""
    await set_link_knobs(n1, "false")
    await set_link_knobs(n2, "false")
    await await_tcp_down(n1, "node1")
    await await_tcp_down(n2, "node2")
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


async def hosts_of(c, app: str) -> list:
    """The hosts apphost.hosts names for app, as hex identities."""
    return sorted(h.hex() for h in await c.call(f"apphost.hosts?app={app}"))


async def hosts(n1, n2, user_token: str, player) -> dict:
    """Register an app on node2 while node1 is linked to it, and ask node1's
    apphost.hosts, as the Player, for that app, for the Player, and for an
    identity that is no app. node1's links to node2 are recorded before and
    after, so the oracle can tell the registration's push from a new link's
    sync."""
    async with await astral.connect(n1["endpoint"], token=user_token) as u, \
            await astral.connect(n1["endpoint"], token=str(player.token)) as p:
        async def link_ids() -> list:
            return sorted(str(ln.id) for ln in await links_to(u, n2["identity"]))

        links_before = await link_ids()
        async with await astral.connect(n2["endpoint"], token=n2["token"]) as c2:
            remote = await c2.apphost.register()
        remote_app = remote.identity.hex()

        started = time.monotonic()
        remote_hosts = await hosts_of(p, remote_app)
        while not remote_hosts and time.monotonic() - started < HOSTS_BUDGET:
            await asyncio.sleep(0.2)
            remote_hosts = await hosts_of(p, remote_app)
        seconds = time.monotonic() - started

        return {
            "remote_app": remote_app,
            "remote_hosts": remote_hosts,
            "remote_seconds": seconds,
            "player_hosts": await hosts_of(p, player.identity.hex()),
            "no_app_hosts": await hosts_of(p, n1["identity"]),
            "links_before": links_before,
            "links_after": await link_ids(),
        }


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

    host_lookup = await hosts(n1, n2, facts["user_token"], player)

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
        "hosts": host_lookup,
        "hosts_budget": HOSTS_BUDGET,
    }})
    print("driver: " + ", ".join(
        f"{k}: {'read ' + str(v.get('bytes')) + ' B' if v.get('ok') else 'refused'}"
        f" in {v.get('seconds', 0):.2f}s" for k, v in phases.items())
        + f"; {len(questions)} questions; relinked {relinked}"
        + f"; node2 app hosts {host_lookup['remote_hosts']}"
        f" in {host_lookup['remote_seconds']:.2f}s")


asyncio.run(main())
