"""Offline tests for astralapi -- no VM, no live astrald.

Exercises the typed accessors (astral-py records over the CLI path), the
stream utilities, parse_cli's stream handling, and the Go-CLI fallback command
construction. Run with:

    python3 -m unittest -v        # from this directory
"""
import json
import os
import sys
import unittest

sys.path.insert(0, os.path.dirname(os.path.realpath(__file__)))
import astralapi  # noqa: E402  (also bootstraps astral onto sys.path)
import astral  # noqa: E402


def O(type, value=None):
    return astral.obj(type, value)


def lines(*objs):
    """Render (type, value) pairs as astral-query -out json output."""
    out = [json.dumps({"Type": t, "Object": v}) for t, v in objs]
    out.append(json.dumps({"Type": "eos", "Object": None}))
    return "\n".join(out) + "\n"


class FakeSsh:
    """Patches astralapi.ssh with per-op canned CLI output."""

    def __init__(self, by_op):
        self.by_op = by_op
        self.calls = []

    def __enter__(self):
        self._orig = astralapi.ssh
        astralapi.ssh = self
        return self

    def __exit__(self, *exc):
        astralapi.ssh = self._orig
        return False

    def __call__(self, vm, remote):
        self.calls.append((vm, remote))
        for op, raw in self.by_op.items():
            if op in remote:
                return raw
        return ""


class TypedAccessorTests(unittest.TestCase):
    """Node's typed accessors decode records identically over the CLI path."""

    def test_user_info(self):
        raw = lines(("mod.user.info", {
            "NodeAlias": "node1", "UserAlias": "alice",
            "ContractID": "data1c",
            "Contract": {"Contract": {"Issuer": "02aa", "Subject": "03bb"}}}))
        with FakeSsh({"user.info": raw}):
            info = astralapi.Node("node1", None, "").user_info()
        self.assertEqual(info.contract_issuer, "02aa")
        self.assertEqual(info.contract_subject, "03bb")
        self.assertEqual(info.node_alias, "node1")

    def test_user_info_none_on_reject(self):
        with FakeSsh({"user.info": ""}):
            self.assertIsNone(astralapi.Node("node1", None, "").user_info())

    def test_swarm_members(self):
        raw = lines(("mod.users.swarm_member", {"Identity": "03bb", "Linked": True}),
                    ("mod.users.swarm_member", {"Identity": "03cc", "Linked": False}))
        with FakeSsh({"user.swarm_status": raw}):
            members = astralapi.Node("node1", None, "").swarm_members()
        self.assertEqual([m.identity for m in members], ["03bb", "03cc"])
        linked = next((m.identity for m in members if m.linked), None)
        self.assertEqual(linked, "03bb")

    def test_links(self):
        raw = lines(("mod.nodes.link_info",
                     {"Network": "tor", "RemoteIdentity": "03bb",
                      "RemoteEndpoint": {"Object": "abc.onion:1791"}}),
                    ("mod.nodes.link_info",
                     {"Network": "tcp", "RemoteIdentity": "03cc"}))
        with FakeSsh({"nodes.links": raw}):
            links = astralapi.Node("node1", None, "").links()
        tor = [l for l in links if l.network == "tor"]
        self.assertEqual(tor[0].remote_identity, "03bb")
        self.assertEqual(tor[0].remote_address, "abc.onion:1791")
        self.assertTrue(any(l.remote_identity == "03cc" for l in links))

    def test_endpoints(self):
        raw = lines(("mod.nodes.endpoint_with_ttl", {"Endpoint": "10.0.0.1:1791"}),
                    ("mod.nodes.endpoint_with_ttl",
                     {"Endpoint": {"Object": "abc.onion:1791"}}))
        with FakeSsh({"nodes.resolve_endpoints": raw}):
            eps = astralapi.Node("node1", None, "").endpoints("localnode")
        onion = next((e.address for e in eps if ".onion" in e.address), None)
        self.assertEqual(onion, "abc.onion:1791")

    def test_expulsions_nested_subject(self):
        raw = lines(("mod.user.signed_expulsion",
                     {"Expulsion": {"Subject": "03bb"}}))
        with FakeSsh({"user.list_expelled": raw}):
            bans = astralapi.Node("node1", None, "").expulsions()
        self.assertTrue(any(b.bans("03bb") for b in bans))
        self.assertFalse(any(b.bans("03cc") for b in bans))

    def test_records_skip_errors_and_scalars(self):
        objs = [O("error_message", "boom"), O("string8", "hi"),
                O("mod.nodes.link_info", {"Network": "tcp"})]
        links = astralapi.records(objs, astralapi.LinkInfo)
        self.assertEqual(len(links), 1)
        self.assertEqual(links[0].network, "tcp")


