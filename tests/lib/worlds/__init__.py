"""Worlds: the machines one planned world of a run stands on.

A run is a list of worlds (lib/plan.py), and each world is opened fresh,
runs its steps, and closes. A world owns machines only: the runner owns the
facts, the verdicts and the session view, and the view never says whether
the endpoints behind it are processes two ports away, VMs behind a tunnel,
or a daemon the developer already runs.

There are four kinds: local (lib/worlds/local.py), docker
(lib/worlds/docker.py), vm (lib/worlds/vm.py) and attach
(lib/worlds/attach.py). Every world reports a node under one
schema, `NODE_FIELDS`; a field a world cannot provide is None.
"""

NODE_FIELDS = ("endpoint", "token", "identity", "root", "tcp_port",
               "lan_endpoint", "mcp_url", "serves_mcp", "authority_url",
               "restarts")


def node_entry(**known) -> dict:
    """One node under the shared schema, None for every field not known."""
    stray = set(known) - set(NODE_FIELDS)
    if stray:
        raise ValueError(f"not a node field: {', '.join(sorted(stray))}")
    return {f: known.get(f) for f in NODE_FIELDS}


class WorldError(Exception):
    """The world broke, not the test: the runner reports it as
    environment."""


class World:
    """The interface the runner drives every world through."""

    # the machines the binaries under test were pushed to; empty where the
    # run spawns its own processes or borrows a daemon
    pushed = ()

    def ensure(self, test) -> None:
        """Bring the test's roster up, and run its VM operations."""
        raise NotImplementedError

    def nodes(self) -> dict:
        """name -> node entry (`node_entry`) for every node up so far."""
        raise NotImplementedError

    def ssh(self, vm: str, script: str, check=True, timeout=None) -> str:
        """Run a root shell script on one node's machine; its output.

        Only a world whose nodes stand on machines of their own (VM, Docker)
        offers one, and the agent driver's operator works through it.
        """
        raise WorldError(f"{type(self).__name__} has no machine to run a "
                         f"shell on: the agent driver needs a VM or Docker "
                         "world")

    def containers(self) -> list:
        """The containers the world started, named in --keep's message."""
        return []

    def restart(self, names: list) -> None:
        """Stop the named nodes and boot them again on the same state.

        A world that cannot restart a node refuses rather than skipping: an
        oracle over a node that never restarted judges nothing.
        """
        raise WorldError(f"{type(self).__name__} cannot restart a node: the "
                         "manifest key restart needs a local or Docker "
                         "world")

    def dead(self) -> list:
        """Machines that died under a test: any verdict over them is
        void."""
        raise NotImplementedError

    def close(self, keep: bool) -> None:
        """End the world, or, kept, leave it up for whoever debugs it next."""
        raise NotImplementedError


def wait_identity(port: int, token: str, timeout: float,
                  interval: float) -> str:
    """The identity the apphost on host port `port` answers with, waited for
    up to `timeout` seconds; WorldError when it never answers."""
    import asyncio
    import time

    import astral

    async def identity_of():
        deadline = time.monotonic() + timeout
        last = None
        while time.monotonic() < deadline:
            try:
                async with await astral.connect(
                        f"tcp:127.0.0.1:{port}", token=token,
                        connect_timeout=3.0) as c:
                    return str(await c.apphost.whoami())
            except Exception as e:               # noqa: BLE001
                last = e
                await asyncio.sleep(interval)
        raise WorldError(f"apphost on 127.0.0.1:{port} never answered "
                         f"(last error: {last})")
    return asyncio.run(identity_of())
