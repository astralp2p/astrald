"""Runs that land in Docker worlds: which host gets one, what is refused,
and what the results say.

why seams: runner.vm_unavailable and runner.docker_unavailable are the host
probes, and _prepare_docker and _docker_world stand in for the engine, so
these tests start nothing.
"""
import argparse
import contextlib
import io
import json
import tempfile
import unittest
from pathlib import Path
from types import SimpleNamespace
from unittest import mock

from lib import runner
from selftest.test_runner import FakeWorld

NO_NETSIM = "netsim runs only on Linux, and this host is Darwin"


def args(selection, **kw):
    a = argparse.Namespace(selection=selection, driver="script",
                           target="fresh", keep=False)
    for k, v in kw.items():
        setattr(a, k, v)
    return runner._with_defaults(a)


class TestHeavyKind(unittest.TestCase):
    def kind(self, why, **kw):
        with mock.patch.object(runner, "vm_unavailable", return_value=why):
            return runner.heavy_kind(args(["smoke"], **kw))

    def test_netsim_gives_vm_worlds_and_its_absence_docker(self):
        self.assertEqual(self.kind(None), "vm")
        self.assertEqual(self.kind(NO_NETSIM), "docker")

    def test_vm_and_from_ask_for_a_vm(self):
        self.assertEqual(self.kind(NO_NETSIM, vm=True), "vm")
        self.assertEqual(self.kind(NO_NETSIM, from_stage="gw"), "vm")


def run(selection, step=lambda *a, **kw: 0, **kw) -> tuple:
    """(code, results doc, the docker worlds opened, stderr) of a run on a
    host without netsim and with Docker, over a fake world."""
    made = []

    def world(*a, **k):
        made.append((a, FakeWorld()))
        return made[-1][1]

    err = io.StringIO()
    skill = kw.pop("skill", "present")
    with tempfile.TemporaryDirectory() as tmp, \
            mock.patch.object(runner, "RESULTS", Path(tmp)), \
            mock.patch.object(runner, "vm_unavailable", return_value=NO_NETSIM), \
            mock.patch.object(runner, "docker_unavailable", return_value=None), \
            mock.patch.object(runner, "ensure_binary",
                              return_value=(Path(tmp) / "astrald", "abc")), \
            mock.patch.object(runner, "_prepare_docker",
                              return_value=(Path(tmp) / "linux" / "astrald",
                                            "astral-node:abc")), \
            mock.patch.object(runner, "_docker_world", world), \
            mock.patch.object(runner, "_local_world",
                              lambda *a: self_fail("a local world")), \
            mock.patch.object(runner, "_vm_world",
                              lambda *a: self_fail("a VM world")), \
            mock.patch.object(runner, "lease_ports",
                              return_value=SimpleNamespace(base=20800)), \
            mock.patch.object(runner, "_run_step", step), \
            mock.patch.object(runner, "_skill",
                              lambda cfg: a_skill(tmp) if skill else None), \
            contextlib.redirect_stdout(io.StringIO()), \
            contextlib.redirect_stderr(err):
        code = runner.main(args(selection, **kw))
        run_dir = next(Path(tmp).iterdir(), None)
        doc = (json.loads((run_dir / "results.json").read_text())
               if run_dir else None)
    return code, doc, made, err.getvalue()


def a_skill(tmp) -> Path:
    d = Path(tmp) / "astral-agent"
    d.mkdir(exist_ok=True)
    (d / "SKILL.md").write_text("---\nname: astral-agent\nversion: 9.9.9\n---\n")
    return d


def self_fail(what):
    raise AssertionError(f"this run opened {what}")


