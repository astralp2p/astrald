"""SwarmState requests and answers, driven over apphost with JSON.

The tests of swarm-state.suite read a node's requests with
`user.swarm_requests`, answer them with `user.answer_request` and read the
entries a node stores with `nodes.swarm_digest` and `nodes.swarm_fetch`.
astral-py has no client for these ops, so the first two go through
`lib/jsonops` (raw JSON envelopes) and the exchange ops, and every app, through
the Go tool `tests/e2e/internal/swarmapps`.

    reqs = await swarmstate.requests(c, all=True)   # [Request, ...]
    await swarmstate.answer(cu, req.id, "approve")  # the signed Approval
    ids = swarmstate.entries(node)                  # {entry ID: envelope}

A Request carries `id` (the entry ID), `status` (pending, approved, declined,
expired), `type` (the signed request's type) and `obj` (the request's own
fields, without the signature: the JSON of a signed request nests them under
the request's type name, e.g. `JoinRequest`).
"""
import asyncio
import contextlib
import json
import os
import time
import uuid
from pathlib import Path
from dataclasses import dataclass

import astral

from lib import jsonops

JOIN = "mod.user.signed_join_request"
CONTRACT = "mod.user.signed_contract_request"
EXPEL = "mod.user.signed_expel_request"


@dataclass
class Request:
    id: str
    status: str
    type: str
    obj: dict

    def names(self, identity: str) -> bool:
        """Whether the request names identity: a JoinRequest's joiner, a
        ContractRequest's identity or an ExpelRequest's subject."""
        return identity in (self.obj.get("Joiner"), self.obj.get("Identity"),
                            self.obj.get("Subject"))


async def requests(c, all: bool = False, **kw) -> list:
    """The requests a node's SwarmState holds, oldest filed first; with `all`,
    every status, else the pending ones only."""
    qs = "user.swarm_requests?out=json" + ("&all=true" if all else "")
    docs = await jsonops.stream(c, qs, **kw)
    # why triples: the op sends a request's entry ID, its status and the
    # signed request, in that order (astral-docs user.swarm_requests.md).
    return [Request(docs[i]["Object"], docs[i + 1]["Object"], docs[i + 2]["Type"],
                    unsigned(docs[i + 2]["Object"]))
            for i in range(0, len(docs) - 2, 3)]


def unsigned(signed: dict) -> dict:
    """The fields of a signed request: the one nested *Request object."""
    for k, v in signed.items():
        if k.endswith("Request") and isinstance(v, dict):
            return v
    return signed


async def find(c, pred, timeout: float = 30.0, all: bool = True, poll: float = 0.5,
               **kw) -> Request:
    """The first request for which pred holds, waited for up to timeout."""
    deadline = time.monotonic() + timeout
    while True:
        for r in await requests(c, all=all, **kw):
            if pred(r):
                return r
        if time.monotonic() >= deadline:
            raise AssertionError(f"no matching request within {timeout:.0f} s")
        await asyncio.sleep(poll)


async def answer(c, req_id: str, answer: str, **kw) -> dict:
    """user.answer_request: the signed Approval or Decline, as a JSON
    envelope. An error_message raises jsonops.OpError."""
    docs = await jsonops.call(c, f"user.answer_request?id={req_id}&answer={answer}&out=json",
                              **kw)
    if not docs:
        raise jsonops.OpError("user.answer_request answered nothing")
    if docs[0]["Type"] == "error_message":
        raise jsonops.OpError(docs[0]["Object"])
    return docs[0]


async def confirm_join(c, req_id: str, **kw) -> None:
    """user.confirm_join as the User: the user key signs the Privileged
    membership the JoinRequest req_id asks for. An error_message raises
    jsonops.OpError."""
    await jsonops.value(c, f"user.confirm_join?id={req_id}&out=json", **kw)


async def status_of(c, req_id: str, **kw) -> str | None:
    """A request's status on a node, or None when the node does not hold it."""
    for r in await requests(c, all=True, **kw):
        if r.id == req_id:
            return r.status
    return None


async def join(c, carrier: str, tier: str = "member", timeout: float = 60.0) -> list:
    """user.join on the node c reaches, through carrier at tier: every envelope
    it answered (the membership first, on success)."""
    return await jsonops.call(c, f"user.join?node={carrier}&tier={tier}&out=json",
                              timeout=timeout)


