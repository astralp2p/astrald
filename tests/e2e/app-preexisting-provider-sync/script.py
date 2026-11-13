#!/usr/bin/env python3
"""Driver: a provider app registered on a node before the node is adopted is
discovered and called from the User's node after the adoption.

The provider app runs in a Go program (driver/, `provider` mode) that stays up
across the whole flow; astral-py drives the setup of the User on ppsyncuser and
the adoption of ppsyncjoiner (the adopt-node steps); the Go program's
`consumer` mode then finds the provider with reach=swarm and calls it. astral-py
speaks the pre-v1 Services wire, so every services call is in Go.
"""
import asyncio
import json
import os
import subprocess
import sys
from pathlib import Path

import astral
from astral.api.crypto import public_key_to_identity

from lib.sessionio import load, write_facts

ROOT = Path(__file__).resolve().parents[3]
DRIVER = "./tests/e2e/app-preexisting-provider-sync/driver"
ENV = {**os.environ, "GOFLAGS": os.environ.get("GOFLAGS", "-buildvcs=false")}
SESSION = os.environ["ASTRAL_TESTS_SESSION"]


def build() -> str:
    exe = Path(SESSION).parent / "ppsync-driver"
    subprocess.run(["go", "build", "-o", str(exe), DRIVER], cwd=ROOT, env=ENV, check=True)
    return str(exe)


async def bootstrap_user(n: dict) -> tuple[str, str]:
    """A software User key on the node, its node contract, a User token."""
    async with await astral.connect(n["endpoint"], token=n["token"]) as c:
        entropy = await c.bip137sig.new_entropy(bits=256)
        mnemonic = await c.bip137sig.mnemonic(entropy)
        seed = await c.bip137sig.seed(mnemonic)
        privkey = await c.bip137sig.derive_key(seed, path="m/1791'/0'/0'/0/0")
        await c.objects.store(privkey)
        user_pub = await c.crypto.public_key(privkey)
        user_id = str(public_key_to_identity(user_pub))
        contract = await c.user.new_node_contract(user=user_id)
        signed = await c.auth.sign_contract(contract)
        await c.user.accept_contract(signed)
        access = await c.apphost.create_token(user_id)
    return user_id, access.token


async def adopt(home: dict, joiner: dict, user_token: str) -> None:
    """Explicit lan_endpoints both ways, a direct link, then user.adopt."""
    async with await astral.connect(home["endpoint"], token=home["token"]) as c:
        await c.nodes.add_endpoint(joiner["identity"], joiner["lan_endpoint"], experimental=True)
    async with await astral.connect(joiner["endpoint"], token=joiner["token"]) as c:
        await c.nodes.add_endpoint(home["identity"], home["lan_endpoint"], experimental=True)
    async with await astral.connect(home["endpoint"], token=user_token) as c:
        await c.nodes.new_link(joiner["identity"], endpoint=joiner["lan_endpoint"],
                               strategies="basic", experimental=True, timeout=60.0)
        await c.user.adopt(joiner["identity"])


async def state_of(n: dict) -> str:
    async with await astral.connect(n["endpoint"], token=n["token"]) as c:
        return str((await c.user.info()).state)


async def main():
    doc = load()
    home, joiner = doc["nodes"]["ppsyncuser"], doc["nodes"]["ppsyncjoiner"]
    exe = build()
    facts = {}

    user_id, user_token = await bootstrap_user(home)
    facts["joiner_state_before"] = await state_of(joiner)

    provider = subprocess.Popen([exe, "provider", SESSION], cwd=ROOT, env=ENV,
                                stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                                text=True)
    try:
        line = provider.stdout.readline()
        if not line:
            sys.exit(f"driver: the provider exited {provider.wait()} before advertising")
        provider_id = json.loads(line)["provider"]
        facts["provider"] = provider_id

        # the provider is now registered and advertising on a node that is
        # not in the User's swarm; only now does the adoption happen
        await adopt(home, joiner, user_token)
        facts["joiner_state_after"] = await state_of(joiner)

        proc = subprocess.run([exe, "consumer", SESSION, provider_id], cwd=ROOT,
                              env=ENV, capture_output=True, text=True)
        sys.stderr.write(proc.stderr)
        lines = [l for l in proc.stdout.splitlines() if l.startswith("{")]
        if lines:
            facts.update(json.loads(lines[-1])["app_preexisting_provider_sync"])
        consumer_rc = proc.returncode if not lines else 0
    finally:
        provider.stdin.close()
        out = provider.stdout.read()
        provider.wait(timeout=60)
    served = [json.loads(l) for l in out.splitlines() if l.startswith("{")]
    facts["provider_served"] = served[-1]["served"] if served else None

    facts = {"user_id": user_id, **facts}
    write_facts({"app_preexisting_provider_sync": facts})
    if consumer_rc != 0:
        sys.exit(f"driver: consumer exit {consumer_rc}")
    print("driver: provider registered before adoption, discovered and called after")


asyncio.run(main())
