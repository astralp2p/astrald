"""lib/apps.py: app processes that outlive a driver and die with their world."""
import argparse
import contextlib
import io
import os
import subprocess
import sys
import tempfile
import textwrap
import time
import unittest
from pathlib import Path
from unittest import mock

from lib import apps, runner

TESTS = Path(__file__).resolve().parent.parent

# an app: writes its state, spawns a child of its own, and serves until killed
APP = textwrap.dedent("""
    import json, os, subprocess, sys, time
    child = subprocess.Popen([sys.executable, "-c", "import time; time.sleep(600)"])
    json.dump({"pid": os.getpid(), "child": child.pid, "ready": True},
              open(os.environ["ASTRAL_TESTS_APP_STATE"], "w"))
    time.sleep(600)
""")

# runs in another process, as a driver does: starts an app and exits
DRIVER = textwrap.dedent("""
    import sys
    from lib import apps
    app = apps.spawn("one", [sys.executable, "-c", sys.argv[1]])
    app.wait_state(lambda s: s.get("ready"), timeout=20)
    print(app.pid)
""")


def gone(pid: int) -> bool:
    deadline = time.monotonic() + 5
    while time.monotonic() < deadline:
        try:
            os.kill(pid, 0)
        except ProcessLookupError:
            return True
        with contextlib.suppress(OSError):
            if Path(f"/proc/{pid}/stat").read_text().rpartition(") ")[2][:1] == "Z":
                return True
        time.sleep(0.05)
    return False


