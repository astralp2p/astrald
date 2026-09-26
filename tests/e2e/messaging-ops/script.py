#!/usr/bin/env python3
"""Driver: participants mail each other through messaging.* over apphost.

The node serves no MCP: its roster name is in nodeconfig.WITHOUT_MCP, so its
MCP server is off while mod/mcp still loads. Every exchange here reaches
mod/messaging through its own ops, the way an app reaches it.

The admin mints three participants with messaging.create_identity. ada and bob
correspond; cleo is hosted like them and admitted to no mail. The driver serves
the external authority the node asks about mail and delegated reads: it admits
ada and bob to send to and receive from each other, lets cleo read ada's
mailbox, and nothing else.

The driver acts and judges nothing. It records what every op answered, what
cleo read of ada's mailbox and what ada's rows and bob's row read before and
after, what bob and the node are answered when they name ada's mailbox, what
an identity without a hosted mailbox and the node itself are answered, what
messaging.delete_identity leaves of bob, and every question the authority was
asked. The oracle reads the node's own records for the rest.

why bob is the one deleted: bob is admitted, so a send to bob after the
deletion passes the authority and reaches routing. What routing answers then is
the node's withdrawal of the mailbox, and not the authority's refusal.
"""
import asyncio
import urllib.error
import urllib.request

import astral
from astral import querystring
from astral.errors import AstralError

from lib import jsonops, mail
from lib.authority import READ, RECEIVE, SEND, Authority
from lib.sessionio import load, write_facts

ASK = "ada-asks-0xC0FFEE"
ANSWER = "bob-answers-0xBEEF"
AFTER = "ada-writes-after-bob-left"
ALIASES = {"ada": "mail-ada", "bob": "mail-bob", "cleo": "mail-cleo"}

# why a window this long: a wait parks until a message lands, and a landing
# that never comes has to end the park before the driver's own budget does.
WAIT = "10s"


async def attempt(coro) -> dict:
    """Run one call that may be refused; report how it ended."""
    try:
        out = await coro
    except (AstralError, jsonops.OpError, OSError) as e:
        return {"refused": True, "detail": f"{type(e).__name__}: {e}"}
    return {"refused": False, "detail": repr(out)[:400]}


def probe_mcp(url: str) -> dict:
    """What the port reserved for the node's MCP server answers a POST."""
    req = urllib.request.Request(url, data=b"{}", method="POST")
    try:
        with urllib.request.urlopen(req, timeout=5) as resp:
            return {"answered": True, "detail": f"HTTP {resp.status}"}
    except urllib.error.HTTPError as e:
        return {"answered": True, "detail": f"HTTP {e.code}"}
    except (urllib.error.URLError, OSError) as e:
        return {"answered": False, "detail": f"{type(e).__name__}: {e}"}


async def whoami(endpoint: str, token: str) -> dict:
    """Whether a token authenticates, and as whom."""
    async def ask():
        async with await astral.connect(endpoint, token=token) as c:
            return await jsonops.value(c, "apphost.whoami?out=json")
    return await attempt(ask())


async def mint(n: dict) -> dict:
    """The three participants, bob's second token, and an app identity the
    node registered with no mailbox."""
    async with await astral.connect(n["endpoint"], token=n["token"]) as admin:
        minted = {name: await mail.create_identity(admin, alias)
                  for name, alias in ALIASES.items()}
        minted["info_ada"] = await mail.identity(admin, ALIASES["ada"])
        minted["app"] = await jsonops.value(admin, "apphost.register?out=json")
        minted["bob_second"] = await jsonops.value(admin, querystring.build(
            "apphost.create_token",
            {"identity": minted["bob"]["Identity"], "out": "json"}))
    return minted


async def outbox_row(client, id: str) -> dict:
    """The caller's outbox envelope for one message."""
    rows = await mail.list_messages(client, list="outbox")
    return next((r for r in rows if r["ID"] == id), {})


async def mailbox_state(ca, cb, answered: str) -> dict:
    """ada's mailbox as ada lists it, and bob's row for his answer: what a
    delegated read of ada's mailbox must leave as it found them."""
    return {
        "inbox": await mail.list_messages(ca, list="inbox"),
        "outbox": await mail.list_messages(ca, list="outbox"),
        "answer_sent": await outbox_row(cb, answered),
    }


async def delegated(n: dict, ca, cb, ids: dict) -> dict:
    """cleo, whom the authority lets read ada's mailbox, lists it and reads
    ada's question with its replies whole while ada has not read bob's answer;
    bob and the node name ada's mailbox too. ada's rows and bob's row for his
    answer are taken before and after. `ids` holds cleo's token, ada's
    identity, and the ids of ada's question and bob's answer.

    why cleo names ada by alias to list and by identity to read: list_messages
    takes either, and a read request's Mailbox is an identity.
    """
    alias, ada_id = ALIASES["ada"], ids["ada"]
    asked, answered = ids["asked"], ids["answered"]
    x = {"before": await mailbox_state(ca, cb, answered)}
    async with await astral.connect(n["endpoint"], token=ids["cleo"]) as cc:
        x["listed"] = {box: await mail.list_messages(cc, list=box,
                                                     mailbox=alias)
                       for box in ("inbox", "outbox")}
        x["read"] = await mail.read(cc, ("outbox", asked), ("inbox", answered),
                                    mailbox=ada_id, children="full")
    x["bob_list"] = await attempt(mail.list_messages(cb, mailbox=alias))
    x["bob_read"] = await attempt(mail.read(cb, ("inbox", answered),
                                            mailbox=ada_id))
    async with await astral.connect(n["endpoint"], token=n["token"]) as cn:
        x["node_list"] = await attempt(mail.list_messages(cn, mailbox=alias))
    x["after"] = await mailbox_state(ca, cb, answered)
    return x


