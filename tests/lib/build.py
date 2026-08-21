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


def ensure_binary(repo_root: Path) -> tuple:
    cache = repo_root / "tests" / ".cache"
    cache.mkdir(parents=True, exist_ok=True)
    binary = cache / "astrald"
    subprocess.run(["go", "build", "-o", str(binary), "./cmd/astrald"],
                   cwd=repo_root, check=True)
    return binary, git_ref(repo_root)