class AppsCase(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.dir = Path(self.tmp.name) / "apps"
        self.env = mock.patch.dict(os.environ, {apps.ENV_DIR: str(self.dir)})
        self.env.start()
        self.addCleanup(self.env.stop)
        self.addCleanup(apps.reap, self.dir)

    def drive(self, script: str = APP) -> int:
        """Start app `one` from a driver process that then exits; its pid."""
        out = subprocess.run(
            [sys.executable, "-c", DRIVER, script], capture_output=True,
            text=True, timeout=60, cwd=TESTS,
            env={**os.environ, "PYTHONPATH": str(TESTS)})
        self.assertEqual(out.returncode, 0, out.stderr)
        return int(out.stdout)


class TestSpawn(AppsCase):
    def test_an_app_outlives_the_driver_that_started_it(self):
        pid = self.drive()
        app = apps.get("one")
        self.assertEqual(app.pid, pid)
        self.assertTrue(app.alive())
        self.assertEqual(app.state()["pid"], pid)

    def test_stop_ends_the_app_and_what_it_started(self):
        self.drive()
        app = apps.get("one")
        child = app.state()["child"]
        app.stop()
        self.assertTrue(gone(app.pid))
        self.assertTrue(gone(child))
        self.assertFalse(app.alive())
        app.stop()                       # stopping a stopped app is no error

    def test_a_signal_to_the_spawners_group_does_not_reach_the_app(self):
        # why: the runner's Ctrl-C reaches its whole process group, and the
        # app must live until the world's teardown stops it
        app = apps.spawn("one", [sys.executable, "-c", APP])
        app.wait_state(lambda s: s.get("ready"), timeout=20)
        self.assertNotEqual(os.getpgid(app.pid), os.getpgid(0))
        self.assertEqual(os.getpgid(app.pid), app.pid)

    def test_a_name_in_use_is_refused_until_the_app_stops(self):
        app = apps.spawn("one", [sys.executable, "-c", "import time; time.sleep(60)"])
        with self.assertRaisesRegex(apps.AppError, "already running"):
            apps.spawn("one", [sys.executable, "-c", "pass"])
        app.stop()
        apps.spawn("one", [sys.executable, "-c", "pass"])

    def test_the_app_gets_its_state_path_and_the_env_it_was_given(self):
        app = apps.spawn("one", [sys.executable, "-c", textwrap.dedent("""
            import json, os
            json.dump({"x": os.environ["X"]}, open(os.environ["ASTRAL_TESTS_APP_STATE"], "w"))
            """)], env={"X": "secret"})
        self.assertEqual(app.wait_state(lambda s: "x" in s, timeout=20), {"x": "secret"})
        # the registry holds argv, never the environment
        self.assertNotIn("secret", app.entry.read_text())

    def test_wait_state_names_an_app_that_died_with_its_log(self):
        app = apps.spawn("one", [sys.executable, "-c", "print('boom: bad token')"])
        with self.assertRaisesRegex(apps.AppError, "(?s)exited before it was ready.*boom"):
            app.wait_state(lambda s: s.get("ready"), timeout=20)

    def test_wait_state_times_out_with_the_last_state(self):
        app = apps.spawn("one", [sys.executable, "-c", textwrap.dedent("""
            import json, os, time
            json.dump({"n": 1}, open(os.environ["ASTRAL_TESTS_APP_STATE"], "w"))
            time.sleep(60)
            """)])
        with self.assertRaisesRegex(apps.AppError, r"not ready after 1s \(state \{'n': 1\}"):
            app.wait_state(lambda s: s.get("ready"), timeout=1)

    def test_a_pid_taken_by_another_process_is_not_the_app(self):
        # why the start time: a recorded pid can name a later process
        app = apps.spawn("one", [sys.executable, "-c", "import time; time.sleep(60)"])
        if app.started is None:
            self.skipTest("no /proc: start times are not recorded")
        impostor = apps.App(self.dir, "one", app.pid, str(int(app.started) + 1), [])
        child = apps._children.pop(app.pid)   # as in an oracle: not its spawner
        self.addCleanup(lambda: (apps.reap(self.dir), child.wait()))
        self.assertFalse(impostor.alive())
        self.assertTrue(apps.get("one").alive())

    def test_outside_a_run_there_is_no_world_to_spawn_in(self):
        with mock.patch.dict(os.environ):
            os.environ.pop(apps.ENV_DIR)
            with self.assertRaisesRegex(apps.AppError, "ASTRAL_TESTS_APPS"):
                apps.spawn("one", [sys.executable, "-c", "pass"])

    def test_an_app_never_spawned_is_named(self):
        with self.assertRaisesRegex(apps.AppError, "no app ghost"):
            apps.get("ghost")


class TestReap(AppsCase):
    def test_reap_stops_every_app_of_the_world_and_names_the_running(self):
        self.drive()
        a = apps.get("one")
        b = apps.spawn("two", [sys.executable, "-c", "import time; time.sleep(60)"])
        quick = apps.spawn("three", [sys.executable, "-c", "pass"])
        time.sleep(0.5)
        self.assertEqual(apps.reap(self.dir), ["one", "two"])
        self.assertTrue(gone(a.pid) and gone(b.pid) and gone(quick.pid))
        self.assertEqual(apps.reap(self.dir), [])

    def test_reap_of_a_world_that_started_no_app_is_nothing(self):
        self.assertEqual(apps.reap(Path(self.tmp.name) / "none"), [])

    def test_build_go_puts_the_binary_in_the_worlds_bin(self):
        out = apps.build_go("tests/e2e/app-node-restart-reconnect/driver")
        self.assertEqual(out, self.dir / "bin" / "driver")
        self.assertTrue(os.access(out, os.X_OK))
        with self.assertRaisesRegex(apps.AppError, "go build"):
            apps.build_go("tests/e2e/no-such-driver")


class TestRunnerReap(unittest.TestCase):
    """The runner stops a world's apps when the world closes, however the
    run ends."""

    def run_world(self, boom):
        started = []

        def step(py, script, log, env, timeout):
            # a driver: start an app in the world the step's env names
            if script.name == "script.py" and not started:
                with mock.patch.dict(os.environ, {apps.ENV_DIR: env["ASTRAL_TESTS_APPS"]}):
                    app = apps.spawn("one", [sys.executable, "-c", "import time; time.sleep(60)"])
                started.append(app)
                return boom()
            return 0

        from selftest.test_runner import FakeWorld
        args = argparse.Namespace(selection=["smoke"], driver="script", target="fresh",
                                  keep=False)
        with tempfile.TemporaryDirectory() as tmp, \
                mock.patch.object(runner, "RESULTS", Path(tmp)), \
                mock.patch.object(runner, "vm_unavailable", return_value=None), \
                mock.patch.object(runner, "ensure_binary",
                                  return_value=(Path(tmp) / "astrald", "abc")), \
                mock.patch.object(runner, "_local_world", lambda *_: FakeWorld()), \
                mock.patch.object(runner, "lease_ports",
                                  return_value=mock.Mock(base=0)), \
                mock.patch.object(runner, "_run_step", step), \
                contextlib.redirect_stdout(io.StringIO()), \
                contextlib.redirect_stderr(io.StringIO()):
            try:
                runner.main(args)
            except KeyboardInterrupt:
                pass
        self.assertEqual(len(started), 1)
        self.assertTrue(gone(started[0].pid), "the app outlived its world")

    def test_a_passing_world_stops_its_apps(self):
        self.run_world(lambda: 0)

    def test_a_failing_driver_still_stops_its_apps(self):
        self.run_world(lambda: 1)

    def test_ctrl_c_and_sigterm_stop_the_apps(self):
        def interrupt():
            raise KeyboardInterrupt      # tests/run reads SIGTERM and SIGHUP as this
        self.run_world(interrupt)


if __name__ == "__main__":
    unittest.main()
