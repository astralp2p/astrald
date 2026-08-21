"""The shipped manifests and suites, checked against each other.

The other selftests exercise the rules on synthetic trees; this one holds the
repository to them, so a manifest typo or a suite reordered into an
unsatisfiable walk fails here rather than after a daemon has booted.
"""
import unittest
from pathlib import Path

from lib.manifest import load_all, resolve, validate_order
from lib.suites import load

TESTS = Path(__file__).resolve().parent.parent


class TestShipped(unittest.TestCase):
    def setUp(self):
        self.all = load_all(TESTS / "e2e")

    def test_every_suite_walks(self):
        suites = sorted((TESTS / "suites").glob("*.suite"))
        self.assertTrue(suites, "no suites ship")
        for path in suites:
            with self.subTest(suite=path.name):
                validate_order(self.all, load(path))

    def test_every_test_resolves_alone(self):
        for name in sorted(self.all):
            with self.subTest(test=name):
                plan = resolve(self.all, [name])
                self.assertEqual(plan[-1][0].name, name)
                self.assertEqual(plan[-1][1], "test")

    def test_every_test_ships_its_four_files(self):
        for name, t in sorted(self.all.items()):
            with self.subTest(test=name):
                for f in ("test.toml", "script.py", "verify.py", "README.md"):
                    self.assertTrue((t.dir / f).is_file(), f"{name}/{f} missing")

    def test_a_mutator_is_last_in_every_suite_that_lists_it(self):
        # why: the walk already rejects a consumer placed after a mutator, but
        # a mutator mid-suite means every following test was skipped by design
        # rather than by accident — say it once, here.
        for path in sorted((TESTS / "suites").glob("*.suite")):
            listed = load(path)
            for i, name in enumerate(listed[:-1]):
                with self.subTest(suite=path.name, test=name):
                    self.assertFalse(self.all[name].mutates,
                                     f"{name} mutates and is not last")


if __name__ == "__main__":
    unittest.main()
