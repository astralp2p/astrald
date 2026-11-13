"""The Docker world against a faked docker CLI: what it asks the engine to
do, and what it hands the runner and the operator.

why faked: the selftests start no daemon and need no engine. The live
behaviour — containers reaching each other, the operator unable to read a
node's token — is checked by hand on a host with Docker (the task's Mac
checks), and by test_docker_live when ASTRAL_TESTS_DOCKER=1.
"""
import os
import subprocess
import tempfile
import unittest
from pathlib import Path
from types import SimpleNamespace
from unittest import mock

from lib import agent, nodeconfig
from lib.worlds import WorldError
from lib.worlds import docker
from lib.nodeconfig import PortSpan
from lib.worlds.docker import DockerWorld


class FakeDocker:
    """Records every docker call; answers the few whose output matters."""

    def __init__(self, fail=None, ps="", networks="", gateway="172.18.0.1"):
        self.calls = []
        self.fail = fail or (lambda args: False)
        self.ps, self.networks, self.gateway = ps, networks, gateway

    def __call__(self, *args, check=True, timeout=None):
        args = [str(a) for a in args]
        self.calls.append(args)
        if self.fail(args):
            if check:
                raise WorldError(f"docker {args[0]}: exit 1")
            return ""
        if args[0] == "inspect":
            return f"172.18.0.{len(self.of('run')) + 1}"
        if args[0] == "ps":
            return self.ps
        if args[:2] == ["network", "ls"]:
            return self.networks
        if args[:2] == ["network", "inspect"]:
            return self.gateway
        if args[0] == "exec" and args[-1] == "cat /proc/net/route":
            return ROUTES
        if args[0] == "exec" and "grep OPENAI_MODEL=" in args[-1]:
            return "OPENAI_MODEL=m"
        return ""

    def of(self, verb):
        return [c for c in self.calls if c[0] == verb]

    def scripts(self):
        return [c[-1] for c in self.calls
                if c[0] == "exec" and c[-3:-1] == ["sh", "-c"]]


# a container's routes on a world network whose gateway is 172.18.0.1
ROUTES = """\
Iface\tDestination\tGateway \tFlags\tRefCnt\tUse\tMetric\tMask\t\tMTU\tWindow\tIRTT
eth0\t00000000\t010012AC\t0003\t0\t0\t0\t00000000\t0\t0\t0
eth0\t000012AC\t00000000\t0001\t0\t0\t0\t0000FFFF\t0\t0\t0
"""


def a_test(name="t", nodes=("node1",), ops=()):
    return SimpleNamespace(name=name, nodes=list(nodes), ops=list(ops))


class WorldCase(unittest.TestCase):
    def setUp(self):
        self.tmp = Path(tempfile.mkdtemp())
        self.addCleanup(lambda: __import__("shutil").rmtree(self.tmp, True))
        self.fake = FakeDocker()

    def world(self, max_nodes=20, **kw):
        patches = [mock.patch.object(docker, "_docker", self.fake),
                   mock.patch.object(DockerWorld, "_whoami",
                                     lambda self, port, token: f"02{port}")]
        for p in patches:
            p.start()
            self.addCleanup(p.stop)
        return DockerWorld(self.tmp / "20261104T000000Z" / "w0",
                           self.tmp / "linux-arm64" / "astrald",
                           "astral-node:abc", PortSpan(20800, max_nodes),
                           **kw)


