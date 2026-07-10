"""Offline tests for astralapi -- no VM, no live astrald.

Exercises the interrogators against synthetic AstralObjects, parse_cli's
stream handling, and the Go-CLI fallback command construction. Run with:

    python3 -m unittest -v        # from this directory
"""
import os
import sys
import unittest

sys.path.insert(0, os.path.dirname(os.path.realpath(__file__)))
import astralapi  # noqa: E402  (also bootstraps astral onto sys.path)
import astral  # noqa: E402


def O(type, value=None):
    return astral.obj(type, value)


class InterrogatorTests(unittest.TestCase):
    def test_contract(self):
        objs = [O("mod.user.contract",
                  {"Contract": {"Contract": {"Issuer": "02aa", "Subject": "03bb"}}})]
        self.assertEqual(astralapi.contract(objs), ("02aa", "03bb"))
        self.assertEqual(astralapi.contract([O("x", {})]), (None, None))

    def test_linked_sibling_and_identities(self):
        objs = [O("s", {"Identity": "03bb", "Linked": True}),
                O("s", {"Identity": "03cc", "Linked": False})]
        self.assertEqual(astralapi.linked_sibling(objs), "03bb")
        self.assertEqual(astralapi.swarm_identities(objs), {"03bb", "03cc"})
        self.assertIsNone(astralapi.linked_sibling([O("s", {"Identity": "03cc", "Linked": False})]))

    def test_has_link_to(self):
        objs = [O("l", {"RemoteIdentity": "03bb", "Network": "tcp"})]
        self.assertTrue(astralapi.has_link_to(objs, "03bb"))
        self.assertFalse(astralapi.has_link_to(objs, "03cc"))

    def test_is_expelled_nested(self):
        objs = [O("mod.user.signed_expulsion", {"Expulsion": {"Subject": "03bb"}})]
        self.assertTrue(astralapi.is_expelled(objs, "03bb"))
        self.assertFalse(astralapi.is_expelled(objs, "03cc"))
        # an error_message naming the id must not count as an expulsion record
        self.assertFalse(astralapi.is_expelled([O("error_message", "03bb not found")], "03bb"))

    def test_loaded_payload_and_errors(self):
        objs = [O("error_message", "boom"), O("string8", "hello")]
        self.assertEqual(astralapi.loaded_payload(objs), "hello")
        self.assertEqual(astralapi.error_messages(objs), ["boom"])
        self.assertIsNone(astralapi.loaded_payload([O("error_message", "boom")]))

    def test_tor_links_and_endpoint(self):
        objs = [O("l", {"Network": "tor", "RemoteIdentity": "03bb",
                        "RemoteEndpoint": {"Object": "abc.onion:1791"}}),
                O("l", {"Network": "tcp", "RemoteIdentity": "03cc"})]
        self.assertEqual(astralapi.tor_links(objs), [("03bb", "abc.onion:1791")])
        self.assertEqual(astralapi.endpoint_addr("x.onion"), "x.onion")
        self.assertEqual(astralapi.endpoint_addr({"Object": "y.onion"}), "y.onion")
        self.assertEqual(astralapi.endpoint_addr(None), "")

    def test_resolve_onion(self):
        objs = [O("e", {"Endpoint": "10.0.0.1:1791"}),
                O("e", {"Endpoint": {"Object": "abc.onion:1791"}})]
        self.assertEqual(astralapi.resolve_onion(objs), "abc.onion:1791")
        self.assertIsNone(astralapi.resolve_onion([O("e", {"Endpoint": "10.0.0.1:1791"})]))


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
