"""One interface between the runner and the world a test runs in.

An executor materializes a test's start state for its roster, runs the
test's steps, reports its nodes, honors saves, and tears down. It owns
machines only: the runner owns facts and verdicts, and writes the session
view each driver and oracle reads. What an executor does not do is leak:
the view never says whether the endpoints behind it are processes two
ports away or VMs behind a tunnel.

Every executor reports a node under one schema, `NODE_FIELDS`. A field an
executor cannot provide is None.
"""
from pathlib import Path

NODE_FIELDS = ("endpoint", "token", "identity", "root", "tcp_port",
               "lan_endpoint", "mcp_url", "serves_mcp", "authority_url",
               "restarts")


def node_entry(**known) -> dict:
    """One node under the shared schema, None for every field not known."""
    stray = set(known) - set(NODE_FIELDS)
    if stray:
        raise ValueError(f"not a node field: {', '.join(sorted(stray))}")
    return {f: known.get(f) for f in NODE_FIELDS}


class ExecutorError(Exception):
    """The world broke, not the test — the runner reports it as environment."""


class Executor:
    env = None
    # the machines the binaries under test were pushed to; empty where the
    # run spawns its own processes or borrows a daemon
    pushed = ()

    def prepare(self, test) -> None:
        """Bring the roster up at the test's start state and run its steps."""
        raise NotImplementedError

    def nodes(self) -> dict:
        """name -> node entry (`node_entry`) for every node up so far."""
        raise NotImplementedError

    def save(self, state: str) -> None:
        """Make the current world available under a state name."""
        raise NotImplementedError

    def restart(self, names: list) -> None:
        """Stop the named nodes and boot them again on the same state.

        An executor that cannot restart a node refuses rather than skipping:
        an oracle over a node that never restarted judges nothing.
        """
        raise ExecutorError(
            f"{type(self).__name__} cannot restart a node: the manifest key "
            "restart needs env node's fresh world")

    def broken(self) -> list:
        """Machines that died under a test — any verdict over them is void."""
        raise NotImplementedError

    def run_agent(self, test, log: Path, facts: Path, timeout: int) -> int:
        """Drive the test's flow with an AI operator instead of its script.

        The oracle is untouched: whatever performed the flow, verify.py judges
        it, reading the same session view and the same facts — so the driver
        owes the runner a facts file however it produced one. An executor with
        no operator in its world refuses rather than pretending: a driver that
        cannot run is not a test that failed.
        """
        raise ExecutorError(
            f"{type(self).__name__} has no AI operator: --driver agent needs "
            "a world that carries one (env netsim bakes it into the lab)")

    def teardown(self, keep: bool) -> None:
        raise NotImplementedError