async def admit_member(joiner_c, user_c, carrier: str, joiner: str,
                       tier: str = "member", timeout: float = 60.0) -> tuple:
    """Run joiner's user.join through carrier while the user approves its
    JoinRequest on the carrier, or, at privileged_member, confirms it with
    user.confirm_join (the carrier must hold the user key). Returns (the
    join's envelopes, the request)."""
    async def approve():
        # note: the harness answerer (lib/answerer.py) may approve it first
        r = await find(user_c, lambda r: r.type == JOIN and r.names(joiner)
                       and r.status in ("pending", "approved"), timeout=timeout)
        if tier == "privileged_member":
            # why: a Privileged membership is the user key's to sign, after the
            # user's own action (user.confirm_join, D2); no Approval stands in
            await confirm_join(user_c, r.id)
            return r
        if r.status == "approved":
            return r
        try:
            await answer(user_c, r.id, "approve")
        except jsonops.OpError:
            if await status_of(user_c, r.id) != "approved":
                raise
        return r

    with waiting():
        return await asyncio.gather(join(joiner_c, carrier, tier, timeout), approve())


# The app side: tests/e2e/internal/swarmapps, a Go tool built once per world.
TOOL = "tests/e2e/internal/swarmapps"


def tool() -> str:
    """The swarmapps binary in the world's app directory, built when absent."""
    from lib import apps
    path = apps.apps_dir() / "bin" / "swarmapps"
    if not path.exists():
        path = apps.build_go(TOOL, "swarmapps")
    return str(path)


def app_tool(*args, timeout: float = 90.0, env: dict | None = None,
             stdin: str | None = None) -> dict:
    """Run one swarmapps subcommand and return its JSON line. The tool reports
    a failure as {"error": ...} and judges nothing."""
    import json
    import os
    import subprocess
    p = subprocess.run([tool(), *[str(a) for a in args]], capture_output=True,
                       text=True, timeout=timeout, env={**os.environ, **(env or {})},
                       input=stdin)
    lines = [l for l in p.stdout.splitlines() if l.startswith("{")]
    if p.returncode != 0 or not lines:
        raise AssertionError(f"swarmapps {args[0]}: exit {p.returncode}\n{p.stderr}")
    return json.loads(lines[-1])


def register(node: dict, discovery: bool = False) -> dict:
    """A fresh app on node, registered with the node's token: {identity,
    token}. The node files one ContractRequest for the hosting permits and,
    with discovery, a ServiceDiscovery permit for `wallet`."""
    out = app_tool("register", node["endpoint"], node["token"],
                   *(["discovery"] if discovery else []))
    assert "error" not in out, f"apphost.register on {node['identity']}: {out['error']}"
    return out


def await_contract(node: dict, token: str, discovery: bool = False,
                   wait: int = 60) -> dict:
    """The outcome of apphost.request_contract for the app token names, with
    the permits its registration asked: {request_id, contract} once its host
    signed the contract, or {request_id, error}."""
    return app_tool("request", node["endpoint"], token, wait,
                    *(["discovery"] if discovery else []), timeout=wait + 30)


def spawn_wallet(name: str, node: dict, token: str):
    """Start the wallet stand-in as the app token names on node, as the
    long-lived app name (lib/apps), once its host signed its contract. It
    advertises `wallet` as private and refuses a spend over 100 itself."""
    from lib import apps
    # why: the wallet binds and advertises under ServeApps, which it holds
    # only once its host carried the Approval out.
    out = await_contract(node, token)
    assert "contract" in out, f"the wallet got no contract: {out}"
    app = apps.spawn(name, [tool(), "wallet", node["endpoint"], "-", node["host_ip"]],
                     env={"SWARMAPPS_TOKEN": token})
    app.wait_state(lambda s: s.get("serving"), timeout=30)
    return app


def serve_probe(node: dict, token: str) -> dict:
    """The app token names tries apphost.register_handler and a private
    services.advertise on node, the two acts ServeApps guards:
    {register_handler, advertise}, each "ok" or the error the node answered."""
    out = app_tool("serve-probe", node["endpoint"], token)
    assert "error" not in out, f"serve-probe on {node['identity']}: {out['error']}"
    return out


