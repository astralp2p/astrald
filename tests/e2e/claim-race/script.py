#!/usr/bin/env python3
"""Driver: many distinct Users claim one fresh node at the same instant.

Each node of the roster is one round (ROUNDS). On a fresh node the driver
makes CONTENDERS distinct Users from new BIP-39 words. A contender claims with
user.bootstrap, which derives the key and signs inside the op, or with
user.accept_contract, carrying a claim contract the node signed for it
beforehand through auth.sign_contract. Both ops end in the user module's
claim(), the check-and-set its lock makes one transition.

The race: every contender's query is opened first, so every op has passed its
own "node already claimed" check and waits for its body. After SETTLE seconds
the bodies go out together, and every claim reaches claim() while the node is
still unclaimed. Each answer is recorded as it arrives: a win, a refusal with
the node's text, a hang past HANG seconds, or a transport error.

After the race, two late Users claim the claimed node, one through each op.
A late query is read before its body is sent: on a claimed node each op
answers before it reads, and words sent to a closed query reset the
connection (bootstrap-user-op).

The driver judges nothing; verify.py reads each node's state itself.

why the node's token: user.bootstrap and user.join admit the node itself or
the manager (mod/user/src/op_bootstrap.go mayClaim), and the claim window of
auth.sign_contract admits the node alone.
"""
import asyncio
import sys
import time

import astral
from astral.api.bip137sig import mnemonic_local, new_entropy_local, seed_local
from astral.api.crypto import public_key_to_identity
from astral.errors import AstralError, QueryTimeout, RemoteError
from astral.primitives import String16

from lib.sessionio import load, write_facts