class StreamUtilityTests(unittest.TestCase):
    def test_loaded_payload_and_errors(self):
        objs = [O("error_message", "boom"), O("string8", "hello")]
        self.assertEqual(astralapi.loaded_payload(objs), "hello")
        self.assertEqual(astralapi.error_messages(objs), ["boom"])
        self.assertIsNone(astralapi.loaded_payload([O("error_message", "boom")]))


class ParseCliTests(unittest.TestCase):
    def test_drops_eos_keeps_error(self):
        raw = ('{"Type":"string8","Object":"hi"}\n'
               '{"Type":"error_message","Object":"nope"}\n'
               '\n'
               'not-json\n'
               '{"Type":"eos","Object":null}\n')
        objs = astralapi.parse_cli(raw)
        self.assertEqual([o.type for o in objs], ["string8", "error_message"])
        self.assertEqual(astralapi.loaded_payload(objs), "hi")
        self.assertEqual(astralapi.error_messages(objs), ["nope"])

    def test_empty(self):
        self.assertEqual(astralapi.parse_cli(""), [])
        self.assertEqual(astralapi.parse_cli(None), [])


class ShellRoutingTests(unittest.TestCase):
    """Node with no client must build the exact Go astral-query command."""

    def setUp(self):
        self.calls = []
        self._orig = astralapi.ssh

        def fake_ssh(vm, remote):
            self.calls.append((vm, remote))
            return '{"Type":"string8","Object":"hi"}\n{"Type":"eos","Object":null}\n'

        astralapi.ssh = fake_ssh

    def tearDown(self):
        astralapi.ssh = self._orig

    def test_untokened(self):
        node = astralapi.Node("node1", None, "")
        objs = node.call("user.info")
        self.assertEqual(self.calls[-1], ("node1", "astral-query user.info -out json"))
        self.assertEqual(astralapi.loaded_payload(objs), "hi")

    def test_tokened_with_args(self):
        astralapi.Node("node1", None, "TKN").call("objects.load", {"id": "X", "repo": "local"})
        self.assertEqual(
            self.calls[-1][1],
            "export ASTRALD_APPHOST_TOKEN=TKN; "
            "astral-query objects.load -id X -repo local -out json")

    def test_peer_target(self):
        astralapi.Node("node1", None, "TKN").call("objects.load", {"id": "X"}, target="node2")
        self.assertEqual(
            self.calls[-1][1],
            "export ASTRALD_APPHOST_TOKEN=TKN; "
            "astral-query node2:objects.load -id X -out json")

    def test_arg_value_is_shell_quoted(self):
        import shlex
        v = "a b'c"  # a value with a space and a quote
        astralapi.Node("node1", None, "").call("objects.load", {"id": v})
        self.assertIn(f"-id {shlex.quote(v)}", self.calls[-1][1])

    def test_shell_ops_pin_forces_cli(self):
        # even with a (truthy sentinel) client, a pinned op must go to the shell
        astralapi.SHELL_OPS.add("user.info")
        try:
            node = astralapi.Node("node1", object(), "")
            node.call("user.info")
            self.assertEqual(self.calls[-1][1], "astral-query user.info -out json")
        finally:
            astralapi.SHELL_OPS.discard("user.info")


if __name__ == "__main__":
    unittest.main()
