import subprocess
import tempfile
import unittest
from pathlib import Path

from lib.build import lacks_commit


def git(cwd: Path, *args: str) -> str:
    return subprocess.run(["git", *args], cwd=cwd, check=True,
                          capture_output=True, text=True).stdout.strip()


def commit(cwd: Path, message: str) -> str:
    git(cwd, "-c", "user.name=t", "-c", "user.email=t@t", "commit",
        "--allow-empty", "-qm", message)
    return git(cwd, "rev-parse", "HEAD")


class TestAstralPyRequires(unittest.TestCase):
    def test_a_checkout_is_checked_for_the_required_commit(self):
        # why: an astral-py checkout older than the required commit sends
        # argument names the node no longer declares, and the run failed late,
        # after setup had already mutated the node.
        with tempfile.TemporaryDirectory() as tmp:
            repo = Path(tmp)
            git(repo, "init", "-q")
            old = commit(repo, "old")
            required = commit(repo, "required")

            self.assertFalse(lacks_commit(repo, required))
            self.assertFalse(lacks_commit(repo, old))

            git(repo, "checkout", "-q", old)
            self.assertTrue(lacks_commit(repo, required))

    def test_a_tree_git_cannot_read_lacks_every_commit(self):
        with tempfile.TemporaryDirectory() as tmp:
            self.assertTrue(lacks_commit(Path(tmp), "d80c0acdf537f02596f386b535215ce6237d7373"))


if __name__ == "__main__":
    unittest.main()
