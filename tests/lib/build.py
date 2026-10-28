"""Build astrald from this worktree; the binary under test."""
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


def lacks_commit(checkout: Path, commit: str) -> bool:
    """Whether a checkout's HEAD lacks commit. A tree git cannot read lacks it."""
    try:
        subprocess.run(["git", "merge-base", "--is-ancestor", commit, "HEAD"],
                       cwd=checkout, check=True, capture_output=True)
    except (OSError, subprocess.CalledProcessError):
        return True
    return False


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
