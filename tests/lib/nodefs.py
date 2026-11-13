"""A node's files, reached through its session-view `root`.

A driver or oracle that builds a host path from a node's root works in a
local world only. These functions take the node's entry in the session view
and a path relative to its root, and act on the machine the node runs on:

    host path                   a local world: the root is a directory here
    docker:<container>:<path>   a Docker world: through `docker exec`

Any other root (a VM world's `netsim:…`, an attached daemon's None) raises
NodeFSError: that world reaches no node file, and a test that needs one is
planned elsewhere.

why `docker exec` and never a mount: a Docker world mounts nothing of the host
into a container (lib/worlds/docker.py), so a node's files stay in it.
why exec and not `docker cp`: cp needs a host temp file per call and copies a
whole tar stream, where exec streams one file's bytes through cat.
"""
import os
import shutil
import subprocess
from pathlib import Path, PurePosixPath

LOG = "astrald.log"     # astrald's log, under its root in every world


class NodeFSError(Exception):
    """A root nodefs cannot reach."""


def _where(node: dict) -> tuple:
    """(container, root): container None for a host path."""
    root = node.get("root")
    if isinstance(root, str) and root.startswith("docker:"):
        _, container, path = root.split(":", 2)
        if container and path.startswith("/"):
            return container, path
    elif isinstance(root, str) and os.path.isabs(root):
        return None, root
    raise NodeFSError(f"root {root!r} is neither a host path nor "
                      "docker:<container>:<path>, so no node file is "
                      "reachable in this world")


def path(node: dict, rel: str = "") -> str:
    """The absolute path of rel as the node's machine sees it: what an op
    such as fs.new_watch is handed.

    why resolved on the host: a local root lies under the session tree's
    symlink to the temp dir, and /var is itself a symlink on macOS; a node
    stores resolved paths, so an unresolved one would never match.
    """
    container, root = _where(node)
    if container is None:
        return str(Path(root).resolve() / rel)
    return str(PurePosixPath(root) / rel)


def _exec(node: dict, *argv: str, data: bytes | None = None) -> bytes:
    """Run argv as root in the node's container; its stdout.

    why no stdin unless data: docker exec -i would hand the caller's terminal
    to the container (lib/worlds/docker.py _docker)
    """
    container, _ = _where(node)
    cmd = ["docker", "exec", *(["-i"] if data is not None else []),
           container, *argv]
    p = subprocess.run(cmd, input=data, capture_output=True,
                       stdin=None if data is not None else subprocess.DEVNULL)
    if p.returncode != 0:
        raise OSError(f"{' '.join(cmd)}: exit {p.returncode}: "
                      f"{p.stderr.decode(errors='replace').strip()}")
    return p.stdout


def _docker(node: dict) -> bool:
    return _where(node)[0] is not None


def read_bytes(node: dict, rel: str) -> bytes:
    if _docker(node):
        return _exec(node, "cat", "--", path(node, rel))
    return Path(path(node, rel)).read_bytes()


def read_text(node: dict, rel: str) -> str:
    """rel's text; a byte that is not UTF-8 reads as U+FFFD."""
    return read_bytes(node, rel).decode(errors="replace")


def log(node: dict) -> str:
    """The node's astrald.log as text."""
    return read_text(node, LOG)


def write_bytes(node: dict, rel: str, data: bytes) -> None:
    """Write rel, creating its parent directories."""
    p = path(node, rel)
    if _docker(node):
        _exec(node, "sh", "-c", 'mkdir -p -- "$(dirname -- "$1")" && '
              'cat > "$1"', "sh", p, data=data)
        return
    Path(p).parent.mkdir(parents=True, exist_ok=True)
    Path(p).write_bytes(data)


def mkdir(node: dict, rel: str) -> None:
    """Create the directory rel and its parents; one already there stays."""
    if _docker(node):
        _exec(node, "mkdir", "-p", "--", path(node, rel))
        return
    Path(path(node, rel)).mkdir(parents=True, exist_ok=True)


def remove(node: dict, rel: str) -> None:
    """Remove the file, symlink or directory tree rel; a symlink is removed,
    never followed."""
    if _docker(node):
        _exec(node, "rm", "-rf", "--", path(node, rel))
        return
    p = Path(path(node, rel))
    if p.is_dir() and not p.is_symlink():
        shutil.rmtree(p)
    else:
        p.unlink()


def symlink(node: dict, rel: str, target: str) -> None:
    """Make rel a symlink to target, which is relative to the root too; the
    link holds target's absolute path on the node's machine."""
    if _docker(node):
        _exec(node, "ln", "-s", "--", path(node, target), path(node, rel))
        return
    os.symlink(path(node, target), path(node, rel))


def rename(node: dict, src: str, dst: str) -> None:
    """Move src to dst, replacing a file at dst."""
    if _docker(node):
        _exec(node, "mv", "-f", "--", path(node, src), path(node, dst))
        return
    os.replace(path(node, src), path(node, dst))


def listdir(node: dict, rel: str = "") -> dict:
    """{name: kind} of the entries in directory rel, kind "file", "dir",
    "link" or "other". A symlink is "link", whatever it points at."""
    if _docker(node):
        out = _exec(node, "find", path(node, rel), "-mindepth", "1",
                    "-maxdepth", "1", "-printf", "%y %f\\0")
        kinds = {"f": "file", "d": "dir", "l": "link"}
        return {name: kinds.get(y, "other") for y, _, name in
                (e.decode().partition(" ") for e in out.split(b"\0") if e)}
    out = {}
    for e in os.scandir(path(node, rel)):
        out[e.name] = ("link" if e.is_symlink() else
                       "dir" if e.is_dir() else
                       "file" if e.is_file() else "other")
    return out


def set_mtime(node: dict, rel: str, mtime: float) -> None:
    """Set rel's modification time (seconds since the epoch); its access
    time takes the same value."""
    if _docker(node):
        _exec(node, "touch", "-m", "-a", "-d", f"@{mtime}", "--",
              path(node, rel))
        return
    os.utime(path(node, rel), (mtime, mtime))