def discover(node: dict, token: str, reach: str) -> list:
    """The providers of `wallet` one discovery shows the app token names."""
    out = app_tool("discover", node["endpoint"], token, reach)
    assert "error" not in out, f"services.discover: {out['error']}"
    return out["providers"]


async def await_discovery(node: dict, token: str, reach: str, provider: str,
                          timeout: float = 60.0) -> float | None:
    """Seconds until a discovery by the app token names shows provider, or
    None when it does not within timeout."""
    start = time.monotonic()
    while time.monotonic() - start < timeout:
        if provider in discover(node, token, reach):
            return time.monotonic() - start
        await asyncio.sleep(1.0)
    return None


def spend(node: dict, token: str, wallet: str, amount: int) -> dict:
    """wallet.spend as the app token names: {"answer"} or {"error"}."""
    return app_tool("call", node["endpoint"], token, wallet, amount)


def entries(node: dict) -> dict:
    """Every entry node's SwarmState stores, read with the node's own token:
    {entry ID: its JSON envelope {Type, Object}}.

    why the Go tool: nodes.swarm_digest and nodes.swarm_fetch speak only the
    binary encoding, which astral-py does not decode for these types."""
    out = app_tool("entries", node["endpoint"], node["token"])
    assert "error" not in out, f"SwarmState of {node['identity']}: {out['error']}"
    return {e["id"]: e["entry"] for e in out["entries"]}


def contracts_of(node: dict, subject: str) -> list:
    """The contracts naming subject that node's SwarmState stores, as the JSON
    of their Contract."""
    return [e["Object"]["Contract"] for e in entries(node).values()
            if e["Type"] == "mod.auth.signed_contract"
            and e["Object"]["Contract"].get("Subject") == subject]


def entry_on(node: dict, entry_id: str) -> dict | None:
    """The JSON object of the entry entry_id on node, or None when the node
    does not store it."""
    e = entries(node).get(entry_id)
    return e["Object"] if e else None


def sign_approval(node: dict, req_id: str, push_to: list) -> dict:
    """An Approval of req_id signed with node's own key (read from its
    data/node_key with lib/nodefs) and offered with nodes.swarm_push to node
    and to every node of push_to: {approval_id, pushed}."""
    from lib import nodefs
    out = app_tool("approve", node["endpoint"], node["token"], node["identity"], req_id,
                   *[n["identity"] for n in push_to],
                   env={"SWARMAPPS_NODE_KEY": nodefs.read_bytes(node, "data/node_key").hex()})
    assert "error" not in out, f"signing an Approval as {node['identity']}: {out['error']}"
    return out


def offer(node: dict, entry_id: str, push_to: list) -> dict:
    """Offer the entry entry_id that node stores to every node of push_to with
    nodes.swarm_push, as node's own token: {node identity: "ack" or the
    error the push answered}."""
    out = app_tool("offer", node["endpoint"], node["token"], entry_id,
                   *[n["identity"] for n in push_to])
    assert "error" not in out, f"offering {entry_id} from {node['identity']}: {out['error']}"
    return out["pushed"]


def user_membership(node: dict, user_token: str, user_id: str, subject: str,
                    tier: str = "privileged_member", validity: int = 3600,
                    push_to: tuple = ()) -> dict:
    """A membership of subject at tier that the User signs on node
    (crypto.sign_hash with user_token), with no Approval link, offered with
    nodes.swarm_push to node and to every node of push_to: {membership_id,
    pushed: {"local" or node identity: "ack" or the error}}. This is the only
    way the User brings a node in without its JoinRequest."""
    out = app_tool("membership", node["endpoint"], user_token, user_id, subject,
                   tier, validity, *[n["identity"] for n in push_to])
    assert "error" not in out, f"signing a membership for {subject}: {out['error']}"
    return out


def membership_id(node: dict, subject: str) -> str:
    """The entry ID of a membership naming subject that node stores; the
    first one when it stores several."""
    for eid, e in entries(node).items():
        if e["Type"] == "mod.auth.signed_contract" \
                and e["Object"]["Contract"].get("Subject") == subject:
            return eid
    raise AssertionError(f"{node['identity']} stores no contract of {subject}")


