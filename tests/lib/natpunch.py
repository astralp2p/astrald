"""The steps the NAT-punch tests share: a timed punch, a kcp-only pair, an echo.

why one module: nat-punch-translating and nat-punch-double-translating judge
the same three things (X06): `nat.punch` completes within a second, the
punched kcp link carries traffic, and it does so alone. Only how the pair
reaches each other before the punch differs.

why facts of plain values: the driver acts and the oracle judges, so every
step returns a JSON-able dict and asserts nothing.
"""
import asyncio
import json
import time

import astral
from astral.primitives import Bool

# why both: a pair that keeps a tcp or tor link would carry the echo there,
# and the echo has to prove the punched kcp link.
SIGNALING_NETWORKS = ("tcp", "tor")


async def networks_to(c, peer: str) -> list:
    """The networks of the links the node behind c holds to peer."""
    links = await c.nodes.links(experimental=True)
    return sorted({l.network for l in links if str(l.remote_identity) == peer})


def _objects(raw: bytes) -> list:
    """The JSON objects of an `-out json` answer, one per line."""
    out = []
    for line in raw.decode(errors="replace").splitlines():
        line = line.strip()
        if line:
            try:
                out.append(json.loads(line))
            except json.JSONDecodeError:
                out.append({"Type": "unparsed", "Object": line})
    return out


async def punch(c, peer: str, deadline_s: float) -> dict:
    """One `nat.punch` to peer, timed on the host's monotonic clock.

    why raw JSON: the hole's bind ports are new on the wire, and the oracle
    reads them from the node's own JSON rather than from an SDK decoder.
    """
    t0 = time.monotonic()
    try:
        raw = await c.call_raw(f"nat.punch?identity={peer}&out=json",
                               timeout=deadline_s)
        objs = _objects(raw)
    except astral.errors.AstralError as e:
        objs = [{"Type": "error_message", "Object": str(e)}]
    elapsed = time.monotonic() - t0
    holes = [o["Object"] for o in objs if o.get("Type") == "nat.hole"]
    errors = [o.get("Object") for o in objs if o.get("Type") != "nat.hole"]
    return {"seconds": round(elapsed, 3), "hole": holes[0] if holes else None,
            "errors": errors}


async def link_over(c, peer: str, strategy: str, want: str,
                    deadline_s: float, poll_s: float = 2.0) -> list:
    """Run `strategy` toward peer and wait until a `want` link to it exists."""
    try:
        await c.nodes.new_link(peer, strategies=strategy,
                               experimental=True, timeout=deadline_s)
    except astral.errors.RemoteError as e:
        print(f"natpunch: new_link({strategy}) returned early ({e}); waiting")

    deadline = time.monotonic() + deadline_s
    nets = await networks_to(c, peer)
    while want not in nets and time.monotonic() < deadline:
        await asyncio.sleep(poll_s)
        nets = await networks_to(c, peer)
    return nets


async def isolate_kcp(a, a_id: str, b, b_id: str) -> dict:
    """Leave the pair with kcp links only: no tcp or tor dialing, no such link.

    why dialing off on both nodes first: a closed link is redialed by the
    link maintainer, and only the setting keeps it closed for the echo.
    """
    for c in (a, b):
        for net in SIGNALING_NETWORKS:
            await c.tree.set(f"/mod/{net}/settings/dial", Bool(False))

    closed = []
    for c, peer in ((a, b_id), (b, a_id)):
        for l in await c.nodes.links(experimental=True):
            if str(l.remote_identity) == peer and l.network != "kcp":
                try:
                    await c.nodes.close_link(l.id, experimental=True)
                    closed.append(f"{l.network}:{l.id}")
                except astral.errors.RemoteError as e:
                    print(f"natpunch: close_link {l.id} ({l.network}): {e}")
    return {"closed": closed, "a": await networks_to(a, b_id),
            "b": await networks_to(b, a_id)}


async def echo(c, peer: str, seconds: float, every_s: float) -> list:
    """Round trips to peer for `seconds`, one every `every_s`, as ticks.

    A tick is {t, ok, networks}: t in seconds since the echo began, whether a
    `nodes.links` query to peer came back within `every_s * 4`, and the
    networks the node behind c held to peer at that moment.
    """
    ticks = []
    t0 = time.monotonic()
    while (t := time.monotonic() - t0) < seconds:
        ok = True
        try:
            await c.nodes.links(experimental=True, target=peer,
                                timeout=every_s * 4)
        except (astral.errors.AstralError, asyncio.TimeoutError):
            ok = False
        nets = await networks_to(c, peer)
        ticks.append({"t": round(t, 3), "ok": ok, "networks": nets})
        await asyncio.sleep(max(0.0, every_s - (time.monotonic() - t0 - t)))
    return ticks


def hole_ports(hole: dict, self_id: str) -> dict:
    """This node's bind port and external port in a `nat.hole` JSON object.

    why both endpoint forms: astral-go gives nat.endpoint a MarshalText, so
    the JSON channel writes "ip:port"; an object form is read as well.
    """
    side = "Active" if hole.get("ActiveIdentity") == self_id else "Passive"
    ep = hole.get(f"{side}Endpoint")
    port = (int(str(ep).rsplit(":", 1)[1]) if isinstance(ep, str)
            else int((ep or {}).get("Port", 0)))
    return {"bind": int(hole.get(f"{side}BindPort", 0)), "external": port}
