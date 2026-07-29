"""Build astrald from this worktree; the binary under test."""
import subprocess
from pathlib import Path


def ensure_binary(repo_root: Path) -> tuple:
    cache = repo_root / "tests" / ".cache"
    cache.mkdir(parents=True, exist_ok=True)
    binary = cache / "astrald"
    subprocess.run(["go", "build", "-o", str(binary), "./cmd/astrald"],
                   cwd=repo_root, check=True)
    ref = subprocess.run(["git", "rev-parse", "--short", "HEAD"],
                         cwd=repo_root, check=True, capture_output=True,
                         text=True).stdout.strip()
    return binary, ref
