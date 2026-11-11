#!/usr/bin/env python3
"""Driver: ask a claimed node to become another User's node, by every claiming op.

node1 belongs to the User bootstrap-user-software-key made. The driver derives
a second User B from other BIP-39 words and, as node1 and then as the User,
asks node1 to change owner: user.bootstrap with B's words, user.bootstrap
generating new words, user.accept_contract with B's contract, user.accept_membership
with B's issuer signature over a contract for node1, and user.join. It prints
each answer and judges nothing: the oracle reads the node.

why those ops: they are the ops that write an active contract (op_bootstrap.go,
op_accept_contract.go, op_accept_membership.go, op_join.go), and each refuses
a claimed node before it reads its input. astrald has no op that imports a
key into a claimed node; import-user-software-key claims through
user.accept_contract, covered here.

why B's signature is made here: user.accept_membership verifies the issuer's
signature after its claimed check, so a contract B really signed is what a
node that skipped the check would accept and activate. node1 holds no key of
B, and its signing refuses B's contract, so the driver signs with B's key in
memory (ECDSA below, no library).

why a call made again when reset: a claimed node answers and closes without
reading the input, and a socket closed with that input unread resets the
connection in place of the answer (bootstrap-user-op does the same). A
refused call changes nothing, so it is made again.
"""
import asyncio
import hashlib
import secrets

import astral
from astral.api.auth import SignedContract
from astral.api.crypto import Signature, public_key_to_identity
from astral.errors import AstralError, QueryTimeout, RemoteError

from lib.sessionio import load, write_facts

# why these words: the BIP-39 test vector with entropy zero, valid and not the
# words of any User a test makes.
RIVAL_WORDS = " ".join(["abandon"] * 11 + ["about"])
PATH = "m/1791'/0'/0'/0/0"
RESET_ATTEMPTS = 5
# How long accept_membership is given to answer before the driver sends input.
ANSWER_FIRST = 1.0
# why a stranger for join: the op refuses a claimed node before it reads node.
STRANGER = "0279be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f81798"

# secp256k1
P = 2**256 - 2**32 - 977
N = 0xFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFEBAAEDCE6AF48A03BBFD25E8CD0364141
G = (0x79BE667EF9DCBBAC55A06295CE870B07029BFCDB2DCE28D959F2815B16F81798,
     0x483ADA7726A3C4655DA4FBFC0E1108A8FD17B448A68554199C47D08FFB10D4B8)


def add(a, b):
    if a is None:
        return b
    if b is None:
        return a
    if a[0] == b[0] and (a[1] + b[1]) % P == 0:
        return None
    if a == b:
        m = 3 * a[0] * a[0] * pow(2 * a[1], -1, P) % P
    else:
        m = (b[1] - a[1]) * pow(b[0] - a[0], -1, P) % P
    x = (m * m - a[0] - b[0]) % P
    return x, (m * (a[0] - x) - a[1]) % P


def mul(k, point=G):
    result = None
    while k:
        if k & 1:
            result = add(result, point)
        point = add(point, point)
        k >>= 1
    return result


def der_int(v: int) -> bytes:
    b = v.to_bytes((v.bit_length() + 8) // 8, "big")
    return b"\x02" + bytes([len(b)]) + b


def sign_asn1(secret: bytes, digest: bytes) -> Signature:
    """An "asn1" ECDSA signature over a 32-byte digest, the scheme the node
    verifies a contract's issuer with."""
    d, e = int.from_bytes(secret, "big"), int.from_bytes(digest, "big")
    while True:
        k = secrets.randbelow(N - 1) + 1
        r = mul(k)[0] % N
        s = pow(k, -1, N) * (e + r * d) % N
        if r and s:
            break
    s = min(s, N - s)
    body = der_int(r) + der_int(s)
    return Signature(scheme="asn1", data=b"\x30" + bytes([len(body)]) + body)


async def attempt(call) -> dict:
    """One call, made again while its connection is reset: whether the node
    refused it and as what, as the node's own text."""
    reset = ""
    for _ in range(RESET_ATTEMPTS):
        try:
            await call()
        except RemoteError as e:
            return {"refused": True, "error": "RemoteError", "text": e.message}
        except ConnectionError as e:
            reset = f"{type(e).__name__}: {e}"
            continue
        except AstralError as e:
            return {"refused": True, "error": type(e).__name__, "text": str(e)}
        return {"refused": False, "error": "", "text": "answered"}
    return {"refused": True, "error": "reset", "text": reset}


async def membership(c, contract, issuer_sig) -> None:
    """user.accept_membership, reading its answer before sending its input.

    why: the op answers a claimed node before it reads either object, and a
    client that has sent both meets a reset in place of the answer on every
    attempt (measured: five of five). An op that waits for input stays silent,
    so the driver sends the contract and signature once ANSWER_FIRST passes.
    """
    async with c.stream("user.accept_membership") as s:
        try:
            await s.first(timeout=ANSWER_FIRST)
            raise AstralError("user.accept_membership answered with no input")
        except QueryTimeout:
            pass
        await s.send(contract)
        await s.send(issuer_sig)
        await s.first(timeout=15)


async def rival(c) -> tuple[str, bytes]:
    """User B: its identity, and its key in memory only."""
    seed = await c.bip137sig.seed(RIVAL_WORDS)
    key = await c.bip137sig.derive_key(seed, path=PATH)
    return str(public_key_to_identity(await c.crypto.public_key(key))), bytes(key.key)


async def probes(c, rival_id: str, secret: bytes) -> dict:
    """Every claiming op, once, as the session c."""
    contract = await c.user.new_node_contract(user=rival_id)
    issuer_sig = sign_asn1(secret, contract.signable_hash())
    # why the issuer's signature twice: node1 will not sign as the subject, so
    # no fully signed contract for B exists; accept_contract refuses first.
    signed = SignedContract(contract=contract, issuer_sig=issuer_sig,
                            subject_sig=issuer_sig)
    return {
        "bootstrap_words": await attempt(lambda: c.user.bootstrap(RIVAL_WORDS)),
        "bootstrap_generate": await attempt(lambda: c.user.bootstrap(None)),
        "accept_contract": await attempt(lambda: c.user.accept_contract(signed)),
        "accept_membership": await attempt(
            lambda: membership(c, contract, issuer_sig)),
        "join": await attempt(lambda: c.user.join(STRANGER)),
    }


async def main():
    doc = load()
    n1, facts = doc["nodes"]["node1"], doc["facts"]
    attempts = {}
    async with await astral.connect(n1["endpoint"], token=n1["token"]) as c:
        rival_id, secret = await rival(c)
        attempts["node"] = await probes(c, rival_id, secret)
    async with await astral.connect(n1["endpoint"], token=facts["user_token"]) as u:
        attempts["user"] = await probes(u, rival_id, secret)

    write_facts({})
    for caller, answers in attempts.items():
        for op, a in answers.items():
            print(f"driver: {op} as {caller} for {rival_id[:16]}…: "
                  f"{'refused' if a['refused'] else 'ANSWERED'} {a['text']}")


asyncio.run(main())
