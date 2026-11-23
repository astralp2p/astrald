#!/usr/bin/env python3
"""Driver: query node1 and node2 both ways while their LAN goes, Tor restarts under node2, and the LAN returns.

The ops before it gave both nodes an onion while their LAN link was live,
scheduled Tor on node2 to stop --in s on and start --down s later
(restart-tor), and took node2 off the LAN for --for s (lan-outage), closing
the links that rode its address.

1. Seeds each node with the other's onion when it holds none: the onions
   sync over the LAN link before the cut, and a cut that came first leaves
   a node no onion to dial. `seeded` records what it gave.
2. Taps node2's log for the whole flow.
3. Every QUERY_EVERY s per direction, one query at a time: node1 as the User
   asks node2 for `user.info`, and node2 with its node token asks node1,
   each with a QUERY s deadline. Each record holds its start, end, outcome
   and, after an answer, the networks of the caller's links to the peer.
4. Every WATCH s: both nodes' links to each other, whether node2 resolves a
   tor row for itself, and whether node2 holds its 10.77 address
   (`ip.local_addrs`). The first time the tap holds node2's
   `is down: tor control connection closed` line, the driver notes it.
5. Ends when node1 and node2 share a tcp link after the LAN's return, node1
   lists no tor link to node2, and TAIL s of queries ran after the tcp link;
   or END_AFTER s after the LAN's return; or at DEADLINE.

The driver acts and judges nothing. It records the `fallback_paths` fact,
times in seconds since the driver began, on the host's clock: the LAN and
Tor events happen on node2 and the links on both nodes, so one clock spans
them (lib/addrmove.py says the same of a move).
"""
import asyncio
import shlex
import time
import tomllib
from contextlib import AsyncExitStack, suppress
from pathlib import Path

import astral
from astral import errors

from lib.nodelog import LogTap
from lib.sessionio import load, write_facts

QUERY = 30.0
QUERY_EVERY = 1.0
# why: a query the client deadline failed to end is recorded, not waited on
QUERY_GUARD = 60.0
WATCH = 1.0
CALL = 5.0
TAIL = 15.0
# why 230 s: the oracle's 90 s for the tcp link and 130 s more for the tor
# link's retire, and 10 s to see it
END_AFTER = 230.0
DOWN_LINE = "is down: tor control connection closed"
NEEDLES = ("onion service", "server error")


def op_flags(test_dir: Path) -> dict:
    """{op: {flag: int}} of the ops on test_dir's manifest."""
    out = {}
    for line in tomllib.loads((test_dir / "test.toml").read_text())["ops"]:
        words = shlex.split(line)
        out[words[0]] = {f[2:]: int(v) for f, v in zip(words, words[1:])
                         if f.startswith("--") and v.isdigit()}
    return out


def outcome(e: Exception | None) -> str:
    """A call's outcome class (astral-docs core-definitions/query.md, Outcomes)."""
    match e:
        case None:
            return "ok"
        case errors.RouteNotFound():
            return "route_not_found"
        case errors.QueryRejected(code=7):
            return "unavailable"
        case errors.QueryRejected():
            return f"rejected:{e.code}"
        case errors.QueryTimeout() | TimeoutError():
            return "timeout"
        case errors.QueryCanceled():
            return "canceled"
    return f"error:{type(e).__name__}"


async def networks_to(c, peer: str) -> list:
    return sorted({str(i.network) for i in await c.nodes.links(experimental=True)
                   if str(i.remote_identity) == peer})


async def ask(c, peer: str, clock) -> dict:
    """One user.info query of peer through c, as a record."""
    rec, err = {"sent_at": clock()}, None
    try:
        await asyncio.wait_for(c.call("user.info", target=peer, timeout=QUERY), QUERY_GUARD)
    except Exception as e:  # noqa: BLE001 — the outcome is the observation
        err = e
    rec.update(ended_at=clock(), outcome=outcome(err))
    if err is not None:
        rec["how"] = f"{type(err).__name__}: {err}"[:200]
    else:
        with suppress(Exception):
            rec["networks"] = await asyncio.wait_for(networks_to(c, peer), CALL)
    return rec


async def ask_loop(c, peer: str, clock, records: list) -> None:
    """Queries peer through c every QUERY_EVERY s, one at a time, until cancelled."""
    while True:
        rec = await ask(c, peer, clock)
        records.append(rec)
        await asyncio.sleep(max(0.0, rec["sent_at"] + QUERY_EVERY - clock()))


async def own_onion(c) -> str | None:
    eps = await c.nodes.resolve_endpoints("localnode", experimental=True)
    return next((f"tor:{e.endpoint.address()}" for e in eps if e.endpoint.network() == "tor"), None)


async def held_onion(c, peer: str) -> str | None:
    eps = await c.nodes.resolve_endpoints(peer, experimental=True)
    return next((f"tor:{e.endpoint.address()}" for e in eps if e.endpoint.network() == "tor"), None)


async def seed(c1, c2, n1: dict, n2: dict) -> dict:
    """Gives each node the other's onion where it holds none; what it gave."""
    out = {}
    for name, c, other, oc in (("node1", c1, n2, c2), ("node2", c2, n1, c1)):
        if await held_onion(c, other["identity"]) is not None:
            continue
        onion = await own_onion(oc)
        if onion is not None:
            await c.nodes.add_endpoint(other["identity"], onion, experimental=True)
        out[name] = onion
    return out


