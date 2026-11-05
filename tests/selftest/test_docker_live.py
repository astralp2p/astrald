"""The Docker world against a real engine: two containers, the operator's
seat, and the cleanup.

It runs only with ASTRAL_TESTS_DOCKER=1 and a Docker engine that answers:
the first run builds the node image (minutes, and npm's registry), and every
run builds astrald for Linux and starts two daemons. Everything else in
selftest/ takes milliseconds and touches no daemon.

    cd tests && ASTRAL_TESTS_DOCKER=1 PYTHONPATH=.:<astral-py>/src \\
        python3 -m unittest selftest.test_docker_live -v
"""
import json
import os
import shlex
import tempfile
import tomllib
import unittest
from pathlib import Path
from types import SimpleNamespace

from lib import agent
from lib.build import linux_binaries
from lib.nodeconfig import PORTS_PER_NODE, lease_ports
from lib.worlds import WorldError, docker

TESTS = Path(__file__).resolve().parent.parent
WANTED = os.environ.get("ASTRAL_TESTS_DOCKER") == "1"
WHY = (None if not WANTED else docker.unavailable())


@unittest.skipUnless(WANTED, "ASTRAL_TESTS_DOCKER=1 runs the live Docker tests")
@unittest.skipIf(WHY, f"no Docker engine: {WHY}")
class TestDockerLive(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cfg = tomllib.loads((TESTS / "config.toml").read_text())
        cls.lease = lease_ports(**cfg["ports"])
        binary = linux_binaries(TESTS.parent, docker.engine_arch())
        cls.tmp = Path(tempfile.mkdtemp())
        cls.world = docker.DockerWorld(
            cls.tmp / "20990101T000000Z" / "w0", binary,
            docker.ensure_image(), cls.lease.base,
            max_nodes=cfg["ports"]["span"] // PORTS_PER_NODE)
        cls.world.ensure(SimpleNamespace(name="t", nodes=["node1", "node2"],
                                         ops=[]))

    @classmethod
    def tearDownClass(cls):
        cls.world.close(keep=False)
        cls.lease.lock.close()

    def test_the_host_reaches_each_node_with_its_token(self):
        for name, n in self.world.nodes().items():
            with self.subTest(node=name):
                self.assertTrue(n["identity"])

    def test_node1_reaches_node2_on_the_world_network(self):
        lan = self.world.nodes()["node2"]["lan_endpoint"]
        host, port = lan.removeprefix("tcp:").rsplit(":", 1)
        self.world.ssh("node1", f"python3 -c \"import socket; "
                                f"socket.create_connection(('{host}', {port}), 5)\"")

    def test_the_operator_cannot_read_a_node_token(self):
        with self.assertRaises(WorldError):
            self.world.ssh("node1", "su - tester -c "
                                    "'cat /var/lib/astrald/config/apphost.yaml'")

    def test_the_operator_has_its_tools(self):
        out = self.world.ssh("node1", "su - tester -c "
                                      "'command -v qwen astral-query'")
        self.assertIn("qwen", out)
        self.assertIn("astral-query", out)

    def test_the_operator_reaches_node1_tokenless(self):
        # why user.info: a token-less session reaches only the gate's list,
        # and the operator's astral-query holds no token
        self.world.ssh("node1", "su - tester -c 'astral-query user.info'")


    def test_the_operator_drives_node1_and_its_facts_come_back(self):
        # why a stub qwen: this checks the seat — push, su, prompt, facts
        # file, collection — not the model, which needs the network
        stub = ("#!/bin/sh\nmkdir -p ~/.facts\n"
                "printf '{\"answer\": \"42\"}' > ~/.facts/stubbed.json\n"
                "echo stub ran\n")
        self.world.ssh("node1", "set -e; for d in $(su - tester -c "
                       "'dirname $(command -v qwen)'); do "
                       f"rm -f $d/qwen; printf '%s' {shlex.quote(stub)} > $d/qwen; "
                       "chmod 755 $d/qwen; done")
        with tempfile.TemporaryDirectory() as tmp:
            d = Path(tmp) / "stubbed"
            d.mkdir()
            (d / "prompt.md").write_text("answer the question\n")
            art = Path(tmp) / "art"
            art.mkdir()
            (art / "session.json").write_text(json.dumps(
                {"run": "x", "nodes": {}, "facts": {}}))
            t = SimpleNamespace(name="stubbed", dir=d, facts=["answer"])
            rc = agent.Operator(self.world.ssh).run(t, art)
            log = (art / "driver.log").read_text()
            self.assertEqual(rc, 0, log)
            self.assertIn("stub ran", log)
            self.assertEqual(json.loads((art / "facts.json").read_text()),
                             {"answer": "42"})

@unittest.skipUnless(WANTED, "ASTRAL_TESTS_DOCKER=1 runs the live Docker tests")
@unittest.skipIf(WHY, f"no Docker engine: {WHY}")
class TestDockerCleanup(unittest.TestCase):
    def test_a_closed_world_leaves_no_container_or_network(self):
        cfg = tomllib.loads((TESTS / "config.toml").read_text())
        lease = lease_ports(**cfg["ports"])
        try:
            binary = linux_binaries(TESTS.parent, docker.engine_arch())
            tmp = Path(tempfile.mkdtemp())
            stamp = "20990101T000001Z"
            w = docker.DockerWorld(tmp / stamp / "w0", binary,
                                   docker.ensure_image(), lease.base,
                                   max_nodes=cfg["ports"]["span"]
                                   // PORTS_PER_NODE)
            w.ensure(SimpleNamespace(name="t", nodes=["node1"], ops=[]))
            w.close(keep=False)
        finally:
            lease.lock.close()
        left = docker._docker("ps", "-a", "--filter",
                              f"label={docker.LABEL}={stamp}", "-q")
        nets = docker._docker("network", "ls", "--filter",
                              f"label={docker.LABEL}={stamp}", "-q")
        self.assertEqual((left, nets), ("", ""))
        self.assertTrue((tmp / stamp / "w0" / "logs" / "node1.log").is_file())


if __name__ == "__main__":
    unittest.main()
