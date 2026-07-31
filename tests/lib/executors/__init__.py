"""One interface between the runner and the world a test runs in.

An executor materializes a test's start state for its roster, runs the
test's steps, exposes one session.json, honors saves, and tears down. What
it does not do is leak: drivers and oracles read session.json and never
learn whether the endpoints behind it are processes two ports away or VMs
behind a tunnel.
"""
from pathlib import Path


class ExecutorError(Exception):
    """The world broke, not the test — the runner reports it as environment."""


class Executor:
    env = None

    @property
    def session_json_path(self) -> Path:
        """The one file drivers and oracles are allowed to see."""
        raise NotImplementedError

    def prepare(self, test) -> None:
        """Bring the roster up at the test's start state and run its steps."""
        raise NotImplementedError

    def merge_facts(self, path: Path) -> None:
        """Fold a driver's facts into the session the next test inherits."""
        raise NotImplementedError

    def save(self, state: str) -> None:
        """Make the current world available under a state name."""
        raise NotImplementedError

    def broken(self) -> list:
        """Machines that died under a test — any verdict over them is void."""
        raise NotImplementedError

    def teardown(self, keep: bool) -> None:
        raise NotImplementedError