class TestStart(WorldCase):
    def test_each_roster_node_starts_once_on_one_network(self):
        w = self.world()
        w.ensure(a_test(nodes=["node1", "node2"]))
        w.ensure(a_test(nodes=["node1"]))
        self.assertEqual(len(self.fake.of("run")), 2)
        self.assertEqual([c[:2] for c in self.fake.calls
                          if c[:2] == ["network", "create"]],
                         [["network", "create"]])

    def test_a_roster_past_the_span_starts_none_of_its_new_nodes(self):
        w = self.world(max_nodes=2)
        w.ensure(a_test(nodes=["node1"]))
        with self.assertRaisesRegex(
                WorldError, r"^node2, node3: the world already runs 1 nodes, "
                r"and \[ports\] span in tests/config.toml gives a world 2$"):
            w.ensure(a_test(nodes=["node1", "node2", "node3"]))
        self.assertEqual(len(self.fake.of("run")), 1)
        w.ensure(a_test(nodes=["node1"]))

    def test_each_node_broadcasts_on_a_udp_port_of_its_own(self):
        # why: the world's containers share one bridge, and a node that
        # hears a peer's nearby broadcast learns its endpoint off the swarm
        w = self.world()
        w.ensure(a_test(nodes=["node1", "node2"]))
        ports = [line for s in self.fake.scripts() if "ether.yaml" in s
                 for line in s.split("\n") if "udp_port" in line]
        self.assertEqual(len(ports), 2)
        self.assertEqual(len(set(ports)), 2)

    def test_a_container_gets_nothing_of_the_host(self):
        w = self.world()
        w.ensure(a_test())
        [run] = self.fake.of("run")
        for flag in ("-v", "--volume", "--mount", "-e", "--env",
                     "--env-file", "--network=host", "--privileged"):
            self.assertNotIn(flag, run)
        self.assertNotIn("host", run[run.index("--network") + 1])
        publish = run[run.index("-p") + 1]
        self.assertEqual(publish, "127.0.0.1:20800:8625")
        self.assertIn("astral.tests.run=20261104T000000Z", run)
        self.assertIn(f"astral.tests.pid={os.getpid()}", run)

    def test_the_binaries_under_test_are_copied_in(self):
        w = self.world()
        w.ensure(a_test())
        dsts = [c[2] for c in self.fake.of("cp")]
        self.assertTrue(any(d.endswith(":/usr/local/bin/astrald")
                            for d in dsts))
        self.assertTrue(any(d.endswith(":/usr/local/bin/astral-query")
                            for d in dsts))

    def test_configs_are_root_only_and_name_the_token(self):
        w = self.world(config={"node1": {"apphost": {"bind_http": "x"},
                                          "tor": {"enabled": False}}})
        w.ensure(a_test())
        [script] = [s for s in self.fake.scripts() if "apphost.yaml" in s]
        token = w._nodes["node1"]["token"]
        self.assertIn(token, script)
        self.assertIn('"tcp:0.0.0.0:8625"', script)
        self.assertIn("install -d -m 700 /var/lib/astrald/config", script)
        for module in ("apphost", "user", "tor"):
            self.assertIn(f"chmod 600 /var/lib/astrald/config/{module}.yaml",
                          script)
        self.assertIn('"bind_http": "x"', script)

    def test_mcp_is_published_at_the_leased_port_and_declares_the_tools(self):
        w = self.world()
        w.ensure(a_test(nodes=["node1", "node2"]))
        runs = self.fake.of("run")
        self.assertEqual([[r[i + 1] for i, a in enumerate(r) if a == "-p"]
                          for r in runs],
                         [["127.0.0.1:20800:8625", "127.0.0.1:20804:8626"],
                          ["127.0.0.1:20810:8625", "127.0.0.1:20814:8626"]])
        [script] = [s for s in self.fake.scripts() if "apphost.yaml" in s
                    and "ether" in s and "20802" in s]
        self.assertIn('"bind_mcp": "tcp:0.0.0.0:8626"', script)
        self.assertIn("chmod 600 /var/lib/astrald/config/mcp.yaml", script)
        for tool in nodeconfig.NODE_OP_TOOLS.values():
            self.assertIn(f'"name": "{tool}"', script)
        node = w.nodes()["node1"]
        self.assertEqual(node["mcp_url"], "http://127.0.0.1:20804")
        self.assertIs(node["serves_mcp"], True)

    def test_a_node_with_mcp_off_publishes_no_mcp_port(self):
        # why: a published port answers on the host with nothing behind it,
        # and messaging-ops reads a refusal there
        w = self.world(config={"nomcp1": {"mcp": {"bind_mcp": ""}}})
        w.ensure(a_test(nodes=["nomcp1"]))
        [run] = self.fake.of("run")
        self.assertEqual(run.count("-p"), 1)
        [script] = [s for s in self.fake.scripts() if "apphost.yaml" in s]
        self.assertIn('"bind_mcp": ""', script)
        node = w.nodes()["nomcp1"]
        self.assertIs(node["serves_mcp"], False)
        self.assertEqual(node["mcp_url"], "http://127.0.0.1:20804")

    def test_the_authority_is_the_leased_port_at_host_ip(self):
        w = self.world()
        w.ensure(a_test(nodes=["node1", "node2"]))
        nodes = w.nodes()
        self.assertEqual(nodes["node1"]["authority_url"],
                         "http://172.18.0.1:20805/authorize")
        self.assertEqual(nodes["node2"]["authority_url"],
                         "http://172.18.0.1:20815/authorize")
        [script] = [s for s in self.fake.scripts() if "apphost.yaml" in s
                    and "20802" in s]
        self.assertIn('"endpoint": "http://172.18.0.1:20805/authorize"',
                      script)
        for action in nodeconfig.AUTH_ACTIONS:
            self.assertIn(f'"{action}"', script)
        self.assertIn("chmod 600 /var/lib/astrald/config/auth.yaml", script)

    def test_astrald_starts_detached_under_its_root(self):
        w = self.world()
        w.ensure(a_test())
        [start] = [c for c in self.fake.of("exec") if c[1] == "-d"]
        self.assertIn("exec astrald -root /var/lib/astrald", start[-1])

    def test_ops_need_a_vm_world(self):
        w = self.world()
        with self.assertRaisesRegex(WorldError, "--vm"):
            w.ensure(a_test(ops=["enable-tor node1"]))
        self.assertEqual(self.fake.of("run"), [])


