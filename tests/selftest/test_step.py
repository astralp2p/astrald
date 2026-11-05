"""lib/step.py: a script's own programming error exits FAULT, nothing else."""
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

from lib import step


def run(body: str) -> subprocess.CompletedProcess:
    with tempfile.TemporaryDirectory() as tmp:
        script = Path(tmp) / "verify.py"
        script.write_text(body)
        (Path(tmp) / "sibling.py").write_text("NAME = 'sibling'\n")
        return subprocess.run([sys.executable, step.__file__, str(script)],
                              capture_output=True, text=True, timeout=30)


class TestStep(unittest.TestCase):
    def test_a_clean_script_exits_0(self):
        p = run("print('ok')\n")
        self.assertEqual((p.returncode, p.stdout), (0, "ok\n"))

    def test_a_failed_assertion_is_a_verdict(self):
        self.assertEqual(run("assert False, 'astrald answered x'\n").returncode,
                         1)

    def test_an_error_the_node_caused_is_a_verdict(self):
        p = run("class RemoteError(Exception): pass\n"
                "raise RemoteError('refused')\n")
        self.assertEqual(p.returncode, 1)
        self.assertIn("RemoteError: refused", p.stderr)

    def test_a_programming_error_exits_fault(self):
        for body in ("bytes('x')\n", "{}['user_id']\n", "undefined\n",
                     "import asyncio\n"
                     "async def main(): 'a' in False\n"
                     "asyncio.run(main())\n"):
            with self.subTest(body=body):
                p = run(body)
                self.assertEqual(p.returncode, step.FAULT, p.stderr)
                self.assertIn("Traceback", p.stderr)

    def test_an_explicit_exit_code_is_kept(self):
        self.assertEqual(run("import sys\nsys.exit(5)\n").returncode, 5)

    def test_the_script_runs_as_main_from_its_own_directory(self):
        p = run("import sibling\n"
                "from pathlib import Path\n"
                "print(__name__, sibling.NAME, Path(__file__).name)\n")
        self.assertEqual(p.stdout, "__main__ sibling verify.py\n", p.stderr)


if __name__ == "__main__":
    unittest.main()
