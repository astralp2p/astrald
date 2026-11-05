"""Build astrald from this worktree; the binary under test."""
import os
import subprocess
from pathlib import Path


def git_ref(checkout: Path) -> str:
    """Short HEAD of a checkout, or "unknown" when it is not a git tree."""
    try:
        p = subprocess.run(["git", "rev-parse", "--short", "HEAD"],
                           cwd=checkout, check=True, capture_output=True,
                           text=True)
    except (OSError, subprocess.CalledProcessError):
        return "unknown"
    return p.stdout.strip()


def missing_commit(checkout: Path, commit: str) -> str:
    """Why a checkout's HEAD does not hold commit, or "" when it does."""
    try:
        p = subprocess.run(["git", "merge-base", "--is-ancestor", commit, "HEAD"],
                           cwd=checkout, capture_output=True, text=True)
    except OSError as e:
        return f"git cannot run: {e}"
    if p.returncode == 0:
        return ""
    if p.returncode == 1:
        return f"{git_ref(checkout)} lacks {commit[:8]}"
    return (f"git cannot tell whether it holds {commit[:8]} (a shallow or "
            f"unfetched checkout?): {p.stderr.strip()}")


def query_binary(binary: Path) -> Path:
    """The astral-query ensure_binary builds beside an astrald."""
    return Path(binary).with_name("astral-query")


def ensure_binary(repo_root: Path) -> tuple:
    """Build astrald, and astral-query beside it, from one checkout.

    why astral-query too: the VM ops probe readiness with the guest's own
    astral-query, and a lab-baked one drifts from the astrald under test —
    a 2026-08-12 build sent `dir.resolve?name=localnode`, which a current
    astrald rejects, so enter-nat never saw the daemon come back.
    """
    cache = repo_root / "tests" / ".cache"
    cache.mkdir(parents=True, exist_ok=True)
    binary = cache / "astrald"
    for out, pkg in ((binary, "./cmd/astrald"),
                     (query_binary(binary), "./cmd/astral-query")):
        subprocess.run(["go", "build", "-o", str(out), pkg],
                       cwd=repo_root, check=True)
    return binary, git_ref(repo_root)


def linux_binaries(repo_root: Path, arch: str) -> Path:
    """Build astrald, and astral-query beside it, for Linux on `arch`
    (`amd64`, `arm64`): the binaries a Docker world copies into its
    containers.

    why static: a container runs the image's libc, not the host's, and the
    daemon needs none (CGO_ENABLED=0 builds it on modernc.org/sqlite).
    why its own directory: the host build beside it stays the one a local or
    VM world runs.
    """
    out = repo_root / "tests" / ".cache" / f"linux-{arch}"
    out.mkdir(parents=True, exist_ok=True)
    binary = out / "astrald"
    env = dict(os.environ, CGO_ENABLED="0", GOOS="linux", GOARCH=arch)
    for dst, pkg in ((binary, "./cmd/astrald"),
                     (query_binary(binary), "./cmd/astral-query")):
        subprocess.run(["go", "build", "-o", str(dst), pkg],
                       cwd=repo_root, env=env, check=True)
    return binary
