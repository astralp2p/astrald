"""Host-side helpers shared by the vmop verifiers.

A vmop verifier checks a property of the simulated world — a service is up, an
address is gone, a candidate is advertised — from outside the guests. All it
needs is ssh and, for anything astral, the guest's own `astral-query`: the
node under test answers about itself over its own apphost, which is both the
shortest path and the one that stays true when a node is NAT'd into a netns.

why no astral client here: an oracle judges astrald through astral-py because
that is the app's view. A vmop verifier judges the WORLD, and reaching for a
client would pin these three scripts to a client version for no gain — the
predecessor did exactly that against a vendored astral-py and stopped
importing the day the real checkout moved past it.
"""
import json
import subprocess
import sys


def ssh(vm, remote):
    """`netsim ssh <vm> -- <remote>`; stdout, best-effort.

    The simulation comes from NETSIM_SIM_DIR, which the netsim executor sets.
    """
    p = subprocess.run(["netsim", "ssh", vm, "--", remote],
                       capture_output=True, text=True)
    return p.stdout


def read_json(vm, path):
    """<path> on the VM parsed as a dict ({} on error)."""
    try:
        return json.loads(ssh(vm, f"cat {path}") or "{}") or {}
    except json.JSONDecodeError:
        return {}


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


def query(vm, op, netns=""):
    """Ask the guest's own astral-query, raw stdout.

    netns: run inside a network namespace ("priv" once a node is NAT'd, where
    apphost listens on a loopback the host cannot reach).
    """
    prefix = f"ip netns exec {netns} " if netns else ""
    return ssh(vm, f"{prefix}astral-query {op} -out json") or ""


def live_onion(vm):
    """The .onion astrald advertises for itself, or "".

    Reads whatever `nodes.resolve_endpoints -id localnode` prints and picks
    the onion out of it. Deliberately shape-agnostic: the verifier's question
    is "does an onion appear", and pinning the envelope's field names would
    make this fail on a wire change that broke nothing.
    """
    for token in query(vm, "nodes.resolve_endpoints -id localnode").split('"'):
        if ".onion" in token:
            return token.strip()
    return ""


def report_errors(errors, task):
    """Write a `<task> verify FAILED:` bullet report; 1 with errors, else 0."""
    if not errors:
        return 0
    sys.stderr.write(f"{task} verify FAILED:\n")
    for e in errors:
        sys.stderr.write(f"  - {e}\n")
    return 1
