#!/usr/bin/env python3
"""Oracle: messaging serves its participants over apphost, with MCP off.

Six judgements, and the order matters. The node served no MCP, on the port
the harness reserved or anywhere else, or nothing below says anything about
messaging without it. The authority's record comes next, because an exchange
proves nothing about reach unless the authority was asked, naming the right
actor on each side, and admitted it. Then the exchange itself, the refusal of
every caller whose mailbox the node does not host, and what
messaging.delete_identity left of bob.

Last, what the node holds. The driver reports what it was answered; whether
each participant's mailbox is hosted under a contract of its own, and what
ada's outbox rows say happened to her mail, the oracle asks the node itself.
"""
import asyncio
import re
from pathlib import Path

import astral

from lib import jsonops, mail
from lib.authority import RECEIVE, SEND, answers
from lib.sessionio import load

HOST = "mod.messaging.host_mailbox_action"
RELAY = "mod.nodes.relay_for_action"

# What a refusal of a caller must read like: the op rejects the query before
# accepting it. Anything else means the call never reached the guard.
REJECTED = "QueryRejected"

ANSI = re.compile(r"\x1b\[[0-9;]*m")


def check_precondition(facts):
    assert facts["serves_mcp"] is False, (
        "the harness started the node with MCP on — nomcp1 is not in "
        "nodeconfig.WITHOUT_MCP, and nothing below is evidence about "
        "messaging without MCP")
    probe = facts["mcp_probe"]
    assert not probe["answered"] and "refused" in probe["detail"].lower(), (
        f"the node's MCP port answered: {probe['detail']} — an MCP server was "
        "listening while the test ran")


def check_no_mcp_server(node):
    """The node's own log: mod/mcp started no server on any address.

    The probe asks only the port the harness reserved. A node that ignored
    the empty bind_mcp would listen on mod/mcp's default address instead, and
    the probe would pass. The log names the address a server starts at, so
    its silence covers every address. The control is messaging's own verbose
    line: it proves verbose lines reach this log, so the silence is evidence.
    """
    log = ANSI.sub("", (Path(node["root"]) / "astrald.log").read_text(
        errors="replace"))
    lines = [ln for ln in log.splitlines() if "mcp server:" in ln]
    assert not lines, (
        "the node's MCP server tried to start with MCP off:\n"
        + "\n".join(lines))
    assert "[messaging] created participant" in log, (
        "the node's log holds no verbose line from messaging, so the absence "
        "of an MCP server line proves nothing")


def check_authority(facts):
    q, a, b, c = facts["questions"], facts["ada"], facts["bob"], facts["cleo"]
    for kind, actor, other, want, who in (
            (SEND, a, b, [True, True], "ada sending to bob, before and after "
             "bob was deleted"),
            (RECEIVE, b, a, [True], "bob receiving from ada, once"),
            (SEND, b, a, [True], "bob sending to ada"),
            (RECEIVE, a, b, [True], "ada receiving from bob"),
            (SEND, a, c, [False], "ada sending to cleo")):
        got = answers(q, kind, actor, other)
        assert got == want, (
            f"the authority answered {got!r} for {who}, not {want!r} — the "
            "node put the question to the wrong side, named the wrong actor, "
            f"or asked for a mailbox it no longer hosts; it was asked {q!r}")
    kinds = {x["type"] for x in q}
    assert kinds <= {SEND, RECEIVE}, (
        f"the authority was asked {sorted(kinds)} — mail asks send and receive "
        "alone, and hosting is answered by the mailbox's own contract")


def check_exchange(facts):
    x, a = facts["exchange"], facts["ada"]
    waited = x["waited"]
    assert not waited["TimedOut"] and [m["ID"] for m in waited["Messages"]] == [
        x["asked"]], (
        f"bob's wait answered {waited!r}, not ada's message {x['asked']}")
    assert [(m["ID"], m["Sender"].lower()) for m in x["inbox"]] == [
        (x["asked"], a)], f"bob's inbox is {x['inbox']!r}, not ada's message"
    heard = x["heard"]["Messages"][0]
    assert heard["Content"] == facts["ask"], (
        f"bob read {heard['Content']!r}, not {facts['ask']!r}")
    assert x["unfetched"]["FetchedAt"] is None and x["fetched"]["FetchedAt"], (
        f"ada's outbox row read fetched_at {x['unfetched']['FetchedAt']!r} "
        f"before bob read it and {x['fetched']['FetchedAt']!r} after — the "
        "read left no receipt on the sender's row")
    got = x["got"]["Messages"][0]
    assert got["Content"] == facts["answer"] and \
        got["Envelope"]["ParentID"] == x["asked"], (
        f"ada read {got['Content']!r} naming parent "
        f"{got['Envelope']['ParentID']}, not bob's answer to {x['asked']}")
    assert x["archived"] == [True, False], (
        f"archiving twice answered {x['archived']!r}, not [True, False]")
    assert x["asked"] not in x["inbox_after"] and \
        x["asked"] in x["archive_after"], (
        "the archived message did not move from bob's inbox to his archive")
    assert "unknown recipient" in x["to_cleo"]["detail"], (
        f"ada's send to cleo ended {x['to_cleo']!r} — a participant the "
        "authority refuses reads as one the node never heard of")


