#!/usr/bin/env python3
"""Oracle: a token works until it expires or is deleted, and then opens nothing.

The driver's facts say what each token did when probed. The oracle checks them
in the order that makes each verdict meaningful, then asks node1 itself.

1. The controls. A token refused later proves nothing unless the same token was
   accepted earlier, on the same node, as the same identity: `before` holds both
   tokens working inside the short token's lifetime, and the node's own answer
   to `apphost.whoami` is the User.
2. The two ends. The deleted token and the expired token each fail at the
   handshake with `AuthFailed` (`auth_failed`), not with a timeout or a lost
   node, which would leave this green with the check gone. The expiry probe
   ran at least five seconds after minting a three second token.
3. The node's state, measured here and not taken from the driver: the User's
   token still opens a session, the deleted token and the expired token are
   still refused, and `apphost.list_tokens` no longer holds the deleted token.

What a session authenticated before its token ended does afterwards is printed
and never judged: the node checks a token at the handshake only, which is a
property of the code and not a promise the test holds it to.
"""
import asyncio

import astral
from astral.errors import AuthFailed

from lib.sessionio import load

SHORT = 3   # the lifetime the driver asked for, in seconds
WAIT = 5    # the least time that must pass before expiry counts as probed


async def refused(endpoint: str, token: str):
    """None when the handshake refused the token with auth_failed, else what happened."""
    try:
        async with await astral.connect(endpoint, token=token, query_timeout=15) as c:
            who = await c.apphost.whoami(timeout=15)
    except AuthFailed:
        return None
    except Exception as e:  # a refusal that is not auth_failed is not this refusal
        return f"{type(e).__name__}: {e}"
    return f"accepted, whoami {who}"


async def main():
    doc = load()
    n1 = doc["nodes"]["node1"]
    f = doc["facts"]
    user_id = f["user_id"]
    short, doomed = f["tokens"]["short"], f["tokens"]["doomed"]

    # 1. controls
    assert 2.0 <= f["minted"]["short_lifetime_s"] <= 4.0, (
        f"create_token with a 3s duration answered a token that lives "
        f"{f['minted']['short_lifetime_s']}s — the duration argument is not honoured")
    assert f["before"]["elapsed_s"] < SHORT, (
        f"the before-expiry probes took {f['before']['elapsed_s']}s, past the "
        f"{SHORT}s lifetime: they prove nothing")
    for name in ("short", "doomed"):
        r = f["before"][name]
        assert r["ok"], (
            f"the {name} token did not open a session before it ended: "
            f"{r['error']}: {r['detail']} — a minted token must work")
        assert r["identity"] == user_id, (
            f"the {name} token opened a session as {r['identity']}, not as the "
            f"User {user_id}")

    # 2. the two ends, as the driver recorded them
    assert f["deleted"]["ack"], "apphost.delete_token answered nothing"
    r = f["deleted"]["fresh"]
    assert not r["ok"], (
        f"the deleted token still opened a session as {r['identity']} — "
        "deleting a token does not stop it authenticating")
    assert r["error"] == "AuthFailed", (
        f"the deleted token failed with {r['error']}: {r['detail']}, not "
        "auth_failed — the handshake did not refuse it")
    a = f["after"]
    assert a["elapsed_s"] >= WAIT, (
        f"the expiry probe ran {a['elapsed_s']}s after minting, before the "
        f"{WAIT}s the test allows for a {SHORT}s token")
    r = a["short"]
    assert not r["ok"], (
        f"the {SHORT}s token still opened a session as {r['identity']} "
        f"{a['elapsed_s']}s after minting — an expired token authenticates")
    assert r["error"] == "AuthFailed", (
        f"the expired token failed with {r['error']}: {r['detail']}, not "
        "auth_failed — the handshake did not refuse it")

    # 3. node1, asked now
    async with await astral.connect(n1["endpoint"], token=f["user_token"]) as c:
        who = await c.apphost.whoami()
        listed = {t.token for t in await c.apphost.list_tokens(user_id)}
    assert str(who) == user_id, f"the User's own token answered whoami as {who}"
    for name, token in (("deleted", doomed), ("expired", short)):
        why = await refused(n1["endpoint"], token)
        assert why is None, (
            f"the {name} token is not refused by node1 now: {why}")
    assert doomed not in listed, (
        "apphost.list_tokens still holds the token apphost.delete_token removed")

    held = f["held"]
    print("oracle: a minted token worked for the User before it ended; the "
          f"deleted token and the {SHORT}s token (probed {a['elapsed_s']}s after "
          "minting) each fail the handshake with auth_failed, and node1 still "
          "refuses both; the deleted token is gone from list_tokens")
    for name in ("short", "doomed"):
        h = held[name]
        outcome = (f"worked as {h['identity']}" if h["ok"]
                   else f"failed with {h['error']}")
        print(f"oracle: recorded, not judged — a session opened with the {name} "
              f"token before it ended, queried afterwards: {outcome}")


asyncio.run(main())