def move_key(src: dict, dst: dict, identity: str) -> dict:
    """Copy identity's private key from src's object store to dst: the app's
    move to dst, as a backup restored there. The key is found among src's
    stored objects (lib/nodefs, data/data) and stored on dst with dst's own
    token: {key_object}."""
    from lib import nodefs
    blobs = []
    for name, kind in nodefs.listdir(src, "data/data").items():
        if kind != "file":
            continue
        data = nodefs.read_bytes(src, f"data/data/{name}")
        if b"mod.crypto.private_key" in data[:64]:
            blobs.append(data.hex())
    out = app_tool("store-key", dst["endpoint"], dst["token"], identity,
                   stdin="\n".join(blobs) + "\n")
    assert "error" not in out, f"moving {identity}'s key: {out['error']}"
    return out


async def admit(carrier: dict, user_token: str, joiner: dict, tier: str = "member",
                timeout: float = 60.0) -> dict:
    """joiner joins the User's swarm through carrier at tier, the User
    approving its JoinRequest on carrier.
    Both nodes learn each other's LAN endpoint first. Returns the membership
    envelope the join answered; an answer that is not one raises."""
    async with await astral.connect(carrier["endpoint"], token=carrier["token"]) as c:
        await c.nodes.add_endpoint(joiner["identity"], joiner["lan_endpoint"], experimental=True)
    async with await astral.connect(joiner["endpoint"], token=joiner["token"]) as c:
        await c.nodes.add_endpoint(carrier["identity"], carrier["lan_endpoint"], experimental=True)
    async with await astral.connect(joiner["endpoint"], token=joiner["token"]) as jc, \
            await astral.connect(carrier["endpoint"], token=user_token) as uc:
        joined, _ = await admit_member(jc, uc, carrier["identity"], joiner["identity"],
                                       tier, timeout)
    if not joined or joined[0]["Type"] != "mod.auth.signed_contract":
        raise AssertionError(f"{joiner['identity']} did not join through "
                             f"{carrier['identity']}: {joined}")
    return joined[0]


@contextlib.contextmanager
def waiting():
    """Tell the harness answerer (lib/answerer.py) a driver waits for an
    answer, for as long as the block runs: a file in $ASTRAL_TESTS_WAITING."""
    d = os.environ.get("ASTRAL_TESTS_WAITING")
    if not d:
        yield
        return
    f = Path(d) / f"py-{os.getpid()}-{uuid.uuid4().hex}"
    f.touch()
    try:
        yield
    finally:
        f.unlink(missing_ok=True)


async def request_contract(c, actions: list, timeout: float = 60.0) -> dict:
    """apphost.request_contract as the app c's token names, for a permit of
    each action: the envelope of the contract its host signs once the
    request is approved (the harness answerer approves it while a test runs).
    An answer that is not a contract raises jsonops.OpError."""
    with waiting():
        return await _request_contract(c, actions, timeout)


async def _request_contract(c, actions: list, timeout: float) -> dict:
    async with c.stream("apphost.request_contract?in=json&out=json", raw=True,
                        timeout=timeout) as s:
        for a in actions:
            env = {"Type": "mod.auth.permit", "Object": {"Action": a}}
            await s.send_bytes((json.dumps(env) + "\n").encode())
        await s.send_bytes((json.dumps(jsonops.EOS) + "\n").encode())
        buf = b""
        while True:
            chunk = await s.read_bytes(4096, timeout=timeout)
            if not chunk:
                break
            buf += chunk
            *lines, rest = buf.split(b"\n")
            buf = rest
            for line in lines:
                if not line.strip():
                    continue
                doc = json.loads(line)
                if doc["Type"] == "mod.auth.signed_contract":
                    return doc
                if doc["Type"] == "error_message":
                    raise jsonops.OpError(doc["Object"])
    raise jsonops.OpError("apphost.request_contract answered no contract")


HOSTING = ("mod.auth.serve_apps_action", "mod.auth.use_blueprints_action")


async def await_app(endpoint: str, token: str, actions=(), timeout: float = 60.0) -> dict:
    """Wait, as the app token names on the node at endpoint, until its host
    signed the contract its registration asked: the hosting permits and a
    permit of each action (apphost.register asks them in that order). The
    harness answerer approves the request while a test runs."""
    async with await astral.connect(endpoint, token=token) as c:
        return await request_contract(c, [*HOSTING, *actions], timeout=timeout)
