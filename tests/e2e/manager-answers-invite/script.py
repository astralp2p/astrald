#!/usr/bin/env python3
"""Driver: two unclaimed nodes, each with a manager that answers the swarm
invite, are invited by the User's node; one manager approves, one declines.

The driver acts and judges nothing. It claims mai-inviter, links it to both
invitees, and runs driver/ (Go, this repository's astral-go): on each invitee
the node's token appoints a manager whose swarm_invite policy delegate is the
op mai.decide, the manager serves that op, and the User on mai-inviter adopts
the invitee. It records what each manager was asked and how each adoption
ended.

why a Go program: astral-py has no codec for the swarm invite request and
decision, and a manager answers them as an app.
why jsonops for the claim: astral-py has no binding for user.derive_identity
or user.bootstrap.
"""
import asyncio
import json
import os
import subprocess
import sys
from pathlib import Path

import astral

from lib import jsonops
from lib.sessionio import load, write_facts

ROOT = Path(__file__).resolve().parents[3]


async def claim(n: dict) -> tuple[str, str]:
    """Claim the node with a session that has no token; return the User's
    identity and token."""
    async with await astral.connect(n["endpoint"]) as c:
        preview = await jsonops.value(c, "user.derive_identity?generate=true&out=json")
        words = {"Type": "string16", "Object": preview["Mnemonic"]}
        claimed = await jsonops.value(c, "user.bootstrap?in=json&out=json", words)
    return claimed["Identity"], claimed["Token"]


async def link(inviter: dict, user_token: str, targets: list[dict]) -> None:
    """The inviter learns each target's endpoint and links to it.

    why the inviter alone: the unclaimed gate list holds no nodes.add_endpoint,
    and the invitation's answer returns over the link the inviter opened.
    """
    async with await astral.connect(inviter["endpoint"], token=inviter["token"]) as c:
        for t in targets:
            await c.nodes.add_endpoint(t["identity"], t["lan_endpoint"], experimental=True)
    async with await astral.connect(inviter["endpoint"], token=user_token) as c:
        for t in targets:
            await c.nodes.new_link(t["identity"], endpoint=t["lan_endpoint"],
                                   strategies="basic", experimental=True, timeout=60.0)


async def main():
    nodes = load()["nodes"]
    inviter = nodes["mai-inviter"]
    user_id, user_token = await claim(inviter)
    await link(inviter, user_token, [nodes["mai-approve"], nodes["mai-decline"]])

    proc = subprocess.run(
        ["go", "run", "./tests/e2e/manager-answers-invite/driver", "drive",
         os.environ["ASTRAL_TESTS_SESSION"]],
        cwd=ROOT, capture_output=True, text=True,
        env={**os.environ, "GOFLAGS": os.environ.get("GOFLAGS", "-buildvcs=false"),
             "MAI_USER_TOKEN": user_token},
    )
    sys.stderr.write(proc.stderr)
    lines = [l for l in proc.stdout.splitlines() if l.startswith("{")]
    cases = json.loads(lines[-1])["cases"] if lines else {}
    write_facts({"manager_answers_invite": {"user": user_id, "cases": cases}})
    if proc.returncode != 0:
        sys.exit(f"driver: exit {proc.returncode}")
    print(f"driver: user {user_id} invited both nodes; "
          f"approve: {cases['approve']['adopt_error'] or 'adopted'}; "
          f"decline: {cases['decline']['adopt_error'] or 'adopted'}")


asyncio.run(main())