class TestNodes(WorldCase):
    def test_entries_name_the_published_port_and_the_network_address(self):
        w = self.world()
        w.ensure(a_test(nodes=["node1", "node2"]))
        nodes = w.nodes()
        self.assertEqual(nodes["node1"]["endpoint"], "tcp:127.0.0.1:20800")
        self.assertEqual(nodes["node2"]["endpoint"], "tcp:127.0.0.1:20810")
        self.assertEqual(nodes["node1"]["identity"], "0220800")
        self.assertEqual(nodes["node2"]["lan_endpoint"], "tcp:172.18.0.3:1791")
        self.assertEqual(nodes["node1"]["token"], w._nodes["node1"]["token"])
        self.assertEqual(nodes["node1"]["restarts"], 0)

    def test_every_node_dials_the_host_at_the_networks_gateway(self):
        w = self.world()
        w.ensure(a_test(nodes=["node1", "node2"]))
        self.assertEqual({n["host_ip"] for n in w.nodes().values()},
                         {"172.18.0.1"})
        inspects = [c for c in self.fake.calls if c[:2] == ["network",
                                                            "inspect"]]
        self.assertEqual(inspects, [[
            "network", "inspect", "-f",
            "{{range .IPAM.Config}}{{.Gateway}} {{end}}",
            "astral-20261104t000000z-w0-net"]])

    def test_an_ipv6_gateway_is_passed_over_for_the_ipv4_one(self):
        self.fake.gateway = "fd00::1 172.19.0.1"
        w = self.world()
        w.ensure(a_test())
        self.assertEqual(w.nodes()["node1"]["host_ip"], "172.19.0.1")

    def test_no_gateway_in_the_network_falls_back_to_the_default_route(self):
        self.fake.gateway = ""
        w = self.world()
        w.ensure(a_test())
        self.assertEqual(w.nodes()["node1"]["host_ip"], "172.18.0.1")
        self.assertIn(["exec", "astral-20261104t000000z-w0-node1", "sh", "-c",
                       "cat /proc/net/route"], self.fake.calls)

    def test_no_gateway_anywhere_is_a_world_error(self):
        self.fake.gateway = ""
        with mock.patch(f"{__name__}.ROUTES", ROUTES.splitlines()[0]):
            with self.assertRaisesRegex(WorldError, "no gateway"):
                self.world().ensure(a_test())

    def test_a_restart_counts_and_reaches_the_node_again(self):
        w = self.world()
        w.ensure(a_test())
        w.restart(["node1"])
        self.assertEqual(w.nodes()["node1"]["restarts"], 1)
        self.assertTrue(any("pkill -INT -x astrald" in s
                            for s in self.fake.scripts()))

    def test_a_restart_stops_every_named_node_before_it_starts_any(self):
        # why: a node restarted beside running siblings relearns from them
        # what a restart lost (expel-survives-restart)
        w = self.world()
        w.ensure(a_test(nodes=["node1", "node2"]))
        del self.fake.calls[:]
        w.restart(["node1", "node2"])
        steps = [("stop" if "pkill -INT -x astrald" in c[-1] else "start",
                  c[-4] if c[1] == "-d" else c[1])
                 for c in self.fake.calls if c[0] == "exec"
                 and c[-3:-1] == ["sh", "-c"]
                 and ("pkill" in c[-1] or "exec astrald" in c[-1])]
        prefix = "astral-20261104t000000z-w0-"
        self.assertEqual(steps, [("stop", prefix + "node1"),
                                 ("stop", prefix + "node2"),
                                 ("start", prefix + "node1"),
                                 ("start", prefix + "node2")])
        self.assertEqual([n["restarts"] for n in w.nodes().values()], [1, 1])

    def test_a_node_whose_astrald_exited_is_dead(self):
        w = self.world()
        w.ensure(a_test(nodes=["node1", "node2"]))
        self.fake.fail = lambda a: (a[0] == "exec" and "pgrep" in a
                                    and a[1].endswith("-node2"))
        self.assertEqual(w.dead(), ["node2"])


