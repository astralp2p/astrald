import tempfile
import unittest
from pathlib import Path

from lib.nodeconfig import NodePorts, ports_for, render


class TestNodeConfig(unittest.TestCase):
    def test_ports_for(self):
        p = ports_for(20800, 1)
        self.assertEqual((p.apphost, p.tcp, p.ether, p.kcp),
                         (20810, 20811, 20812, 20813))

    def test_render(self):
        with tempfile.TemporaryDirectory() as tmp:
            render(Path(tmp), NodePorts(20800, 20801, 20802, 20803),
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


if __name__ == "__main__":
    unittest.main()