BOOTSTRAP, ACCEPT = "user.bootstrap", "user.accept_contract"
CONTENDERS = 12
# The claim op of each contender, per round. In a mixed round one claim wins,
# most often an accept_contract, signed in advance, and the bootstraps still
# deriving then fail the claim rule in signClaim before claim(). The rounds of
# one op race that op's claims against each other in claim().
ROUNDS = {
    "race1": [BOOTSTRAP] * CONTENDERS,
    "race2": [BOOTSTRAP] * CONTENDERS,
    "race3": [ACCEPT] * CONTENDERS,
    "race4": [BOOTSTRAP, ACCEPT] * (CONTENDERS // 2),
}
PATH = "m/1791'/0'/0'/0/0"
# Seconds between the last query opening and the bodies going out: every op
# has then passed its own claimed check and waits on its body.
SETTLE = 0.5
# Seconds a contender waits for its answer before it counts as a hang.
HANG = 30.0
# Seconds a late claim waits for the refusal the op answers before it reads.
EARLY = 5.0


class User:
    """A User made from new words: its identity, and the body of its claim."""

    def __init__(self, identity: str, op: str, body):
        self.identity = identity
        self.op = op
        self.body = body


async def new_user(c, op: str) -> User:
    words = mnemonic_local(new_entropy_local(128))
    key = await c.bip137sig.derive_key(seed_local(words), path=PATH)
    uid = str(public_key_to_identity(await c.crypto.public_key(key)))
    if op == BOOTSTRAP:
        return User(uid, op, String16(" ".join(words)))
    # why store the key: auth.sign_contract signs as the issuer with a key the
    # node holds (import-user-software-key does the same).
    await c.objects.store(key)
    contract = await c.user.new_node_contract(user=uid)
    signed = await c.auth.sign_contract(contract)
    return User(uid, op, signed)


def outcome(user: User, kind: str, detail: str, token: bool = False,
            elapsed: float = 0.0) -> dict:
    """One claim's record. `token` says whether the answer carried one; the
    token itself stays out of the facts."""
    return {"user": user.identity, "op": user.op, "outcome": kind,
            "detail": detail, "token": token, "elapsed": round(elapsed, 3)}


def answered(user: User, answer, elapsed: float) -> dict:
    """A claim the node answered with an object: the op's success shape."""
    if user.op == BOOTSTRAP:
        return outcome(user, "won", type(answer).__name__,
                       token=bool(getattr(answer, "token", "")),
                       elapsed=elapsed)
    return outcome(user, "won", type(answer).__name__, elapsed=elapsed)


async def contend(c, user: User, opened: asyncio.Queue, go: asyncio.Event) -> dict:
    """One contender: open the claim, wait for the start, send, read."""
    async with c.stream(user.op) as s:
        await opened.put(user.identity)
        await go.wait()
        t0 = time.monotonic()
        try:
            await s.send(user.body, timeout=HANG)
            answer = await s.first(timeout=HANG)
        except RemoteError as e:
            return outcome(user, "refused", e.message,
                           elapsed=time.monotonic() - t0)
        except QueryTimeout as e:
            return outcome(user, "hang", f"{type(e).__name__}: {e}",
                           elapsed=time.monotonic() - t0)
        except (AstralError, ConnectionError, OSError) as e:
            return outcome(user, "error", f"{type(e).__name__}: {e}",
                           elapsed=time.monotonic() - t0)
        if answer is None:
            return outcome(user, "error", "the stream ended without an answer",
                           elapsed=time.monotonic() - t0)
        return answered(user, answer, time.monotonic() - t0)


async def race(endpoint: str, token: str, users: list[User]) -> list[dict]:
    async with await astral.connect(endpoint, token=token,
                                    max_concurrency=None) as c:
        opened: asyncio.Queue = asyncio.Queue()
        go = asyncio.Event()
        tasks = [asyncio.create_task(contend(c, u, opened, go)) for u in users]
        for _ in users:
            got = asyncio.create_task(opened.get())
            done, _p = await asyncio.wait({got, *tasks}, timeout=HANG,
                                          return_when=asyncio.FIRST_COMPLETED)
            if got not in done:
                got.cancel()
                for t in done:
                    t.result()  # a contender that failed to open raises here
                raise SystemExit("driver: a claim query did not open within "
                                 f"{HANG}s")
        await asyncio.sleep(SETTLE)
        go.set()
        return list(await asyncio.gather(*tasks))


async def late_claim(c, user: User) -> dict:
    """A claim on the claimed node: its answer is read before the body goes."""
    async with c.stream(user.op) as s:
        try:
            answer = await s.first(timeout=EARLY)
        except RemoteError as e:
            return outcome(user, "refused", e.message)
        except QueryTimeout:
            answer = None  # the op waits for its body: it did not refuse early
        except (AstralError, ConnectionError, OSError) as e:
            return outcome(user, "error", f"{type(e).__name__}: {e}")
        if answer is not None:
            return answered(user, answer, 0.0)
        try:
            await s.send(user.body, timeout=HANG)
            answer = await s.first(timeout=HANG)
        except RemoteError as e:
            return outcome(user, "refused after reading", e.message)
        except (AstralError, ConnectionError, OSError) as e:
            return outcome(user, "error", f"{type(e).__name__}: {e}")
        if answer is None:
            return outcome(user, "error", "the stream ended without an answer")
        return answered(user, answer, 0.0)


async def round_on(node: dict, ops: list[str]) -> dict:
    endpoint, token = node["endpoint"], node["token"]
    async with await astral.connect(endpoint, token=token) as c:
        state = (await c.user.info()).state
        if state == "claimed":
            sys.exit(f"driver: {node['identity'][:16]}… answers state "
                     f"{state!r}; claim-race needs a fresh node")
        users = [await new_user(c, op) for op in ops]
        late = [await new_user(c, op) for op in (BOOTSTRAP, ACCEPT)]

    t0 = time.monotonic()
    results = await race(endpoint, token, users)
    took = time.monotonic() - t0

    async with await astral.connect(endpoint, token=token) as c:
        late_results = [await late_claim(c, u) for u in late]

    return {"state_before": state, "race_seconds": round(took, 3),
            "contenders": results, "late": late_results}


async def main():
    nodes = load()["nodes"]
    facts = {}
    for name, ops in ROUNDS.items():
        facts[name] = await round_on(nodes[name], ops)
        r = facts[name]
        won = [x for x in r["contenders"] if x["outcome"] == "won"]
        kinds = {}
        for x in r["contenders"]:
            kinds[x["outcome"]] = kinds.get(x["outcome"], 0) + 1
        print(f"driver: {name}: {kinds} in {r['race_seconds']}s; won by "
              + ", ".join(f"{x['user'][:16]}… via {x['op']}" for x in won)
              + "; late: " + ", ".join(f"{x['op']} {x['outcome']}"
                                       for x in r["late"]))
    write_facts({"claim_race": facts})


asyncio.run(main())
