import os
import subprocess
import tempfile
import unittest
from pathlib import Path

from lib.build import missing_commit

# why: the test's own git calls must not read the host's git setup; a global
# commit.gpgsign or an inherited GIT_DIR breaks them.
GIT_ENV = {k: v for k, v in os.environ.items() if not k.startswith("GIT_")} | {
    "GIT_CONFIG_GLOBAL": os.devnull, "GIT_CONFIG_NOSYSTEM": "1"}


def git(cwd: Path, *args: str) -> str:
    return subprocess.run(["git", *args], cwd=cwd, check=True, env=GIT_ENV,
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

            self.assertEqual(missing_commit(repo, required), "")
            self.assertEqual(missing_commit(repo, old), "")

            git(repo, "checkout", "-q", old)
            self.assertIn(f"lacks {required[:8]}", missing_commit(repo, required))

    def test_a_commit_git_cannot_find_is_not_reported_as_lacking(self):
        with tempfile.TemporaryDirectory() as tmp:
            repo = Path(tmp)
            git(repo, "init", "-q")
            commit(repo, "only")
            reason = missing_commit(repo, "d80c0acdf537f02596f386b535215ce6237d7373")
            self.assertIn("git cannot tell", reason)


if __name__ == "__main__":
    unittest.main()
