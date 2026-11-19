"""A local node's astrald process, found by the root it runs with.

why by root: a local world starts each astrald with `-root <root>`
(lib/localnode.py), and the session view carries that root, so the root
names the one process of a node. Only a local world runs its nodes on the
test host, so a test that signals a node is `local_only`.
"""
import os
import signal
from pathlib import Path


def find_pid(root: str) -> int:
    """The pid of the astrald process that runs with `-root root`."""
    want = b"-root\x00" + root.encode() + b"\x00"
    for cmdline in Path("/proc").glob("[0-9]*/cmdline"):
        try:
            if want in cmdline.read_bytes():
                return int(cmdline.parent.name)
        except OSError:
            continue
    raise LookupError(f"no astrald process runs with -root {root}")


def freeze(root: str) -> int:
    """Stop the node's process with SIGSTOP; its sockets stay open. Returns the pid."""
    pid = find_pid(root)
    os.kill(pid, signal.SIGSTOP)
    return pid


def thaw(pid: int) -> None:
    """Resume a process freeze() stopped."""
    os.kill(pid, signal.SIGCONT)
