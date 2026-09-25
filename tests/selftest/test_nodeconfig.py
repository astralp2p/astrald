import socket
import tempfile
import unittest
from pathlib import Path

from lib.nodeconfig import (WITHOUT_MCP, NodePorts, PortsBusy, lease_ports,
                            ports_for, render)

# why not the config base: a real run on this host may hold 20800's spans
LEASE_BASE = 23800


class TestNodeConfig(unittest.TestCase):
    def test_ports_for(self):
        p = ports_for(20800, 1)
        self.assertEqual((p.apphost, p.tcp, p.ether, p.kcp, p.mcp, p.authority),
                         (20810, 20811, 20812, 20813, 20814, 20815))

    def test_render(self):
        with tempfile.TemporaryDirectory() as tmp:
            render(Path(tmp), NodePorts(20800, 20801, 20802, 20803, 20804,
                                        20805),
                   token="sekrit")
            cfg = Path(tmp) / "config"
            apphost = (cfg / "apphost.yaml").read_text()
            self.assertIn('- "tcp:127.0.0.1:20800"', apphost)
            self.assertIn('bind_http: ""', apphost)
            self.assertIn('"sekrit": localnode', apphost)
            self.assertNotIn("unix:", apphost)      # no socket-path collisions
            self.assertIn("listen_port: 20801", (cfg / "tcp.yaml").read_text())
            self.assertIn("udp_port: 20802", (cfg / "ether.yaml").read_text())
            self.assertIn("listen_port: 20803", (cfg / "kcp.yaml").read_text())
            self.assertIn('bind_mcp: "tcp:127.0.0.1:20804"',
                          (cfg / "mcp.yaml").read_text())
            auth = (cfg / "auth.yaml").read_text()
            self.assertIn('endpoint: "http://127.0.0.1:20805/authorize"', auth)
            self.assertIn("- mod.mcp.call_agent_action", auth)
            self.assertIn("- mod.messaging.send_action", auth)
            self.assertIn("- mod.messaging.receive_action", auth)
            # a node hosts a mailbox under its identity's contract, never
            # under an external authority's word
            self.assertNotIn("host_mailbox_action", auth)
            self.assertNotIn("answer_agent_action", auth)

    def test_render_without_mcp(self):
        with tempfile.TemporaryDirectory() as tmp:
            render(Path(tmp), NodePorts(20800, 20801, 20802, 20803, 20804,
                                        20805),
                   token="sekrit", serves_mcp=False)
            mcp = (Path(tmp) / "config" / "mcp.yaml").read_text()
            self.assertIn('bind_mcp: ""', mcp)
            self.assertNotIn("20804", mcp)

    def test_a_roster_name_decides_mcp(self):
        self.assertIn("nomcp1", WITHOUT_MCP)
        self.assertNotIn("node1", WITHOUT_MCP)
        self.assertNotIn("node2", WITHOUT_MCP)

    def test_lease_ports_skips_a_leased_span(self):
        first = lease_ports(LEASE_BASE, span=10, spans=3)
        second = lease_ports(LEASE_BASE, span=10, spans=3)
        try:
            self.assertNotEqual(first.base, second.base)
        finally:
            first.lock.close()
            second.lock.close()

    def test_lease_ports_skips_a_bound_span(self):
        with socket.socket() as listener:
            listener.bind(("127.0.0.1", LEASE_BASE + 4))
            listener.listen()
            lease = lease_ports(LEASE_BASE, span=10, spans=3)
            lease.lock.close()
        self.assertEqual(lease.base, LEASE_BASE + 10)

    def test_lease_ports_refuses_when_every_span_is_leased(self):
        held = lease_ports(LEASE_BASE, span=10, spans=1)
        try:
            with self.assertRaises(PortsBusy):
                lease_ports(LEASE_BASE, span=10, spans=1)
        finally:
            held.lock.close()


if __name__ == "__main__":
    unittest.main()