class TestDockerRuns(unittest.TestCase):
    def test_docker_puts_a_script_run_in_a_docker_world(self):
        code, doc, made, _ = run(["smoke"], docker=True)
        self.assertEqual(code, 0)
        self.assertEqual(len(made), 1)
        (dir, binary, image, span, keep, config, skill), _ = made[0]
        self.assertEqual((image, span.base, span.max_nodes, skill),
                         ("astral-node:abc", 20800, 20, None))
        self.assertEqual(binary.parent.name, "linux")
        self.assertEqual({e["env"] for e in doc["entries"]}, {"container"})

    def test_the_agent_driver_without_netsim_runs_in_docker(self):
        code, doc, made, _ = run(["bootstrap-user-software-key"],
                                 driver="agent")
        self.assertEqual(len(made), 1)
        [e] = doc["entries"]
        self.assertEqual((e["env"], e["driver"]), ("container", "agent"))
        self.assertEqual(doc["agent_isolation"], "container-user")
        self.assertEqual(doc["agent_version"], runner.docker.QWEN_CODE)
        self.assertEqual(doc["agent_skill"], "astral-agent 9.9.9")

    def test_an_agent_run_without_the_skill_is_refused_before_anything(self):
        code, doc, made, err = run(["bootstrap-user-software-key"],
                                   driver="agent", skill=None)
        self.assertEqual((code, doc, made), (2, None, []))
        self.assertIn("no installed astral-agent skill", err)
        self.assertIn("[agent] skill", err)

    def test_a_skill_counts_only_with_its_astral_docs_mount(self):
        # why: a raw skills checkout has SKILL.md and references/ but no
        # astral-docs, so every link the operator follows into it is missing
        with tempfile.TemporaryDirectory() as tmp:
            d = Path(tmp) / "astral-agent"
            (d / "references").mkdir(parents=True)
            (d / "SKILL.md").write_text("---\nname: astral-agent\n---\n")
            cfg = {"agent": {"skill": str(d)}}
            self.assertIsNone(runner._skill(cfg))
            (d / "astral-docs").mkdir()
            (d / "astral-docs" / "README.md").write_text("docs\n")
            self.assertEqual(runner._skill(cfg), d.resolve())

    def test_a_host_without_docker_refuses_before_anything(self):
        with mock.patch.object(runner, "docker_unavailable",
                               return_value="docker is not on PATH"), \
                mock.patch.object(runner, "vm_unavailable",
                                  return_value=NO_NETSIM), \
                mock.patch.object(runner, "ensure_binary",
                                  side_effect=AssertionError("built")), \
                mock.patch.object(runner, "lease_ports",
                                  side_effect=AssertionError("leased")), \
                contextlib.redirect_stderr(io.StringIO()) as err:
            self.assertEqual(runner.main(args(["smoke"], docker=True)), 2)
        self.assertIn("docker is not on PATH", err.getvalue())

    def test_an_engine_that_fails_to_prepare_leaves_no_run(self):
        from lib.worlds import WorldError
        with tempfile.TemporaryDirectory() as tmp, \
                mock.patch.object(runner, "RESULTS", Path(tmp) / "results"), \
                mock.patch.object(runner, "docker_unavailable",
                                  return_value=None), \
                mock.patch.object(runner, "ensure_binary",
                                  return_value=(Path(tmp) / "a", "abc")), \
                mock.patch.object(runner, "lease_ports",
                                  return_value=SimpleNamespace(base=0)), \
                mock.patch.object(runner, "_prepare_docker",
                                  side_effect=WorldError("build failed")), \
                contextlib.redirect_stderr(io.StringIO()) as err:
            self.assertEqual(runner.main(args(["smoke"], docker=True)), 2)
            self.assertFalse((Path(tmp) / "results").exists())
        self.assertIn("build failed", err.getvalue())

    def test_docker_refuses_save_from_and_attach(self):
        for kw in ({"save": "gw"}, {"from_stage": "gw"},
                   {"target": "attach"}):
            with self.subTest(**kw), \
                    contextlib.redirect_stderr(io.StringIO()):
                self.assertEqual(runner.main(args(["smoke"], docker=True,
                                                  **kw)), 2)

    def test_a_vm_run_records_the_vm_seat(self):
        made = []

        def world(*a):
            made.append(FakeWorld())
            return made[-1]

        with tempfile.TemporaryDirectory() as tmp, \
                mock.patch.object(runner, "RESULTS", Path(tmp)), \
                mock.patch.object(runner, "vm_unavailable", return_value=None), \
                mock.patch.object(runner, "ensure_binary",
                                  return_value=(Path(tmp) / "astrald", "abc")), \
                mock.patch.object(runner, "_vm_world", world), \
                mock.patch.object(runner, "_run_step", lambda *a, **k: 0), \
                contextlib.redirect_stdout(io.StringIO()), \
                contextlib.redirect_stderr(io.StringIO()):
            runner.main(args(["bootstrap-user-software-key"], driver="agent"))
            doc = json.loads((next(Path(tmp).iterdir())
                              / "results.json").read_text())
        self.assertEqual((doc["agent_isolation"], doc["agent_version"]),
                         ("vm-user", "(not recorded)"))


if __name__ == "__main__":
    unittest.main()
