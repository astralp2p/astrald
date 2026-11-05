"""nearby over apphost: the ops a driver asks, and a status as facts.

why jsonops: astral-py binds no nearby op and decodes no nearby type, and a
status's attachments are the node's own JSON (lib/jsonops.py).

The wire is the cutover's (astrald intern0/dev/nearby-tightening, astral-go
api/nearby): nearby.list answers one mod.nearby.status per cache entry, its
Identity nil for a stealth status the node does not recognise and its
Attachments as received, the mod.nearby.status_proof last.
"""
import re

from astral.errors import AstralError

from lib import jsonops

PROFILE = "mod.nearby.public_profile"
HINT = "mod.nearby.stealth_hint"
PROOF = "mod.nearby.status_proof"
FLAG = "mod.nearby.flag"
MODE = "mod.nearby.mode"
SILENT, VISIBLE, STEALTH = 0, 1, 2
# the stored mode, in the node's tree (mod/nearby/src/deps.go modePath)
MODE_PATH = "/mod/nearby/mode"

_HEX_ID = re.compile(r"[0-9a-f]{66}")


def identity(value) -> str | None:
    """An identity as the session view writes one; None for nil or zero.

    note: the zero identity is written as a name, not as hex
    (astral-go astral/identity.go MarshalJSON).
    """
    return value if isinstance(value, str) and _HEX_ID.fullmatch(value) else None


def summary(status: dict) -> dict:
    """A mod.nearby.status as facts: whom the node took it for, what it
    carried, and its proof's nonce and timestamp, which a newer status of the
    same node replaces."""
    atts = [a for a in status.get("Attachments") or [] if isinstance(a, dict)]
    profile = next((a.get("Object") for a in atts if a.get("Type") == PROFILE), None)
    proof = next((a.get("Object") for a in atts if a.get("Type") == PROOF), None)
    proof = proof if isinstance(proof, dict) else {}
    return {
        "identity": identity(status.get("Identity")),
        "types": [a.get("Type") for a in atts],
        "flags": [a.get("Object") for a in atts if a.get("Type") == FLAG],
        "profile": None if not isinstance(profile, dict) else {
            "node": identity(profile.get("NodeID")),
            "user": identity(profile.get("UserID")),
            "alias": profile.get("NodeAlias") or "",
        },
        "nonce": proof.get("Nonce"),
        "timestamp": proof.get("Timestamp"),
    }


async def statuses(c) -> list:
    """Every live entry of the node's nearby cache, summarised."""
    return [summary(d["Object"])
            for d in await jsonops.stream(c, "nearby.list?out=json")]


def of(listing: list, node: str) -> dict | None:
    """The entry of `node` in a listing, if the node lists it as verified."""
    return next((s for s in listing if s["identity"] == node), None)


async def answer(c, qs: str, body: dict | None = None, eos: bool = False) -> dict:
    """What an op that answers `ack` said: its envelope types, or its refusal."""
    try:
        docs = await jsonops.call(c, qs, body, eos=eos)
    except AstralError as e:
        return {"refused": True, "detail": f"{type(e).__name__}: {e}"}
    if docs and docs[0]["Type"] == "error_message":
        return {"refused": True, "detail": docs[0]["Object"]}
    return {"refused": False, "types": [d["Type"] for d in docs]}


async def scan(c) -> dict:
    return await answer(c, "nearby.scan?out=json")


async def broadcast(c) -> dict:
    return await answer(c, "nearby.broadcast?out=json")


async def set_mode(c, mode: int) -> dict:
    """Store the nearby mode, as tree.set's body form.

    why the body: tree.set's `value` argument parses text, and mod.nearby.mode
    has no text form (astral-go api/tree/client/server.go setSingle); the body
    carries the object as JSON. nearby has no op of its own for the mode.
    """
    return await answer(c, f"tree.set?in=json&out=json&path={MODE_PATH}",
                        {"Type": MODE, "Object": mode}, eos=True)


async def stored_mode(c):
    """The mode stored in the node's tree: 0, 1 or 2, or None when unset."""
    try:
        docs = await jsonops.stream(c, f"tree.get?out=json&path={MODE_PATH}")
    except (jsonops.OpError, AstralError):
        return None
    return docs[0].get("Object") if docs and docs[0].get("Type") == MODE else None