class TestStop(WorldCase):
    def test_a_stopped_node_is_neither_in_the_view_nor_dead(self):
        w = self.world()
        w.ensure(a_test(nodes=["first", "member"]))
        w.stop(["first"])
        self.assertTrue(any("pkill -INT -x astrald" in s
                            for s in self.fake.scripts()))
        self.fake.fail = lambda a: (a[0] == "exec" and "pgrep" in a
                                    and a[1].endswith("-first"))
        self.assertEqual(list(w.nodes()), ["member"])
        self.assertEqual(w.dead(), [])

    def test_a_later_roster_or_restart_naming_a_stopped_node_is_refused(self):
        w = self.world()
        w.ensure(a_test(nodes=["first", "member"]))
        w.stop(["first"])
        with self.assertRaisesRegex(WorldError, "^later: first was stopped"):
            w.ensure(a_test(name="later", nodes=["member", "first"]))
        with self.assertRaisesRegex(WorldError, "^restart: first was stopped"):
            w.restart(["first"])
        self.assertEqual(len(self.fake.of("run")), 2)

    def test_close_still_pulls_the_stopped_nodes_log(self):
        w = self.world()
        w.ensure(a_test(nodes=["first"]))
        w.stop(["first"])
        w.close(keep=False)
        self.assertTrue(any(c[0] == "cp" and c[1].endswith("-first:"
                            "/var/lib/astrald/astrald.log")
                            for c in self.fake.calls))


class TestShell(WorldCase):
    def test_ssh_runs_as_root_in_the_named_container(self):
        w = self.world()
        w.ensure(a_test())
        w.ssh("node1", "echo hi")
        self.assertEqual(self.fake.calls[-1],
                         ["exec", "astral-20261104t000000z-w0-node1",
                          "sh", "-c", "echo hi"])

    def test_a_failing_script_is_a_world_error_unless_unchecked(self):
        w = self.world()
        w.ensure(a_test())
        self.fake.fail = lambda a: a[-1] == "false"
        with self.assertRaises(WorldError):
            w.ssh("node1", "false")
        self.assertEqual(w.ssh("node1", "false", check=False), "")

    def test_a_timeout_reaches_the_caller(self):
        def slow(*args, check=True, timeout=None):
            raise subprocess.TimeoutExpired(args, timeout)
        w = self.world()
        w.ensure(a_test())
        with mock.patch.object(docker, "_docker", slow):
            with self.assertRaises(subprocess.TimeoutExpired):
                w.ssh("node1", "sleep 9", timeout=1)

    def test_a_node_not_up_has_no_shell(self):
        with self.assertRaisesRegex(WorldError, "no node1"):
            self.world().ssh("node1", "true")

    def test_the_operator_drives_node1_through_it(self):
        # why: the operator's scripts reach a container exactly as a VM's,
        # so lib/agent.py needs no change for a Docker world
        w = self.world()
        w.ensure(a_test())
        agent.Operator(w.ssh, model="m")._set_model()
        self.assertTrue(any("OPENAI_MODEL=" in s for s in self.fake.scripts()))