def check_unhosted(facts):
    for who, r in facts["unhosted"].items():
        assert r["refused"] and REJECTED in r["detail"], (
            f"{who} ended {r['detail']} — a caller whose mailbox the node "
            "does not host reached a mail operation, or failed without "
            "being rejected")


def check_deletion(facts):
    d = facts["deletion"]
    for k, r in d["before"].items():
        assert not r["refused"] and facts["bob"] in r["detail"].lower(), (
            f"bob's {k} token did not authenticate before the deletion: "
            f"{r['detail']} — its refusal afterwards proves nothing")
    assert not d["deleted"]["refused"], f"the deletion failed: {d['deleted']}"
    for k in ("info", "again"):
        assert "identity not found" in d[k]["detail"], (
            f"after the deletion the node answered {d[k]['detail']} for bob, "
            "not identity not found")
    for k, r in d["after"].items():
        assert r["refused"] and "AuthFailed" in r["detail"], (
            f"bob's {k} token ended {r['detail']} after the deletion — a "
            "deleted participant still speaks to the node")
    assert "delivery failed" in d["sent"]["detail"], (
        f"a send to bob after the deletion ended {d['sent']['detail']} — the "
        "node still routes to a mailbox it withdrew")


async def check_contracts(admin, facts):
    """Every participant's mailbox is hosted under a contract of its own, and
    the deletion withdrew bob's without revoking it."""
    node = facts["node"]
    for name in ("ada", "bob", "cleo", "app"):
        held = await mail.contracts_of(admin, facts[name])
        hosting = [c for c in held if mail.permits(c) == [(HOST, 0, None)]]
        relay = [c for c in held if mail.permits(c) == [(RELAY, 0, None)]]
        want = 0 if name == "app" else 1
        assert len(hosting) == want and all(
            c["Object"]["Contract"]["Subject"].lower() == node
            for c in hosting), (
            f"{name} issued {len(hosting)} hosting contract(s) to this node, "
            f"not {want}; it issued {[mail.permits(c) for c in held]}")
        assert len(relay) == 1, (
            f"{name} issued {len(relay)} relay contracts, not one of its own")


async def check_records(n, facts):
    async with await astral.connect(n["endpoint"], token=n["token"]) as admin:
        await check_contracts(admin, facts)
        info = await mail.identity(admin, facts["aliases"]["ada"])
        gone = await jsonops.call(admin, "messaging.identity?identity="
                                  f"{facts['bob']}&out=json")
    assert not [k for k in info if "token" in k.lower()], (
        f"messaging.identity answered {info} — the record carries a credential")
    assert gone[0]["Object"] == "identity not found", (
        f"the node still names bob: {gone}")

    async with await astral.connect(n["endpoint"],
                                    token=facts["ada_token"]) as ca:
        rows = {r["ID"]: r for r in await mail.list_messages(ca, list="outbox")}
    asked = rows[facts["exchange"]["asked"]]
    assert asked["LandedAt"] and asked["FetchedAt"], (
        f"ada's row for her question reads {asked!r}, not landed and fetched")
    after = [r for r in rows.values()
             if r["Recipient"].lower() == facts["bob"] and r is not asked]
    assert len(after) == 1 and after[0]["FailedAt"] and \
        not after[0]["LandedAt"], (
        f"ada's rows to bob after the deletion read {after!r}, not one failed")


def main():
    doc = load()
    facts = doc["facts"]
    check_precondition(facts)
    check_no_mcp_server(doc["nodes"]["nomcp1"])
    check_authority(facts)
    check_exchange(facts)
    check_unhosted(facts)
    check_deletion(facts)
    asyncio.run(check_records(doc["nodes"]["nomcp1"], facts))
    print("oracle: with MCP off, ada and bob exchanged mail through the "
          "messaging ops, a caller without a hosted mailbox was rejected, "
          "every mailbox is hosted under its own contract, and bob's deletion "
          "withdrew his mailbox and his tokens while his contract stays")


main()
