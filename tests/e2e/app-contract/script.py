#!/usr/bin/env python3
"""Driver: a contract is what makes an app addressable — on its host and off it.

Three acts against two apps that serve throughout. The apps, their handlers,
their tokens and the queries are identical in every act; the only thing that
changes is whether node1 holds a signed contract naming the app. That is the
control the test rests on — a query that fails and then succeeds with nothing
else touched cannot be failing for another reason.

  1. no contract          — neither node1 nor node2 may reach the app
  2. apphost.install_app  — create, sign, index and record in one call
  3. new_app_contract + sign_app_contract — the same reachability, provisioned
                            the long way, so both paths are shown to work

Each act asks twice, from node1 and from node2, because a contract is supposed
to do two separable things: make the app addressable on the node that hosts it,
and — through the push to the local user swarm that signing performs — make it
addressable from a sibling holding no key of its own. Asking only from the host
would call the mechanism proven while half of it never ran.

An app identity is minted the way `import-user-software-key` mints the User's:
derive a key, store it so crypto indexes it as a signer, then take the identity
from its public key. `secp256k1.new` hands back a key the node does not keep,
and a node that does not hold the key cannot sign a contract for it.

The peer asks **as the User**, not as node2 itself. That is the tree's standing
convention — `read-remote-peer` states the rule and every cross-node test
follows it — and it is what the mechanism requires: relay authorization is
written for the user (`mod/user/src/authorizers.go:40`), and an app's reach is
defined over one User's own nodes. A query issued under node2's node identity
takes `mux.go`'s plain-frame branch, which carries no target, so the relay hop
arrives at node1 addressed to node1 and is refused in microseconds. An access
token is node-local, so node1's `user_token` does not authenticate on node2 and
the peer mints its own for the same identity.

Reachability is eventual rather than instant on the peer side — a contract
lands on the sibling within milliseconds of the push, but the first ask can
still race it — so those asks poll to a deadline instead of asking once.
"""
import asyncio

import astral
from astral.api.crypto import public_key_to_identity
from astral.errors import AstralError

from lib.sessionio import load, write_facts

OP = "test.app_contract.absorb"
# The same phrase import-user-software-key carries — a valid BIP-39 mnemonic is
# a checksummed thing, not a sentence anyone can make up. These apps derive at
# their own paths, so they share nothing with that test's User but the entropy,
# and that test does not run in this chain.
SEED_PHRASE = ("horse soldier imitate stool square buyer verb party enjoy "
               "result jazz rabbit trigger file benefit cloth term change")
INSTALLED_PATH = "m/44'/0'/7'/0/0"
SIGNED_PATH = "m/44'/0'/8'/0/0"

# One payload per (act, asker): every reach leaves a distinct object, so the
# oracle judges each one separately by content-addressed presence.
PAYLOADS = {
    "no-contract.host": "app-contract-act1-host-before-any-contract",
    "no-contract.peer": "app-contract-act1-peer-before-any-contract",
    "installed.host": "app-contract-act2-host-through-install-app",
    "installed.peer": "app-contract-act2-peer-through-install-app",
    "signed.host": "app-contract-act3-host-through-sign-app-contract",
    "signed.peer": "app-contract-act3-peer-through-sign-app-contract",
}

REACH_DEADLINE = 8.0


async def mint_app(c, seed, path):
    """An identity node1 holds the key for, with no contract of any kind yet."""
    privkey = await c.bip137sig.derive_key(seed, path=path)
    await c.objects.store(privkey)
    return str(public_key_to_identity(await c.crypto.public_key(privkey)))


async def ask(c, app_id, payload):
    """One query, always the same shape. None when it did not get through."""
    try:
        return str(await c.call_one(
            f"{OP}?payload={payload}", target=app_id, timeout=10))
    except AstralError:
        return None


async def ask_until(c, app_id, payload, deadline=REACH_DEADLINE):
    """Poll for reachability. A contract reaches a peer when the swarm says so."""
    loop = asyncio.get_running_loop()
    end = loop.time() + deadline
    while True:
        got = await ask(c, app_id, payload)
        if got is not None or loop.time() >= end:
            return got
        await asyncio.sleep(0.5)


async def main():
    doc = load()
    n1, n2 = doc["nodes"]["node1"], doc["nodes"]["node2"]
    facts = {"op": OP, "payloads": PAYLOADS}

    async with await astral.connect(n1["endpoint"], token=n1["token"]) as host:
        seed = await host.bip137sig.seed(SEED_PHRASE)
        installed_id = await mint_app(host, seed, INSTALLED_PATH)
        signed_id = await mint_app(host, seed, SIGNED_PATH)
        facts["installed_id"] = installed_id
        facts["signed_id"] = signed_id

        async def absorb(q):
            payload = q.params["payload"].encode()
            async with host.objects.create() as w:
                await w.write(payload)
                object_id = await w.commit()
            async with await q.accept() as stream:
                await stream.send(object_id)
                await stream.send_eos()

        installed_token = (await host.apphost.create_token(installed_id)).token
        signed_token = (await host.apphost.create_token(signed_id)).token

        async with await astral.connect(n1["endpoint"],
                                        token=installed_token) as app_a, \
                   await astral.connect(n1["endpoint"],
                                        token=signed_token) as app_b:
            svc_a = await app_a.serve()
            svc_b = await app_b.serve()
            svc_a.mount(OP, absorb)
            svc_b.mount(OP, absorb)
            try:
                async with await astral.connect(n2["endpoint"],
                                                token=n2["token"]) as n2c:
                    peer_token = (await n2c.apphost.create_token(
                        doc["facts"]["user_id"])).token
                async with await astral.connect(n2["endpoint"],
                                                token=peer_token) as peer:
                    # Act 1 — both apps serving, named by no contract at all.
                    facts["no-contract.host"] = await ask(
                        host, installed_id, PAYLOADS["no-contract.host"])
                    facts["no-contract.peer"] = await ask(
                        peer, installed_id, PAYLOADS["no-contract.peer"])

                    # Act 2 — install_app, then the same two queries.
                    await host.apphost.install_app(installed_id)
                    facts["installed.host"] = await ask_until(
                        host, installed_id, PAYLOADS["installed.host"])
                    facts["installed.peer"] = await ask_until(
                        peer, installed_id, PAYLOADS["installed.peer"])

                    # Act 3 — the long way round, same outcome expected.
                    contract = await host.apphost.new_app_contract(signed_id)
                    await host.apphost.sign_app_contract(contract)
                    facts["signed.host"] = await ask_until(
                        host, signed_id, PAYLOADS["signed.host"])
                    facts["signed.peer"] = await ask_until(
                        peer, signed_id, PAYLOADS["signed.peer"])
            finally:
                await svc_a.aclose()
                await svc_b.aclose()

    write_facts(facts)
    reached = sorted(k for k in PAYLOADS if facts[k] is not None)
    print(f"driver: reached {reached or 'nothing'}")


asyncio.run(main())
