#!/usr/bin/env python3
"""Driver: appoint a manager on a fresh node, and claim the node as the manager.

The driver acts and judges nothing. It reads user.info at each step, appoints
the manager from a session with the node's token, asks for a second
appointment, asks
as the manager for an op the unclaimed gate list omits, serves an op as the
manager on the unclaimed node and queries it, registers an ordinary app that
asks to claim, claims as the manager, and asks for a third appointment.

why jsonops: astral-py has no binding for sys.register_manager or
user.bootstrap, and the ops' JSON form is the node's own.
"""
import asyncio
import sys

import astral
from astral.errors import AstralError

from lib import jsonops
from lib.sessionio import load, write_facts

REQUEST = {"Type": "mod.sys.register_manager_request", "Object": {"Name": "manager"}}
SERVED_OP = "test.manager_setup.whoami"


async def attempt(coro) -> dict:
    """Run one probe; report whether it was refused, and how it read."""
    try:
        answer = await coro
    except (AstralError, jsonops.OpError) as e:
        return {"refused": True, "detail": f"{type(e).__name__}: {e}"}
    return {"refused": False, "detail": "answered", "answer": answer}


async def info(c) -> dict:
    i = await jsonops.value(c, "user.info?out=json")
    return {"state": i["State"], "manager": i["Manager"] or ""}


async def appoint(c) -> dict:
    return await attempt(jsonops.value(c, "sys.register_manager?in=json&out=json", REQUEST))


async def serve_unclaimed(m, n, manager_id) -> dict:
    """Serve one op as the manager on the unclaimed node; query it as the node.

    why: the manager answers the policies delegated to it only through a
    handler, and registering one takes apphost.bind and
    apphost.register_handler, which the unclaimed gate list must hold.
    """
    async def whoami(q):
        async with await q.accept() as stream:
            await stream.send(q.caller)
            await stream.send_eos()

    async def run() -> str:
        # why host_ip: the node dials the handler, and the default listener,
        # a unix socket or 127.0.0.1, is on the host only in a local world
        async with await m.serve(endpoint=f"tcp:{n['host_ip']}:0",
                                 ready_timeout=15) as svc:
            svc.mount(SERVED_OP, whoami)
            async with await astral.connect(n["endpoint"], token=n["token"]) as caller:
                return str(await caller.call_one(SERVED_OP, target=manager_id, timeout=15))

    result = await attempt(run())
    if not result["refused"]:
        result["detail"] = result.pop("answer")
    return result


async def main():
    n = load()["nodes"]["managed"]
    facts = {}

    # why the node token: the E2E tokenless list omits sys.register_manager.
    async with await astral.connect(n["endpoint"], token=n["token"]) as c:
        facts["fresh"] = await info(c)
        first = await appoint(c)
        facts["second"] = await appoint(c)
        facts["unclaimed"] = await info(c)
    async with await astral.connect(n["endpoint"]) as c:          # anonymous
        app = await jsonops.value(c, "apphost.register?in=json&out=json",
                                  {"Type": "mod.apphost.register_request", "Object": {}})
    if first["refused"]:
        sys.exit(f"driver: the first sys.register_manager answered {first['detail']}")
    manager = first.pop("answer")
    facts["manager"] = manager["Identity"]

    async with await astral.connect(n["endpoint"], token=manager["Token"]) as m:
        facts["outside_gate"] = await attempt(m.apphost.list_tokens())
        facts["served"] = await serve_unclaimed(m, n, manager["Identity"])
        async with await astral.connect(n["endpoint"], token=app["Token"]) as a:
            facts["app_claim"] = await attempt(
                jsonops.value(a, "user.bootstrap?generate=true&out=json"))
            facts["app_claim"].pop("answer", None)
        claim = await jsonops.value(m, "user.bootstrap?generate=true&out=json")
    facts["claim"] = {"user": claim["Identity"], "token": bool(claim["Token"])}

    async with await astral.connect(n["endpoint"], token=claim["Token"]) as u:
        facts["claimed"] = await info(u)
        facts["third"] = await appoint(u)

    for k in ("second", "third", "outside_gate"):
        facts[k].pop("answer", None)
    write_facts({"manager_setup": facts})
    print(f"driver: manager {facts['manager']}; states {facts['fresh']['state']} → "
          f"{facts['unclaimed']['state']} → {facts['claimed']['state']}")


asyncio.run(main())
