"""The presentable document, held to the verdicts it renders."""
import unittest

from lib.report import render, summary_line

HEADER = {
    "astrald_ref": "abc1234", "astral_py_ref": "def5678", "host": "pi",
    "sandbox": "host", "hermetic": True, "selection": "main.suite",
    "target": "fresh", "started": "2026-08-11T00:00:00Z", "wall_time_s": 3.5,
}


def doc(*entries):
    return dict(HEADER, entries=list(entries))


def entry(name, status, **kw):
    e = {"test": name, "kind": "test", "env": "node", "driver": "script",
         "status": status, "duration_s": 1.0, "artifacts": f"{name}/"}
    e.update(kw)
    return e


class TestReport(unittest.TestCase):
    def test_green_run_reads_as_pass(self):
        text = render(doc(entry("smoke", "pass")), "20260811T000000Z")
        self.assertIn("# Integration test run — PASS", text)
        self.assertIn("**1 of 1 passed**", text)
        self.assertNotIn("## Failures", text)

    def test_failure_names_the_layer_and_its_log(self):
        text = render(doc(entry("smoke", "fail", failure_kind="verify")),
                      "20260811T000000Z")
        self.assertIn("— FAIL", text)
        self.assertIn("### smoke — verify", text)
        self.assertIn("astrald misbehaved", text)
        self.assertIn("20260811T000000Z/smoke/", text)

    def test_skipped_run_is_not_green(self):
        # why: a skipped test carries no verdict, so a run holding one must
        # never render as PASS — that is the whole point of the third status.
        text = render(doc(entry("smoke", "pass"),
                          entry("adopt-node", "skipped")),
                      "20260811T000000Z")
        self.assertIn("— INCOMPLETE", text)
        self.assertIn("## Skipped", text)

    def test_note_column_appears_only_when_there_is_a_note(self):
        green = render(doc(entry("smoke", "pass")), "r")
        self.assertNotIn("| Note |", green)
        prereq = render(doc(entry("bootstrap", "pass", kind="prereq")), "r")
        self.assertIn("| Note |", prereq)
        self.assertIn("_(prereq)_", prereq)

    def test_header_states_what_was_run(self):
        text = render(dict(HEADER, hermetic=False, target="attach",
                           selection="smoke", entries=[entry("smoke", "pass")]),
                      "r")
        self.assertIn("`smoke`", text)
        self.assertIn("attach (not hermetic)", text)

    def test_summary_line_matches_the_document_verdict(self):
        self.assertTrue(summary_line(doc(entry("a", "pass"))).startswith("PASS"))
        self.assertTrue(summary_line(
            doc(entry("a", "fail", failure_kind="driver"))).startswith("FAIL"))
        self.assertTrue(summary_line(
            doc(entry("a", "skipped"))).startswith("INCOMPLETE"))


if __name__ == "__main__":
    unittest.main()
