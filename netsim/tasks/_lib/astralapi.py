"""Shared host-side verify library for the netsim astral scenarios.

Each task's verify.py reaches this through a realpath shim that crosses netsim's
per-task symlink:

    import os, sys
    sys.path.insert(0, os.path.join(
        os.path.dirname(os.path.dirname(os.path.realpath(__file__))), "_lib"))
    import astralapi

It centralises the two halves every verifier shares:

  * transport -- ssh()/file readers/all_running_vms()/peer_lan_ip(): unchanged
    subprocess plumbing for reading the agent's recorded artifacts and probing
    inside a VM.

  * queries -- connect(vm, token=...) yields a Node whose .call(op, ...) returns
    a list of astral.AstralObject. Queries go through the astral-py typed client
    (reached host-side over an ssh -L forward of the VM's WebSocket apphost
    port), falling back to the lockstep Go `astral-query` CLI -- same JSON,
    parsed with astral-py's from_json_envelope -- whenever the client can't
    serve an op (pinned in SHELL_OPS, or it raised). Both paths return the same
    list[AstralObject], so Node's typed accessors (user_info, links, ...) decode
    astral-py protocol records (LinkInfo, UserInfo, SwarmMember, SignedExpulsion,
    EndpointWithTTL) identically on either path.

astral-py is the submodule at _lib/astral-py (package under src/); imported without
pip. $ASTRALPY_SRC overrides the src dir for local dev against another checkout.
"""
import contextlib
import json
import os
import shlex
import socket
import subprocess
import sys
import time

# --- astral-py (submodule at _lib/astral-py; pip-free) -----------------------
# why: realpath resolves _lib through netsim's per-task symlink; the submodule's
# package lives under src/. $ASTRALPY_SRC overrides for local dev.
_ASTRALPY_SRC = os.environ.get("ASTRALPY_SRC") or os.path.join(
    os.path.dirname(os.path.realpath(__file__)), "astral-py", "src")
if not os.path.isdir(os.path.join(_ASTRALPY_SRC, "astral")):
    raise ImportError(
        f"astral-py not found at {_ASTRALPY_SRC} -- run "
        "`git submodule update --init netsim/tasks/_lib/astral-py` "
        "(or set $ASTRALPY_SRC to an astral-py checkout's src/)")
sys.path.insert(0, _ASTRALPY_SRC)
import astral  # noqa: E402
from astral.encoding import from_json_envelope  # noqa: E402
from astral.protocols.nodes import EndpointWithTTL, LinkInfo  # noqa: E402
from astral.protocols.user import (  # noqa: E402
    SignedExpulsion,
    SwarmMember,
    UserInfo,
)

# apphost WebSocket port inside each VM (binds 0.0.0.0; reachable via ssh -L).
WS_PORT = 8624

# Ops to keep on the Go astral-query CLI instead of the astral-py client.
# Populated by the smoke-test triage when the client disagrees with the CLI on a
# specific op (a silent mismatch the auto-fallback can't catch). Empty => every
# op tries the client first.
SHELL_OPS = set()


# --- transport: subprocess into the VM ---------------------------------------
def ssh(vm, remote):
    """Run `netsim ssh <vm> -- <remote>` on the host; return stdout (best-effort)."""
    p = subprocess.run(["netsim", "ssh", vm, "--", remote],
                       capture_output=True, text=True)
    return p.stdout


def read_file(vm, path):
    """Contents of <path> on the VM, trailing newline stripped ("" on error)."""
    return (ssh(vm, f"cat {path}") or "").rstrip("\n")


def read_json(vm, path):
    """<path> on the VM parsed as a dict ({} on error)."""
    try:
        return json.loads(ssh(vm, f"cat {path}") or "{}") or {}
    except json.JSONDecodeError:
        return {}


def home_json(vm, name):
    """An agent artifact under /home/tester/<name>, parsed as a dict."""
    return read_json(vm, f"/home/tester/{name}")


def all_running_vms():
    """Hostnames of the running VMs in the current simulation."""
    out = subprocess.run(["netsim", "vm", "ls", "--json"],
                         capture_output=True, text=True).stdout
    try:
        return [v["hostname"] for v in json.loads(out or "[]")
                if v.get("state") == "running"]
    except json.JSONDecodeError:
        return []


def peer_lan_ip(peer):
    """The 10.77.* LAN address of <peer> ("" if none)."""
    for tok in (ssh(peer, "hostname -I") or "").split():
        if tok.startswith("10.77."):
            return tok
    return ""


# --- queries: astral-py client over an ssh -L forward, Go-CLI fallback -------
def parse_cli(raw):
    """Parse `astral-query -out json` output into AstralObjects (eos dropped)."""
    out = []
    for ln in (raw or "").splitlines():
        ln = ln.strip()
        if not ln:
            continue
        try:
            obj = from_json_envelope(json.loads(ln))
        except Exception:
            continue
        if not obj.is_eos:
            out.append(obj)
    return out


