#!/usr/bin/env python3
"""Driver: an app requests discovery scoped to one service, then a second.

The admin selects the accept_all grant request policy; a fresh app asks
apphost.request_grant for one discovery scope at a time with a
mod.apphost.request_grant_request, and the driver records what services.discover
admits, what apphost.list_grants lists, and what apphost.revoke -id leaves.
The policy is cleared at the end, as the node entered it.
"""
import asyncio

import astral

from lib import jsonops
from lib.sessionio import load, write_facts

DISCOVERY = "mod.services.service_discovery_action"


def scoped(*services: str) -> dict:
    """A request_grant_request for one discovery permit scoped to services."""
    constraints = [{"Type": "mod.services.discovery_scope",
                    "Object": {"Rules": [{"Services": list(services), "Nodes": []}]}}]
    return request([{"Action": DISCOVERY, "Constraints": constraints, "Delegation": 0}])


def request(permits: list) -> dict:
    return {"Type": "mod.apphost.request_grant_request",
            "Object": {"Permits": permits, "Duration": 0}}


async def answer(client, qs: str, body: dict | None = None) -> str:
    """The type of the first object the op answered, or its error text."""
    docs = await jsonops.call(client, qs, body)
    if not docs:
        return "nothing"
    if docs[0]["Type"] == "error_message":
        return docs[0]["Object"]
    return docs[0]["Type"]


async def discover(app, service: str) -> str:
    """`granted` when services.discover reports service granted, `not granted`
    when its services.not_granted names it, else the error it answers."""
    docs = await jsonops.call(app, f"services.discover?services={service}&out=json")
    errors = [d["Object"] for d in docs if d["Type"] == "error_message"]
    if errors:
        return errors[0]
    denied = [s for d in docs if d["Type"] == "services.not_granted" for s in d["Object"]["Services"]]
    return "not granted" if service in denied else "granted"


async def grants(admin, app_id: str) -> list:
    """The app's discovery grants as list_grants reports them."""
    return [{"id": d["Object"]["ID"],
             "services": [s for c in d["Object"]["Permit"]["Constraints"] or []
                          for r in c["Object"]["Rules"] for s in r["Services"]],
             "expires_at": d["Object"]["ExpiresAt"]}
            for d in await jsonops.stream(
                admin, f"apphost.list_grants?identity={app_id}&out=json")
            if d["Object"]["Permit"]["Action"] == DISCOVERY]


async def main():
    n1 = load()["nodes"]["node1"]
    facts = {}

    async with await astral.connect(n1["endpoint"], token=n1["token"]) as admin:
        facts["set_accept_all"] = await answer(
            admin, "apphost.set_grant_request_policy?mode=accept_all&out=json")
        try:
            token = await jsonops.value(
                admin, "apphost.register?in=json&out=json",
                body={"Type": "mod.apphost.register_request", "Object": {"Name": "request-grant-scope"}})
            app_id = token["Identity"]
            async with await astral.connect(n1["endpoint"], token=token["Token"]) as app:
                facts["inference_before"] = await discover(app, "inference")
                facts["request_inference"] = await answer(
                    app, "apphost.request_grant?in=json&out=json", scoped("inference"))
                facts["inference_after"] = await discover(app, "inference")
                facts["chat_after_inference"] = await discover(app, "chat")
                facts["request_unscoped"] = await answer(
                    app, "apphost.request_grant?in=json&out=json", request([{"Action": DISCOVERY}]))
                facts["request_chat"] = await answer(
                    app, "apphost.request_grant?in=json&out=json", scoped("chat"))
                facts["grants_two"] = await grants(admin, app_id)

                chat = [g["id"] for g in facts["grants_two"] if g["services"] == ["chat"]]
                facts["revoke_chat"] = await answer(
                    admin, f"apphost.revoke?identity={app_id}&id={chat[0] if chat else 0}&out=json")
                facts["grants_left"] = await grants(admin, app_id)
                facts["inference_after_revoke"] = await discover(app, "inference")
                facts["chat_after_revoke"] = await discover(app, "chat")
        finally:
            facts["restore_policy"] = await answer(
                admin, "apphost.set_grant_request_policy?out=json")

    print(f"driver: {facts}")
    write_facts(facts)


if __name__ == "__main__":
    asyncio.run(main())