class TestSkill(WorldCase):
    def test_the_skill_reaches_node1_alone_with_links_resolved(self):
        src = self.tmp / "skill-src"
        src.mkdir()
        (src / "SKILL.md").write_text("name: astral-agent\n")
        skill = self.tmp / "skill"
        skill.mkdir()
        (skill / "SKILL.md").symlink_to(src / "SKILL.md")
        w = self.world(skill=skill)
        w.ensure(a_test(nodes=["node1", "node2"]))
        to_skill = [c for c in self.fake.of("cp")
                    if c[2].endswith(docker.SKILL_DIR)]
        self.assertEqual(len(to_skill), 1)
        self.assertIn("-node1:", to_skill[0][2])

    def test_a_skill_without_skill_md_is_refused(self):
        empty = self.tmp / "empty"
        empty.mkdir()
        with self.assertRaisesRegex(WorldError, "SKILL.md"):
            self.world(skill=empty).ensure(a_test())


class TestClose(WorldCase):
    def test_close_pulls_logs_then_removes_containers_and_network(self):
        w = self.world()
        w.ensure(a_test(nodes=["node1", "node2"]))
        w.close(keep=False)
        logs = [c for c in self.fake.of("cp") if "astrald.log" in c[1]]
        self.assertEqual(len(logs), 2)
        removed = [c[2] for c in self.fake.of("rm")]
        self.assertEqual(removed, ["astral-20261104t000000z-w0-node1",
                                   "astral-20261104t000000z-w0-node2"])
        self.assertIn(["network", "rm", "astral-20261104t000000z-w0-net"],
                      self.fake.calls)

    def test_a_kept_world_stays_up(self):
        w = self.world()
        w.ensure(a_test())
        w.close(keep=True)
        self.assertEqual(self.fake.of("rm"), [])
        self.assertEqual(w.containers(), ["astral-20261104t000000z-w0-node1"])

    def test_an_interrupted_log_copy_still_removes_the_world(self):
        w = self.world()
        w.ensure(a_test())
        self.fake.fail = lambda a: a[0] == "cp" and "astrald.log" in a[1]

        def interrupt(*args, **kw):
            if args[0] == "cp":
                raise KeyboardInterrupt
            return FakeDocker.__call__(self.fake, *args, **kw)
        with mock.patch.object(docker, "_docker", interrupt), \
                self.assertRaises(KeyboardInterrupt):
            w.close(keep=False)
        self.assertEqual([c[0] for c in self.fake.calls[-2:]],
                         ["rm", "network"])

    def test_a_node_that_failed_to_start_is_still_removed(self):
        w = self.world()
        self.fake.fail = lambda a: a[0] == "run" and a[a.index("--name") + 1] \
            .endswith("node2")
        with self.assertRaises(WorldError):
            w.ensure(a_test(nodes=["node1", "node2"]))
        self.fake.fail = lambda a: False
        w.close(keep=False)
        self.assertEqual(len(self.fake.of("rm")), 2)


class TestReap(unittest.TestCase):
    def test_reap_removes_only_what_a_dead_run_left(self):
        fake = FakeDocker(ps=f"live {os.getpid()}\ngone 999999999\n"
                             f"kept {docker.KEPT}",
                          networks="gone-net 999999999\n"
                                   f"kept-net {docker.KEPT}")
        with mock.patch.object(docker, "_docker", fake):
            self.assertEqual(docker.reap(), ["gone", "gone-net"])
        self.assertIn(["rm", "-f", "gone"], fake.calls)
        self.assertNotIn(["rm", "-f", "live"], fake.calls)
        self.assertNotIn(["rm", "-f", "kept"], fake.calls)

    def test_a_kept_world_is_labelled_so_reap_leaves_it(self):
        fake = FakeDocker()
        with tempfile.TemporaryDirectory() as tmp, \
                mock.patch.object(docker, "_docker", fake), \
                mock.patch.object(DockerWorld, "_whoami",
                                  lambda self, port, token: "02aa"):
            w = DockerWorld(Path(tmp) / "20261104T000000Z" / "w0",
                            Path(tmp) / "astrald", "astral-node:abc",
                            PortSpan(20800, 20), keep=True)
            w.ensure(a_test())
        [run] = fake.of("run")
        self.assertIn(f"{docker.PID_LABEL}={docker.KEPT}", run)