def _free_port():
    s = socket.socket()
    s.bind(("127.0.0.1", 0))
    port = s.getsockname()[1]
    s.close()
    return port


def _wait_port(port, timeout=10.0):
    deadline = time.time() + timeout
    while time.time() < deadline:
        try:
            with socket.create_connection(("127.0.0.1", port), timeout=0.5):
                return True
        except OSError:
            time.sleep(0.1)
    return False


def records(objs, record):
    """Decode the dict-valued objects into astral-py ``record`` instances.

    error_message and scalar entries are skipped -- verification asserts on the
    extracted content and reports errors via error_messages().
    """
    return [record.from_value(o.value) for o in objs if isinstance(o.value, dict)]


class Node:
    """A handle to one VM's apphost: .call(op, ...) -> list[AstralObject]."""

    def __init__(self, vm, client, token):
        self.vm = vm
        self._client = client
        self._token = token

    @property
    def uses_client(self):
        return self._client is not None

    def _via_shell(self, op, args, target):
        q = f"{target}:{op}" if target else op
        flags = "".join(f" -{k} {shlex.quote(str(v))}" for k, v in (args or {}).items())
        tok = f"export ASTRALD_APPHOST_TOKEN={self._token}; " if self._token else ""
        return parse_cli(ssh(self.vm, f"{tok}astral-query {q}{flags} -out json"))

    def call(self, op, args=None, target=None):
        """Run an apphost op; return its result objects (eos dropped, errors kept).

        Routes through the astral-py client unless the op is pinned in SHELL_OPS
        or no client is available; on any client error, falls back to the Go CLI.
        """
        if self._client is None or op in SHELL_OPS:
            return self._via_shell(op, args, target)
        try:
            with self._client.query(op, args or None, target=target) as st:
                return list(st)
        except Exception:
            # why: anonymous WS sessions and any client error fall back to the
            # lockstep astral-query so verification still runs.
            return self._via_shell(op, args, target)

    # --- typed accessors: astral-py records over either query path ----------
    def user_info(self):
        """The node's UserInfo, or None (rejected / no contract)."""
        infos = records(self.call("user.info"), UserInfo)
        return infos[0] if infos else None

    def swarm_members(self):
        """The swarm roster as SwarmMember records (user.swarm_status)."""
        return records(self.call("user.swarm_status"), SwarmMember)

    def links(self):
        """The active links as LinkInfo records (nodes.links)."""
        return records(self.call("nodes.links"), LinkInfo)

    def endpoints(self, identity):
        """The known endpoints of <identity> as EndpointWithTTL records."""
        return records(
            self.call("nodes.resolve_endpoints", {"id": identity}), EndpointWithTTL
        )

    def expulsions(self):
        """The swarm's bans as SignedExpulsion records (user.list_expelled)."""
        return records(self.call("user.list_expelled"), SignedExpulsion)


@contextlib.contextmanager
def connect(vm, token=None):
    """Yield a Node for <vm>.

    Opens an ssh -L forward of the VM's WebSocket apphost port (using netsim's
    own $NETSIM_SSH_CONFIG) and an astral-py client over it. If the forward or
    client can't be established, yields a shell-only Node so verification still
    runs via the Go CLI.
    """
    cfg = os.environ.get("NETSIM_SSH_CONFIG")
    client = None
    tunnel = None
    if cfg:
        try:
            port = _free_port()
            tunnel = subprocess.Popen(
                ["ssh", "-F", cfg, "-o", "ExitOnForwardFailure=yes",
                 "-L", f"{port}:127.0.0.1:{WS_PORT}", "-N", vm],
                stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
            if _wait_port(port):
                c = astral.connect(f"ws://127.0.0.1:{port}/.ws", token=token)
                # why: some astrald builds reject anonymous WS route_query (ProtocolError);
                # probe once so the session degrades to the Go CLI wholesale, not per call.
                try:
                    c.whoami()
                    client = c
                except Exception:
                    try:
                        c.close()
                    except Exception:
                        pass
                    client = None
        except Exception:
            client = None
    try:
        yield Node(vm, client, token)
    finally:
        try:
            if client is not None:
                client.close()
        except Exception:
            pass
        if tunnel is not None:
            tunnel.terminate()
            try:
                tunnel.wait(timeout=3)
            except Exception:
                tunnel.kill()


# --- stream utilities: list[AstralObject] -> extracted value -----------------
def loaded_payload(objs):
    """The decoded string payload from an objects.load result, or None."""
    for o in objs:
        if o.type in ("eos", "error_message"):
            continue
        if isinstance(o.value, str):
            return o.value
    return None


def error_messages(objs):
    """The error_message strings in a result stream."""
    return [o.value for o in objs if o.type == "error_message"]
