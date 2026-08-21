import tempfile
import unittest
from pathlib import Path

from lib.results import fresh_run_dir


class TestRunDir(unittest.TestCase):
    def test_same_second_runs_do_not_share_a_directory(self):
        # why: the stamp has one-second resolution. Two runs that shared a
        # directory overwrote results.json and interleaved events.jsonl.
        with tempfile.TemporaryDirectory() as tmp:
            made = [fresh_run_dir(Path(tmp)) for _ in range(3)]
            self.assertEqual(len(set(made)), 3)
            for d in made:
                self.assertTrue(d.is_dir())


if __name__ == "__main__":
    unittest.main()
