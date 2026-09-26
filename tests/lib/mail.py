"""The mod/messaging ops, called over apphost the way an app calls them.

Each call takes a client authenticated as the identity it acts for: the admin
ops as a caller holding `mod.auth.admin_manage_apps_action`, and the mail ops
as the participant whose own boxes they read and write. No mail op names an
owner; the caller is the owner. A read may name another mailbox, which makes it
a delegated read the node asks the authority about.

Objects travel as the node's JSON envelopes (lib/jsonops.py). An envelope's
keys are the Go field names: `Identity`, `Token`, `ID`, `Box`, `FetchedAt`.
"""
from astral import querystring

from lib import jsonops

SEND_REQUEST = "messaging.send_message_request"
READ_REQUEST = "messaging.read_messages_request"
SIGNED_CONTRACT = "mod.auth.signed_contract"


async def create_identity(admin, alias: str) -> dict:
    """A new participant's messaging.identity_credential."""
    return await jsonops.value(admin, querystring.build(
        "messaging.create_identity", {"alias": alias, "out": "json"}))


async def identity(admin, name: str) -> dict:
    """A participant's messaging.identity_info. An unknown one raises OpError."""
    return await jsonops.value(admin, querystring.build(
        "messaging.identity", {"identity": name, "out": "json"}))


async def delete_identity(admin, name: str) -> None:
    await jsonops.value(admin, querystring.build(
        "messaging.delete_identity", {"identity": name, "out": "json"}))


async def send(client, to: str, content: str, parent: str = "") -> str:
    """The id the message is stored under. A refusal raises OpError."""
    req = {"To": to, "Content": content}
    if parent:
        req["ParentID"] = parent
    return await jsonops.value(
        client, "messaging.send_message?in=json&out=json",
        {"Type": SEND_REQUEST, "Object": req})


async def list_messages(client, **args) -> list:
    """The envelopes of one list of a mailbox. `args` are the op's own:
    `list`, `from`, `to`, `since`, `unread_only`, `awaiting_pickup`, and
    `mailbox`, which names a mailbox other than the caller's own."""
    docs = await jsonops.stream(client, querystring.build(
        "messaging.list_messages", {**args, "out": "json"}))
    return [d["Object"] for d in docs]


async def read(client, *refs, mailbox: str = "", children: str = "") -> dict:
    """A messaging.read_messages_result for (box, id) refs, from the mailbox
    `mailbox` names (hex identity), or the caller's own when it names none.
    `children` is `none`, `envelopes` or `full`; empty reads as `envelopes`."""
    req = {"Refs": [{"Box": box, "ID": id} for box, id in refs]}
    if mailbox:
        req["Mailbox"] = mailbox
    if children:
        req["Children"] = children
    return await jsonops.value(
        client, "messaging.read_messages?in=json&out=json",
        {"Type": READ_REQUEST, "Object": req})


async def wait(client, timeout: str, since: int = 0) -> dict:
    """A messaging.wait_result, parked for at most `timeout` (`10s`)."""
    return await jsonops.value(client, querystring.build(
        "messaging.wait", {"timeout": timeout, "since": since, "out": "json"}))


async def archive(client, box: str, id: str, undo: bool = False) -> bool:
    """Whether this call moved the message."""
    args = {"box": box, "id": id, "out": "json"}
    if undo:
        args["undo"] = True
    result = await jsonops.value(client, querystring.build(
        "messaging.archive", args))
    return result["Changed"]


async def contracts_of(admin, issuer: str) -> list:
    """The signed contracts `issuer` issued, as the node's local repository
    stores them, each as its full {Type, Object} envelope.

    why the repository and not auth: auth serves no read of its index, and
    every contract this node signs is stored where objects.scan reaches it.
    """
    out = []
    for doc in await jsonops.stream(admin, "objects.scan?repo=local&out=json"):
        loaded = await jsonops.call(admin, querystring.build(
            "objects.load", {"id": doc["Object"], "out": "json"}))
        if not loaded or loaded[0]["Type"] != SIGNED_CONTRACT:
            continue
        contract = loaded[0]["Object"]["Contract"]
        if contract["Issuer"].lower() == issuer.lower():
            out.append(loaded[0])
    return out


def permits(envelope: dict) -> list:
    """The actions a signed contract envelope permits, with their delegation
    and constraints."""
    return [(p["Action"], p["Delegation"], p["Constraints"])
            for p in envelope["Object"]["Contract"]["Permits"]]
