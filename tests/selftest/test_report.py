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

    def test_failure_heading_names_the_phase(self):
        text = render(doc(entry("policy-delegate", "fail",
                                failure_kind="environment", phase="restart")),
                      "r")
        self.assertIn("### policy-delegate — environment in restart", text)
        same = render(doc(entry("smoke", "fail", failure_kind="verify",
                                phase="verify")), "r")
        self.assertIn("### smoke — verify\n", same)

    def test_a_run_of_several_worlds_names_each_tests_world(self):
        text = render(doc(entry("smoke", "pass", world="w0"),
                          entry("bootstrap", "pass", kind="prereq",
                                world="w1"),
                          entry("hold-purge", "pass", world="w1")), "r")
        self.assertIn("| Test | World | Env |", text)
        self.assertIn("| hold-purge | w1 | node |", text)
        one = render(doc(entry("smoke", "pass", world="w0")), "r")
        self.assertNotIn("World", one)
        skips = render(doc(entry("smoke", "pass", world="w0"),
                           entry("hold-purge", "skipped", world="w1",
                                 reason="adopt-node failed in w1")), "r")
        self.assertIn("- `hold-purge` (w1) — adopt-node failed in w1", skips)

    def test_skip_reasons_are_listed(self):
        text = render(doc(entry("bootstrap", "fail", failure_kind="verify"),
                          entry("adopt-node", "skipped",
                                reason="state one-node was never built"),
                          entry("expel-node", "skipped")),
                      "r")
        self.assertIn("- `adopt-node` — state one-node was never built", text)
        self.assertIn("- `expel-node`\n", text)    # a pre-v2.2 skip, no reason

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
        ended = summary_line(dict(doc(entry("a", "pass")),
                                  aborted="interrupted"))
        self.assertTrue(ended.startswith("INCOMPLETE"))
        self.assertTrue(ended.endswith("(ended early: interrupted)"))


if __name__ == "__main__":
    unittest.main()


class TestHostRow(unittest.TestCase):
    def test_a_v2_3_run_names_its_os_and_machine(self):
        text = render(dict(doc(entry("smoke", "pass")), host_os="Linux 6.1",
                           host_arch="aarch64"), "20260811T000000Z")
        self.assertIn("| Host | pi · Linux 6.1 aarch64 |", text)

    def test_an_older_run_names_its_host_alone(self):
        text = render(doc(entry("smoke", "pass")), "20260811T000000Z")
        self.assertIn("| Host | pi |", text)
