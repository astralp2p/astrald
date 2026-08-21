"""A suite is a file: these tests, in this order — composition, nothing else.

What a test needs stays in its manifest; a suite never restates it.
"""
from pathlib import Path

SUFFIX = ".suite"


def find(suites_dir: Path, name: str) -> Path | None:
    """The suite file `name` refers to, or None when it names no suite.

    why: a name ending in .suite must resolve — a typo there is a broken
    suite reference, never a test name to fall through to.
    """
    named = name.endswith(SUFFIX)
    path = Path(suites_dir) / (name if named else name + SUFFIX)
    if path.exists():
        return path
    if named:
        raise ValueError(f"no such suite: {path}")
    return None


def load(path: Path) -> list:
    """The test names a suite lists, in order. `#` comments, blanks ignored."""
    names = []
    for raw in Path(path).read_text().splitlines():
        line = raw.split("#", 1)[0].strip()
        if line:
            names.append(line)
    if not names:
        raise ValueError(f"{path}: suite lists no tests")
    return names
