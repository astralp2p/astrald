import tempfile
import unittest
from pathlib import Path

from lib.suites import find, load


class TestSuites(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.dir = Path(self.tmp.name)
        (self.dir / "main.suite").write_text(
            "# the env-node chain\n"
            "smoke\n"
            "\n"
            "bootstrap-user-software-key   # produces one-node\n"
            "adopt-node\n")

    def tearDown(self):
        self.tmp.cleanup()

    def test_load_strips_comments_and_blanks(self):
        self.assertEqual(load(self.dir / "main.suite"),
                         ["smoke", "bootstrap-user-software-key", "adopt-node"])

    def test_find_accepts_bare_and_suffixed_names(self):
        self.assertEqual(find(self.dir, "main"), self.dir / "main.suite")
        self.assertEqual(find(self.dir, "main.suite"), self.dir / "main.suite")

    def test_a_bare_name_that_is_no_suite_falls_through(self):
        self.assertIsNone(find(self.dir, "adopt-node"))

    def test_a_missing_suffixed_name_is_an_error(self):
        # why: `run typo.suite` is a broken suite reference, never a test name
        with self.assertRaisesRegex(ValueError, "no such suite"):
            find(self.dir, "typo.suite")

    def test_empty_suite_is_rejected(self):
        (self.dir / "empty.suite").write_text("# nothing here\n")
        with self.assertRaisesRegex(ValueError, "lists no tests"):
            load(self.dir / "empty.suite")


if __name__ == "__main__":
    unittest.main()