async def exchange(n: dict, ada: dict, bob: dict, cleo: dict) -> dict:
    """ada asks, bob waits, reads and answers, cleo reads ada's mailbox, ada
    reads the answer, bob archives the question, and ada writes to cleo."""
    async with await astral.connect(n["endpoint"], token=ada["Token"]) as ca, \
            await astral.connect(n["endpoint"], token=bob["Token"]) as cb:
        # why the wait starts first: it parks, and the delivery is what ends it
        parked = asyncio.create_task(mail.wait(cb, WAIT))
        await asyncio.sleep(0.5)
        asked = await mail.send(ca, ALIASES["bob"], ASK)
        waited = await parked
        inbox = await mail.list_messages(cb, list="inbox")
        unfetched = await outbox_row(ca, asked)
        heard = await mail.read(cb, ("inbox", asked))
        fetched = await outbox_row(ca, asked)

        answered = await mail.send(cb, ada["Identity"], ANSWER, parent=asked)
        back = await mail.wait(ca, WAIT)
        read_by_cleo = await delegated(n, ca, cb, {
            "cleo": cleo["Token"], "ada": ada["Identity"],
            "asked": asked, "answered": answered})
        got = await mail.read(ca, ("inbox", answered))
        answer_fetched = await outbox_row(cb, answered)

        archived = [await mail.archive(cb, "inbox", asked),
                    await mail.archive(cb, "inbox", asked)]
        return {
            "asked": asked,
            "answered": answered,
            "waited": waited,
            "inbox": inbox,
            "unfetched": unfetched,
            "heard": heard,
            "fetched": fetched,
            "back": back,
            "delegated": read_by_cleo,
            "got": got,
            "answer_fetched": answer_fetched,
            "archived": archived,
            "inbox_after": [e["ID"] for e in await mail.list_messages(
                cb, list="inbox")],
            "archive_after": [e["ID"] for e in await mail.list_messages(
                cb, list="archive")],
            "to_cleo": await attempt(mail.send(ca, ALIASES["cleo"], ASK)),
        }


async def unhosted(n: dict, app: dict, bob: dict) -> dict:
    """What an identity without a hosted mailbox, and the node itself, are
    answered by the mail ops."""
    async with await astral.connect(n["endpoint"], token=app["Token"]) as c:
        app_list = await attempt(mail.list_messages(c, list="inbox"))
        app_send = await attempt(mail.send(c, bob["Identity"], ASK))
        app_read = await attempt(mail.read(c, ("inbox", "01" * 16)))
    async with await astral.connect(n["endpoint"], token=n["token"]) as c:
        node_list = await attempt(mail.list_messages(c, list="inbox"))
    return {"app_list": app_list, "app_send": app_send, "app_read": app_read,
            "node_list": node_list}


async def deletion(n: dict, ada: dict, bob: dict, second: dict) -> dict:
    """bob's tokens before and after messaging.delete_identity, what the node
    answers about bob afterwards, and a send to bob that follows."""
    tokens = {"first": bob["Token"], "second": second["Token"]}
    before = {k: await whoami(n["endpoint"], t) for k, t in tokens.items()}
    async with await astral.connect(n["endpoint"], token=n["token"]) as admin:
        deleted = await attempt(mail.delete_identity(admin, ALIASES["bob"]))
        info = await attempt(mail.identity(admin, bob["Identity"]))
        again = await attempt(mail.delete_identity(admin, bob["Identity"]))
    after = {k: await whoami(n["endpoint"], t) for k, t in tokens.items()}
    async with await astral.connect(n["endpoint"], token=ada["Token"]) as ca:
        sent = await attempt(mail.send(ca, bob["Identity"], AFTER))
    return {"before": before, "deleted": deleted, "info": info,
            "again": again, "after": after, "sent": sent}


async def main():
    n = load()["nodes"]["nomcp1"]
    facts = {"node": n["identity"].lower(), "serves_mcp": n["serves_mcp"],
             "mcp_probe": probe_mcp(n["mcp_url"]), "ask": ASK,
             "answer": ANSWER, "after": AFTER, "aliases": ALIASES}

    minted = await mint(n)
    ada, bob, cleo = minted["ada"], minted["bob"], minted["cleo"]
    a, b = ada["Identity"].lower(), bob["Identity"].lower()
    facts.update({
        "ada": a, "bob": b, "cleo": cleo["Identity"].lower(),
        "app": minted["app"]["Identity"].lower(),
        "ada_token": ada["Token"], "info_ada": minted["info_ada"],
    })

    c = cleo["Identity"].lower()
    authority = Authority(n["authority_url"], {
        (SEND, a, b), (RECEIVE, b, a),
        (SEND, b, a), (RECEIVE, a, b),
        (READ, c, a),
    })
    try:
        facts["exchange"] = await exchange(n, ada, bob, cleo)
        facts["unhosted"] = await unhosted(n, minted["app"], bob)
        facts["deletion"] = await deletion(n, ada, bob, minted["bob_second"])
    finally:
        authority.close()
    facts["questions"] = authority.questions
    write_facts(facts)

    x, d = facts["exchange"], facts["deletion"]
    print(f"driver: ada and bob exchanged {len(ASK)}+{len(ANSWER)} B; "
          f"cleo read {len(x['delegated']['read']['Messages'])} of ada's; "
          f"archive moved {x['archived']}; cleo refused={x['to_cleo']['refused']}; "
          f"bob deleted={not d['deleted']['refused']}; authority asked "
          f"{len(authority.questions)} questions")


asyncio.run(main())
