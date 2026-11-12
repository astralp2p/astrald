"""Host ports a test may hand to a node, and a node's extra TCP listeners.

why ports outside the run's span: a run leases its span (config.toml
[ports]) for its nodes, and a port the kernel picks for a bind to port 0
lies in its ephemeral range, never in that span.

why JSON ops: astral-py has no tcp module client. lib/jsonops reads the
node's own JSON, and `out=json` in the query string asks for it.
"""
import socket

from lib import jsonops


def free_ports(n: int) -> list:
    """n distinct ports nothing on this host listens on right now.

    The sockets stay bound until all n are picked, so no port repeats. A port
    is free only until someone binds it: use it at once.
    """
    held = []
    try:
        for _ in range(n):
            s = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
            s.bind(("", 0))
            held.append(s)
        return [s.getsockname()[1] for s in held]
    finally:
        for s in held:
            s.close()


async def new_ephemeral_listener(c, port: int):
    """Make the node behind c accept links on TCP `port`, on every address.

    The listener outlives the query: an op runs under a detached context
    (astral-go lib/routing). jsonops.OpError carries a refusal.
    """
    return await jsonops.value(
        c, f"tcp.new_ephemeral_listener?port={port}&out=json")


async def close_ephemeral_listener(c, port: int):
    """Stop the listener new_ephemeral_listener started on `port`."""
    return await jsonops.value(
        c, f"tcp.close_ephemeral_listener?port={port}&out=json")
