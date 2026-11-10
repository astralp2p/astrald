import errno
import json
import socket
import tempfile
import tomllib
import unittest
from pathlib import Path

from lib import nodeconfig
from lib.manifest import load_all
from lib.nodeconfig import (AUTH_ACTIONS, E2E_APPHOST_POLICIES, E2E_GATE,
                            E2E_USER, NODE_OP_TOOLS, PEER_ALIAS,
                            PEER_TOOLS, NodePorts, PortsBusy, lease_ports,
                            module_configs, ports_for, render, serves_mcp)

# why outside config.toml's lease range: a real run on this host may hold
# any of its spans
LEASE_BASE = 25800
LEASE_END = LEASE_BASE + 30     # the end of the spans these tests lease
PORTS = NodePorts(20800, 20801, 20802, 20803, 20804, 20805)


def rendered(overrides=None) -> dict:
    """module -> the config a node reads back from its rendered root."""
    with tempfile.TemporaryDirectory() as tmp:
        render(Path(tmp), module_configs(PORTS, "sekrit", overrides))
        return {f.stem: json.loads(f.read_text())
                for f in (Path(tmp) / "config").glob("*.yaml")}


class TestNodeConfig(unittest.TestCase):
    def test_ports_for(self):
        p = ports_for(20800, 1)
        self.assertEqual((p.apphost, p.tcp, p.ether, p.kcp, p.mcp, p.authority),
                         (20810, 20811, 20812, 20813, 20814, 20815))

    def test_render(self):
        cfg = rendered()
        self.assertEqual(cfg["apphost"], {
            "listen": ["tcp:127.0.0.1:20800"],     # no socket-path collisions
            "bind_http": "", "tokens": {"sekrit": "localnode"},
            "gate": E2E_GATE, "policies": E2E_APPHOST_POLICIES})
        self.assertEqual(cfg["user"], E2E_USER)
        self.assertEqual(cfg["tcp"], {"listen_port": 20801})
        self.assertEqual(cfg["ether"], {"udp_port": 20802})
        self.assertEqual(cfg["kcp"], {"listen_port": 20803})
        self.assertEqual(cfg["objects"],
                         {"external_registration_sweep_interval": "1s"})
        mcp = cfg["mcp"]
        self.assertEqual(mcp["bind_mcp"], "tcp:127.0.0.1:20804")
        queries = {t["name"]: t["query"] for t in mcp["tools"]}
        # every declared tool puts a node operation to this node, and every
        # peer tool puts its path to the node the alias names
        self.assertEqual(queries, {
            **{name: f"astral://localnode:{path}"
               for path, name in NODE_OP_TOOLS.items()},
            **{name: f"astral://{PEER_ALIAS}:{path}"
               for path, name in PEER_TOOLS.items()}})
        authorizer, = cfg["auth"]["external_authorizers"]
        self.assertEqual(authorizer["endpoint"],
                         "http://127.0.0.1:20805/authorize")
        # the authority answers mail and delegated reads, nothing else; a node
        # hosts a mailbox under its identity's contract, never under an
        # external authority's word
        self.assertEqual(authorizer["actions"], [
            "mod.messaging.send_action", "mod.messaging.receive_action",
            "mod.messaging.read_mailbox_action"])
        self.assertEqual(authorizer["actions"], AUTH_ACTIONS)

    def test_an_override_replaces_its_keys_and_keeps_the_rest(self):
        cfg = rendered({"mcp": {"bind_mcp": ""},
                        "objects": {"extra": [1, "two"]},
                        "gateway": {"gateways": ["x"]}})
        self.assertEqual(cfg["mcp"]["bind_mcp"], "")
        self.assertEqual(len(cfg["mcp"]["tools"]),
                         len(NODE_OP_TOOLS) + len(PEER_TOOLS))
        self.assertEqual(cfg["objects"], {
            "external_registration_sweep_interval": "1s",
            "extra": [1, "two"]})
        # a module the harness renders nothing for gets the override alone
        self.assertEqual(cfg["gateway"], {"gateways": ["x"]})

    def test_a_value_outside_the_bmp_is_written_as_itself(self):
        # why: an escaped one is a surrogate pair, which yaml.v2 refuses, and
        # astrald then stops the node at startup
        with tempfile.TemporaryDirectory() as tmp:
            render(Path(tmp), {"objects": {"label": "probe \U0001F680"}})
            raw = (Path(tmp) / "config" / "objects.yaml").read_bytes()
        self.assertIn("\U0001F680".encode(), raw)
        self.assertNotIn(b"\\ud83d", raw)
        self.assertEqual(json.loads(raw)["label"], "probe \U0001F680")

    def test_a_character_yaml_refuses_or_folds_raw_is_escaped(self):
        # why: yaml.v2 refuses a raw DEL or C1 control, which stops the node,
        # and folds a raw NEL or U+2028 into a value no test set
        label = "a\x7fb\x85c\x9fd\u2028e\u2029f\ufffeg\uffffh é"
        text = nodeconfig.config_text({"label": label})
        self.assertEqual(json.loads(text), {"label": label})
        for c in "\x7f\x85\x9f\u2028\u2029\ufffe\uffff":
            self.assertNotIn(c, text)
        self.assertIn("\\u007fb\\u0085c\\u009fd\\u2028e\\u2029f\\ufffeg"
                      "\\uffffh é", text)

    def test_the_mcp_config_decides_whether_a_node_serves_mcp(self):
        self.assertTrue(serves_mcp(module_configs(PORTS, "t")))
        self.assertFalse(serves_mcp(module_configs(
            PORTS, "t", {"mcp": {"bind_mcp": ""}})))

    def test_no_roster_name_decides_a_nodes_config(self):
        # why: a node's config comes from its world's overrides, and a name
        # listed here would quietly configure whichever test started it
        source = Path(nodeconfig.__file__).read_text()
        self.assertNotIn("nomcp1", source)
        self.assertNotIn("WITHOUT_MCP", source)

    def test_the_leases_here_lie_outside_the_runs_lease_range(self):
        ports = tomllib.loads((Path(nodeconfig.__file__).parent.parent
                               / "config.toml").read_text())["ports"]
        end = ports["base"] + ports["span"] * ports["spans"]
        self.assertTrue(LEASE_END <= ports["base"] or LEASE_BASE >= end,
                        f"[{LEASE_BASE}, {LEASE_END}) overlaps "
                        f"[{ports['base']}, {end})")

    def test_a_span_gives_node_n_its_block_and_refuses_past_its_size(self):
        span = nodeconfig.PortSpan(20800, 2)
        self.assertEqual(span.ports(1), ports_for(20800, 1))
        self.assertIsNone(span.refusal(1, ["node2"]))
        self.assertEqual(span.refusal(1, ["node2", "node3"]),
                         "node2, node3: the world already runs 1 nodes, and "
                         "[ports] span in tests/config.toml gives a world 2")

    def test_an_overlay_writes_keys_over_a_copy(self):
        base = {"user": {"a": 1, "b": 2}}
        out = nodeconfig.overlay(base, {"user": {"b": 3}, "new": {"c": 4}})
        self.assertEqual(out, {"user": {"a": 1, "b": 3}, "new": {"c": 4}})
        self.assertEqual(base, {"user": {"a": 1, "b": 2}})

    def test_an_empty_gate_and_policies_override_leaves_astrald_its_defaults(self):
        # why: distribution-gate-setup starts a node under the gate and the app
        # register policy a released astrald ships. An empty mapping replaces
        # the harness's keys, and astrald loads a config over its code defaults
        # (mod/apphost/src/config_gate_test.go holds the other half), so the
        # node's own lists stand; the harness's token is not part of the gate
        shipped = load_all(Path(nodeconfig.__file__).parent.parent / "e2e")
        override = shipped["distribution-gate-setup"].config["node1"]
        cfg = rendered(override)["apphost"]
        self.assertEqual((cfg["gate"], cfg["policies"]), ({}, {}))
        self.assertEqual(cfg["tokens"], {"sekrit": "localnode"})
        self.assertEqual(rendered()["apphost"]["gate"], E2E_GATE)

    def test_lease_ports_skips_a_leased_span(self):
        first = lease_ports(LEASE_BASE, span=10, spans=3)
        second = lease_ports(LEASE_BASE, span=10, spans=3)
        try:
            self.assertNotEqual(first.base, second.base)
        finally:
            first.lock.close()
            second.lock.close()

    def test_lease_ports_skips_a_bound_span(self):
        # why these holders: what a run's nodes bind — 127.0.0.1 for apphost,
        # mcp and the authority, Go's dual-stack [::] for tcp, ether and kcp
        # — and a foreign 0.0.0.0. macOS read a 127.0.0.1 one as free.
        holders = [("tcp 127.0.0.1", socket.AF_INET, socket.SOCK_STREAM,
                    "127.0.0.1"),
                   ("tcp 0.0.0.0", socket.AF_INET, socket.SOCK_STREAM,
                    "0.0.0.0"),
                   ("tcp [::]", socket.AF_INET6, socket.SOCK_STREAM, "::"),
                   ("udp 127.0.0.1", socket.AF_INET, socket.SOCK_DGRAM,
                    "127.0.0.1"),
                   ("udp [::]", socket.AF_INET6, socket.SOCK_DGRAM, "::")]
        for name, family, kind, addr in holders:
            with self.subTest(holder=name):
                if family == socket.AF_INET6 and not socket.has_ipv6:
                    self.skipTest(f"{name}: no IPv6 on this host")
                with socket.socket(family, kind) as holder:
                    if family == socket.AF_INET6:
                        holder.setsockopt(socket.IPPROTO_IPV6,
                                          socket.IPV6_V6ONLY, 0)
                    try:
                        holder.bind((addr, LEASE_BASE + 4))
                    except OSError as e:
                        if e.errno != errno.EADDRNOTAVAIL:
                            raise
                        self.skipTest(f"{name}: {e}")
                    if kind == socket.SOCK_STREAM:
                        holder.listen()
                    lease = lease_ports(LEASE_BASE, span=10, spans=3)
                    lease.lock.close()
                self.assertEqual(lease.base, LEASE_BASE + 10)

    def test_lease_ports_reads_a_time_wait_port_free(self):
        # why: astrald binds over its own TIME_WAIT (Go sets SO_REUSEADDR),
        # so a span a finished run just left must lease again
        port = LEASE_BASE + 6
        with socket.socket() as listener:
            listener.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
            listener.bind(("127.0.0.1", port))
            listener.listen()
            with socket.create_connection(("127.0.0.1", port)) as client:
                accepted, _ = listener.accept()
                accepted.close()        # the server side closes first
                client.recv(1)          # its FIN has arrived
        with socket.socket() as plain, self.assertRaises(OSError):
            plain.bind(("127.0.0.1", port))     # the port is held: TIME_WAIT
        lease = lease_ports(LEASE_BASE, span=10, spans=3)
        lease.lock.close()
        self.assertEqual(lease.base, LEASE_BASE)

    def test_lease_ports_refuses_when_every_span_is_leased(self):
        held = lease_ports(LEASE_BASE, span=10, spans=1)
        try:
            with self.assertRaises(PortsBusy):
                lease_ports(LEASE_BASE, span=10, spans=1)
        finally:
            held.lock.close()


if __name__ == "__main__":
    unittest.main()
