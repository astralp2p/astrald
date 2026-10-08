#!/usr/bin/env python3
"""Oracle: node1 reads node2's object for an app only while node1 allows it,
and node1's apphost.hosts names the node that hosts each app.

Ground truth is node2's own copy, read with node2's token from its local
repository, and node1's device repository, asked by the oracle itself. The
driver's facts supply what only the run could see: what each read returned,
which questions the evaluator answered, and which bytes of node2's log each
phase spans.

why node2's log: "no query reaches node 2" is a claim about node2, and node2's
log is the one record of what it was asked. The allowed phase is the control:
its read must appear there, or the absence in the refused phase proves nothing.
"""
import asyncio
import re
from pathlib import Path

import astral

from lib.sessionio import load

ANSI = re.compile(r"\x1b\[[0-9;]*m")

# astral.CodeRejected, and objects.CodeUnavailable in mod/objects.
CODE_REJECTED = 1
CODE_UNAVAILABLE = 5


def log_slice(path: Path, span: list) -> str:
    with path.open("rb") as f:
        f.seek(span[0])
        return ANSI.sub("", f.read(span[1] - span[0]).decode(errors="replace"))


def reads_of(log: str, object_id: str) -> list:
    """Lines of a node log that record an objects.read of the object."""
    return [ln for ln in log.splitlines()
            if "objects.read" in ln and object_id in ln]


def names(question: dict, player: str, object_hash: str) -> bool:
    """Whether a question's action names the player and the object."""
    return player in question["payload"] and object_hash in question["payload"]


async def main():
    doc = load()
    n1, n2 = doc["nodes"]["node1"], doc["nodes"]["node2"]
    facts = doc["facts"]
    object_id = facts["peer_object_id"]
    run = facts.get("read_via_sibling")
    assert run, "the driver recorded no read_via_sibling facts"
    phases, questions = run["phases"], run["questions"]
    player, object_hash = run["player"], run["object_hash"]
    node2_log = Path(n2["root"]) / "astrald.log"

    async with await astral.connect(n2["endpoint"], token=n2["token"]) as c:
        held = await c.objects.read(object_id, repo="local")
    async with await astral.connect(n1["endpoint"],
                                    token=facts["user_token"]) as c:
        node1_device = await c.objects.contains("device", object_id)

    assert held, f"node2 holds no bytes for {object_id}"
    assert not node1_device, (
        f"node1's device repository holds {object_id}: the read stored it, or "
        "node1 held it before, and the allowed read proves nothing about node2")
    assert not run["node1_device_contains"], (
        "node1's device repository held the object right after the reads")

    allowed = phases["allowed"]
    assert allowed["ok"], f"the allowed read failed: {allowed}"
    assert bytes.fromhex(allowed["data"]) == held, (
        f"the app read {allowed['bytes']} B that differ from node2's "
        f"{len(held)} B")
    assert reads_of(log_slice(node2_log, allowed["node2_log"]), object_id), (
        "node2's log records no objects.read of the object during the allowed "
        "read, so the bytes did not come from node2")

    refused = phases["refused"]
    assert not refused["ok"] and refused.get("rejected"), (
        f"the refused read was not rejected: {refused}")
    assert refused["code"] == CODE_REJECTED, (
        f"the refused read was rejected with code {refused['code']}; an "
        f"authorization refusal answers {CODE_REJECTED}")
    leaked = reads_of(log_slice(node2_log, refused["node2_log"]), object_id)
    assert not leaked, (
        "node2 was asked for the object while node1's evaluator refused the "
        f"app: {leaked}")

    asked = {True: [], False: []}
    for q in questions:
        asked[q["allow"]].append(q)
    assert asked[True] and all(names(q, player, object_hash) for q in asked[True]), (
        "the evaluator answered no allowed question naming the app and the "
        "object")
    assert asked[False] and all(names(q, player, object_hash) for q in asked[False]), (
        "the evaluator answered no refused question naming the app and the "
        "object: the refusal was not the evaluator's")

    unlinked = phases["unlinked"]
    assert not unlinked.get("ok"), (
        f"the read succeeded while node2 was unlinked: {unlinked}")
    assert unlinked["seconds"] < run["unlinked_budget"], (
        f"the unlinked read took {unlinked['seconds']:.2f}s; the budget is "
        f"{run['unlinked_budget']}s")
    assert unlinked.get("rejected") and unlinked["code"] == CODE_UNAVAILABLE, (
        f"the unlinked read answered {unlinked}; an object no sibling could "
        f"supply is rejected with code {CODE_UNAVAILABLE}")
    assert run["relinked"], "node1 did not link to node2 again after the test"

    hosts = run["hosts"]
    assert hosts["remote_hosts"] == [n2["identity"]], (
        f"node1's apphost.hosts named {hosts['remote_hosts']} for the app "
        f"registered on node2 within {run['hosts_budget']}s; want node2 alone")
    assert hosts["links_before"] and hosts["links_after"] == hosts["links_before"], (
        "node1's links to node2 changed while the node2 app's contract "
        f"arrived ({hosts['links_before']} -> {hosts['links_after']}), so a "
        "new link's sync may have carried it rather than the registration")
    assert hosts["player_hosts"] == [n1["identity"]], (
        f"node1's apphost.hosts named {hosts['player_hosts']} for the Player "
        "registered on node1; want node1 alone")
    assert hosts["no_app_hosts"] == [], (
        f"node1's apphost.hosts named {hosts['no_app_hosts']} for an identity "
        "that is no app; want EOS alone")

    print(f"oracle: allowed read {allowed['bytes']} B matching node2's copy "
          f"in {allowed['seconds']:.2f}s; refused read rejected with node2 "
          f"unasked; unlinked read failed in {unlinked['seconds']:.2f}s; "
          f"{len(questions)} evaluator questions; node1's device lacks the "
          "object; node1's apphost.hosts names node2 for node2's app in "
          f"{hosts['remote_seconds']:.2f}s and node1 for the Player")


asyncio.run(main())
