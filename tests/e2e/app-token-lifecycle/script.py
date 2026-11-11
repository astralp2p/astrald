#!/usr/bin/env python3
"""Driver: mint two app tokens, let one expire, delete the other, and probe both.

The token ops are local-only and answer to the User, so the driver acts as the
User on node1 (`user_token` from bootstrap-user-software-key).

Two tokens for the User, minted with `apphost.create_token`:

- `short` lives 3 s (`duration=3s`), and is left to expire;
- `doomed` lives ten minutes and is deleted with `apphost.delete_token`.

Each token is probed three ways: a fresh session (`astral.connect`, whose
handshake presents the token and is refused at once on a bad one), a query on
that session (`apphost.whoami`), and a session opened before the token ended and
held open across it. What a held session does after its token ends is recorded,
not judged: `Guest.onAuthTokenMsg` checks a token once, at the handshake, and
nothing re-checks it per query (mod/apphost/src/guest.go), which is a property
of the code today and not a contract the test should freeze.

The driver acts and judges nothing, except that the before-expiry probes must
land inside the 3 s lifetime: past it the "worked before" evidence is void, and
that is the host being too slow, not astrald.
"""
import asyncio
import datetime
import time

import astral
from astral.errors import AstralError
from astral.session import Session
from astral.stream import Stream

from lib.sessionio import load, write_facts

SHORT = 3       # seconds the short token lives
WAIT = 5        # seconds after minting before the expiry is probed (>= SHORT + margin)
LONG = 600      # seconds the doomed token lives
DEADLINE = 15   # seconds a probe may take; a hung query is not an answer


async def whoami_on(session: Session) -> str:
    """One `apphost.whoami` on a session that already authenticated."""
    conn = await session.route_query("apphost.whoami", timeout=DEADLINE)
    async with Stream(conn) as s:
        return str(await s.value(timeout=DEADLINE))


async def open_session(endpoint: str, token: str) -> dict:
    """A fresh session with the token: did the handshake accept it, and whoami."""
    try:
        async with await astral.connect(endpoint, token=token) as c:
            who = await c.apphost.whoami(timeout=DEADLINE)
    except AstralError as e:
        return {"ok": False, "error": type(e).__name__, "detail": str(e)}
    return {"ok": True, "identity": str(who)}


async def held_query(session: Session) -> dict:
    """A query on a session opened earlier, whatever became of its token."""
    try:
        return {"ok": True, "identity": await whoami_on(session)}
    except AstralError as e:
        return {"ok": False, "error": type(e).__name__, "detail": str(e)}


async def main():
    doc = load()
    n1 = doc["nodes"]["node1"]
    user_id = doc["facts"]["user_id"]
    ep = n1["endpoint"]

    async with await astral.connect(ep, token=doc["facts"]["user_token"]) as user:
        doomed = await user.apphost.create_token(user_id, duration=datetime.timedelta(seconds=LONG))
        short = await user.apphost.create_token(user_id, duration=datetime.timedelta(seconds=SHORT))
        minted_at = time.monotonic()
        lifetime_ns = int(short.expires_at) - time.time_ns()

        # Before: both work through a fresh session, immediately after minting.
        before = {"short": await open_session(ep, short.token),
                  "doomed": await open_session(ep, doomed.token)}
        before_elapsed = time.monotonic() - minted_at

        # Sessions opened now, held across the end of their tokens.
        held_short = await Session.connect(ep, token=short.token)
        held_doomed = await Session.connect(ep, token=doomed.token)
        assert time.monotonic() - minted_at < SHORT, (
            f"the host needed {time.monotonic() - minted_at:.2f}s to open the "
            f"held sessions, past the {SHORT}s lifetime: nothing is proven")

        try:
            # Deletion: the op answers, and the token stops opening sessions.
            ack = await user.call_one(
                f"apphost.delete_token?token={doomed.token}", timeout=DEADLINE)
            deleted = {"ack": getattr(ack, "ASTRAL_TYPE", type(ack).__name__),
                       "fresh": await open_session(ep, doomed.token)}

            # Expiry: wait out the lifetime, with margin, since the mint.
            await asyncio.sleep(max(0.0, WAIT - (time.monotonic() - minted_at)))
            after = {"short": await open_session(ep, short.token),
                     "elapsed_s": round(time.monotonic() - minted_at, 2)}

            # Recorded only: sessions authenticated before their tokens ended.
            held = {"short": await held_query(held_short),
                    "doomed": await held_query(held_doomed)}

            tokens = await user.apphost.list_tokens(user_id)
        finally:
            await held_short.aclose()
            await held_doomed.aclose()

    listed = {t.token for t in tokens}
    write_facts({
        "tokens": {"short": short.token, "doomed": doomed.token},
        "minted": {"short_lifetime_s": round(lifetime_ns / 1e9, 2),
                   "short_listed": short.token in listed,
                   "doomed_listed": doomed.token in listed},
        "before": {**before, "elapsed_s": round(before_elapsed, 2)},
        "deleted": deleted,
        "after": after,
        "held": held,
    })

    def word(r):
        return "works" if r["ok"] else r["error"]
    print(f"driver: before — short {word(before['short'])}, doomed "
          f"{word(before['doomed'])}; deleted doomed — {word(deleted['fresh'])}; "
          f"short after {after['elapsed_s']}s — {word(after['short'])}; held "
          f"sessions afterwards (recorded) — short {word(held['short'])}, "
          f"doomed {word(held['doomed'])}")


asyncio.run(main())