class TestImage(unittest.TestCase):
    def test_the_image_carries_the_docs_this_checkout_pins(self):
        # why: the operator then reads docs that describe the astrald the
        # run tests, whatever skill the host has
        import subprocess
        pin = subprocess.run(["git", "-C", str(docker.TESTS.parent),
                              "rev-parse", "HEAD:.ai/system"], text=True,
                             capture_output=True, check=True).stdout.strip()
        self.assertEqual(docker.docs_ref(), pin)
        calls = []

        def build(cmd, **kw):
            calls.append(cmd)
            return subprocess.CompletedProcess(cmd, 0)
        with tempfile.TemporaryDirectory() as tmp, \
                mock.patch.object(docker, "BUILD_LOG", Path(tmp) / "b.log"), \
                mock.patch.object(docker, "_docker",
                                  side_effect=WorldError("no image")), \
                mock.patch.object(docker, "docs_ref", return_value=pin), \
                mock.patch.object(docker.subprocess, "run", build):
            docker.ensure_image()
        [cmd] = calls
        self.assertIn(f"ASTRAL_DOCS_REF={pin}", cmd)

    def test_a_host_skill_brings_its_skill_md_but_not_its_docs(self):
        fake = FakeDocker()
        with tempfile.TemporaryDirectory() as tmp, \
                mock.patch.object(docker, "_docker", fake), \
                mock.patch.object(DockerWorld, "_whoami",
                                  lambda self, port, token: "02aa"):
            skill = Path(tmp) / "skill"
            (skill / "references").mkdir(parents=True)
            (skill / "SKILL.md").write_text("x")
            (skill / "astral-docs").mkdir()
            (skill / "astral-docs" / "README.md").write_text("stale docs")
            copied = []
            real_cp = fake.__call__

            def cp(*args, **kw):
                if args[0] == "cp" and str(args[1]).endswith("/."):
                    copied.append(sorted(p.name for p in
                                         Path(str(args[1])[:-2]).iterdir()))
                return real_cp(*args, **kw)
            with mock.patch.object(docker, "_docker", cp):
                w = DockerWorld(Path(tmp) / "20261104T000000Z" / "w0",
                                Path(tmp) / "astrald", "astral-node:abc",
                                PortSpan(20800, 20), skill=skill)
                w.ensure(a_test())
        self.assertEqual(copied, [["SKILL.md", "references"]])


class TestHost(unittest.TestCase):
    def test_no_docker_cli_is_named(self):
        with mock.patch.object(docker.shutil, "which", return_value=None):
            self.assertEqual(docker.unavailable(), "docker is not on PATH")

    def test_an_engine_that_does_not_answer_is_named(self):
        with mock.patch.object(docker.shutil, "which", return_value="/d"), \
                mock.patch.object(docker, "_docker", FakeDocker(
                    fail=lambda a: True)):
            self.assertIn("does not answer", docker.unavailable())

    def test_the_engine_architecture_maps_to_goarch(self):
        for machine, goarch in (("x86_64", "amd64"), ("aarch64", "arm64")):
            with mock.patch.object(docker, "_docker",
                                   lambda *a, **k: machine):
                self.assertEqual(docker.engine_arch(), goarch)

    def test_the_image_tag_follows_its_inputs(self):
        tag = docker.image_tag()
        self.assertRegex(tag, r"^astral-node:[0-9a-f]{12}$")
        with mock.patch.object(docker, "QWEN_CODE", "0.0.1"):
            self.assertNotEqual(docker.image_tag(), tag)


if __name__ == "__main__":
    unittest.main()