async def links_rows(c, peer: str) -> list:
    return [{"id": str(i.id), "network": str(i.network)}
            for i in await c.nodes.links(experimental=True) if str(i.remote_identity) == peer]


async def read_state(c1, c2, n1: dict, n2: dict) -> dict:
    """One watch sample: both ends' links, node2's tor row, node2's LAN address."""
    l1 = await links_rows(c1, n2["identity"])
    l2 = await links_rows(c2, n1["identity"])
    tor = await own_onion(c2) is not None
    lan = any(str(a).startswith("10.77.") for a in await c2.ip.local_addrs())
    return {"n1": l1, "n2": l2, "tor": tor, "lan": lan}


def shared(s: dict, network: str) -> bool:
    """Whether node1 and node2 list one link over network under one id."""
    ids = lambda rows: {r["id"] for r in rows if r["network"] == network}  # noqa: E731
    return bool(ids(s["n1"]) & ids(s["n2"]))


def mark(seen: dict, s: dict) -> None:
    """Records the LAN's return, the shared tcp link after it, and node1's last tor link going."""
    if not s["lan"]:
        seen["lan_gone_seen"] = True
    elif seen.get("lan_gone_seen") and seen["lan_back_at"] is None:
        seen["lan_back_at"] = s["t"]
    if seen["lan_back_at"] is not None and seen["tcp_both_at"] is None and shared(s, "tcp"):
        seen["tcp_both_at"] = s["t"]
    if seen["tcp_both_at"] is not None and seen["tor_gone_at"] is None \
            and not any(r["network"] == "tor" for r in s["n1"]):
        seen["tor_gone_at"] = s["t"]


def done(seen: dict, now: float) -> bool:
    back, tcp = seen["lan_back_at"], seen["tcp_both_at"]
    if back is not None and now >= back + END_AFTER:
        return True
    return tcp is not None and seen["tor_gone_at"] is not None and now >= tcp + TAIL


async def watch(c1, c2, nodes: tuple, tap: LogTap, clock, seen: dict, deadline: float) -> None:
    """Samples both nodes every WATCH s until done or deadline."""
    while clock() < deadline and not done(seen, clock()):
        t = clock()
        try:
            s = {"t": t, **await asyncio.wait_for(read_state(c1, c2, *nodes), CALL)}
            seen["samples"].append(s)
            mark(seen, s)
        except Exception as e:  # noqa: BLE001 — a missed sample is not an observation
            seen["samples"].append({"t": t, "error": f"{type(e).__name__}: {e}"[:200]})
        if seen["down_seen"] is None and tap.find(DOWN_LINE):
            seen["down_seen"] = clock()
        await asyncio.sleep(max(0.0, t + WATCH - clock()))


async def connect(stack: AsyncExitStack, node: dict, token: str):
    return await stack.enter_async_context(await astral.connect(node["endpoint"], token=token))


async def run(stack: AsyncExitStack, doc: dict, ops: dict) -> dict:
    """The whole flow on open sessions; the fact."""
    n1, n2 = doc["nodes"]["node1"], doc["nodes"]["node2"]
    # why four sessions: each loop runs its own queries, one at a time
    cu = await connect(stack, n1, doc["facts"]["user_token"])
    cq2 = await connect(stack, n2, n2["token"])
    c1, c2 = await connect(stack, n1, n1["token"]), await connect(stack, n2, n2["token"])
    t0 = time.monotonic()
    clock = lambda: round(time.monotonic() - t0, 2)  # noqa: E731
    seen = {"ops": ops, "seeded": await seed(c1, c2, n1, n2), "samples": [],
            "down_seen": None, "lan_back_at": None, "tcp_both_at": None, "tor_gone_at": None,
            "queries": {"node1_to_node2": [], "node2_to_node1": []}}
    tap = await stack.enter_async_context(LogTap(n2))
    loops = [asyncio.create_task(ask_loop(cu, n2["identity"], clock, seen["queries"]["node1_to_node2"])),
             asyncio.create_task(ask_loop(cq2, n1["identity"], clock, seen["queries"]["node2_to_node1"]))]
    try:
        await watch(c1, c2, (n1, n2), tap, clock, seen,
                    ops["lan-outage"]["for"] + END_AFTER + 60)
    finally:
        for task in loops:
            task.cancel()
        await asyncio.gather(*loops, return_exceptions=True)
    seen.update(log=tap.find(*NEEDLES), tap=tap.state(), ended_at=clock())
    return seen


async def main():
    doc = load()
    ops = op_flags(Path(__file__).parent)
    async with AsyncExitStack() as stack:
        fact = await run(stack, doc, ops)
    write_facts({"fallback_paths": fact})
    q = fact["queries"]
    print(f"driver: seeded {fact['seeded']}; Tor down seen at {fact['down_seen']} s; LAN back at "
          f"{fact['lan_back_at']} s; tcp link at {fact['tcp_both_at']} s; tor link gone at "
          f"{fact['tor_gone_at']} s; {len(q['node1_to_node2'])} + {len(q['node2_to_node1'])} queries")


asyncio.run(main())
