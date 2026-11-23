"""lib/meshapps.py and lib/kcponly.py: the pure functions, on synthetic states
and call logs, with no daemon and no app process."""
import tempfile
import unittest
from pathlib import Path

from lib import kcponly, meshapps

NAMES = ["node1", "node2", "node3"]
APP = {"node1": "02aa", "node2": "02bb", "node3": "02cc"}


def call(start, end, outcome="ok", code=0):
    return {"start": start, "end": end, "outcome": outcome, "code": code,
            "ms": (end - start) * 1000}


def peer(first=None, last_ok=None, last_fail=None):
    return {"reported_at": 1.0, "target": False, "listed": True, "removed": [],
            "restored": [], "first": first, "calls": 1, "ok": int(bool(last_ok)),
            "by_outcome": {}, "last_ok": last_ok, "last_fail": last_fail,
            "inflight_since": None, "max_ms": 0}


def mesh(providers: dict) -> dict:
    """States of the three apps; providers is {(c, p): peer}."""
    out = {}
    for n in NAMES:
        out[n] = {"app": APP[n], "providers": {}, "probes": {}}
    for (c, p), rec in providers.items():
        out[c]["providers"][APP[p]] = rec
    return out


def log(*entries):
    return [{"t0": t0, "t1": t1, "to": to, "kind": "app", "outcome": o,
             "code": 0, "ms": (t1 - t0) * 1000} for t0, t1, to, o in entries]


class Argv(unittest.TestCase):
    def test_serve_argv_carries_every_flag(self):
        self.assertEqual(
            meshapps.serve_argv("f2", period=0, keep_removed=True,
                                targets=["02aa", "02bb"], probe_nodes=["03cc"]),
            ["serve", "-service", "f2", "-period", "0s", "-keep-removed",
             "-targets", "02aa,02bb", "-probe-nodes", "03cc"])

    def test_serve_argv_leaves_unset_flags_out(self):
        self.assertEqual(meshapps.serve_argv("f3", period=1.0, keep_removed=False),
                         ["serve", "-service", "f3", "-period", "1.0s"])

    def test_per_app_takes_a_list_or_a_dict(self):
        self.assertEqual(meshapps._per_app(None, "node1"), [])
        self.assertEqual(meshapps._per_app(["a"], "node1"), ["a"])
        self.assertEqual(meshapps._per_app({"node1": ["a"]}, "node1"), ["a"])
        self.assertEqual(meshapps._per_app({"node1": ["a"]}, "node2"), [])

    def test_node_env_points_at_the_node(self):
        node = {"endpoint": "tcp:127.0.0.1:1", "token": "t", "host_ip": "127.0.0.1"}
        self.assertEqual(meshapps.node_env(node),
                         {"APP_NODE_ENDPOINT": "tcp:127.0.0.1:1",
                          "APP_NODE_TOKEN": "t", "APP_HOST_IP": "127.0.0.1"})
        self.assertEqual(meshapps.node_env({**node, "host_ip": None}, "u"),
                         {"APP_NODE_ENDPOINT": "tcp:127.0.0.1:1", "APP_NODE_TOKEN": "u"})


class Ready(unittest.TestCase):
    def test_ready_needs_registration_advertise_and_a_boundary(self):
        base = {"registered_at": 1.0, "advertised_at": 1.5, "follow_opens": 1}
        self.assertTrue(meshapps.ready(base))
        self.assertFalse(meshapps.ready({**base, "advertised_at": None}))
        self.assertFalse(meshapps.ready({**base, "registered_at": None}))
        self.assertFalse(meshapps.ready({**base, "follow_opens": 0}))
        self.assertFalse(meshapps.ready({}))

    def test_a_refused_follow_is_settled(self):
        # why: a stranger's follow may be refused before its boundary, and
        # its app still calls its targets
        self.assertTrue(meshapps.ready({"registered_at": 1.0, "advertised_at": 1.5,
                                        "follow_opens": 0, "follow_err": "denied"}))


class Pairs(unittest.TestCase):
    def test_ids_and_pairs(self):
        st = mesh({})
        self.assertEqual(meshapps.ids(st), APP)
        self.assertEqual(len(meshapps.pairs(NAMES)), 6)
        self.assertNotIn(("node1", "node1"), meshapps.pairs(NAMES))

    def test_first_calls_names_a_pair_never_seen_as_none(self):
        first = call(10, 10.2)
        st = mesh({("node1", "node2"): peer(first=first, last_ok=first)})
        got = meshapps.first_calls(st, NAMES)
        self.assertEqual(got[("node1", "node2")], first)
        self.assertIsNone(got[("node2", "node1")])
        self.assertEqual(len(got), 6)

    def test_answering_since_is_the_last_ok_ending_after_t(self):
        st = mesh({(c, p): peer(last_ok=call(19, 20.5)) for c, p in meshapps.pairs(NAMES)})
        st["node3"]["providers"][APP["node1"]] = peer(last_ok=call(19, 20.0),
                                                      last_fail=call(21, 22, "unavailable", 7))
        del st["node2"]["providers"][APP["node3"]]
        self.assertEqual(meshapps.answering_since(st, NAMES, 20.0),
                         [("node2", "node3"), ("node3", "node1")])
        self.assertEqual(meshapps.answering_since(st, NAMES, 10.0), [("node2", "node3")])


class CallLogs(unittest.TestCase):
    def test_first_ok_after_skips_failures_and_other_targets(self):
        calls = log((1, 2, "02bb", "ok"), (3, 4, "02bb", "timeout"),
                    (5, 6, "02cc", "ok"), (7, 8, "02bb", "ok"), (9, 10, "02bb", "ok"))
        self.assertEqual(meshapps.first_ok_after(calls, "02bb", 2.5), 8)
        self.assertEqual(meshapps.first_ok_after(calls, "02bb", 1.5), 2)
        self.assertIsNone(meshapps.first_ok_after(calls, "02bb", 10))

    def test_recovered_at_is_the_latest_pair(self):
        st = mesh({})
        logs = {"node1": log((5, 6, "02bb", "ok"), (5, 7, "02cc", "ok")),
                "node2": log((4, 4.5, "02aa", "ok"), (5, 9, "02cc", "ok")),
                "node3": log((5, 5.5, "02aa", "ok"), (5, 6.5, "02bb", "ok"))}
        self.assertEqual(meshapps.recovered_at(st, logs, NAMES, 4.0), 9)
        logs["node2"] = log((4, 4.5, "02aa", "ok"), (5, 9, "02cc", "unavailable"))
        self.assertIsNone(meshapps.recovered_at(st, logs, NAMES, 4.0))

    def test_window_holds_the_calls_started_in_it(self):
        calls = log((1, 2, "02bb", "ok"), (2, 30, "02bb", "timeout"),
                    (3, 4, "02cc", "ok"), (5, 6, "02bb", "ok"))
        self.assertEqual([e["t0"] for e in meshapps.window(calls, "02bb", 2, 5)], [2])
        self.assertEqual([e["t0"] for e in meshapps.window(calls, "02bb", 0, 10)], [1, 2, 5])

    def test_read_calls_skips_a_line_caught_mid_write(self):
        with tempfile.TemporaryDirectory() as d:
            path = Path(d) / "a.state.json.calls.jsonl"
            path.write_text('{"t0": 1, "t1": 2, "to": "02bb", "outcome": "ok"}\n{"t0": 3, "t1"')
            self.assertEqual([e["t0"] for e in meshapps.read_calls(path)], [1])
            self.assertEqual(meshapps.read_calls(Path(d) / "absent"), [])


class KcpOnly(unittest.TestCase):
    IDS = {"node1": "02aa", "node2": "02bb", "node3": "02cc"}

    def rows(self, network="kcp"):
        return {a: [{"id": f"{a}{b}", "remote": self.IDS[b], "network": network}
                    for b in NAMES if b != a] for a in NAMES}

    def test_pairs_put_the_lower_name_first(self):
        self.assertEqual(kcponly.pairs(["node3", "node1", "node2"]),
                         [("node1", "node2"), ("node1", "node3"), ("node2", "node3")])

    def test_kcp_only_needs_a_kcp_link_to_every_peer(self):
        view = self.rows()
        self.assertTrue(kcponly.kcp_only(view, NAMES, self.IDS))
        view["node2"].append({"id": "x", "remote": "02cc", "network": "tcp"})
        self.assertFalse(kcponly.kcp_only(view, NAMES, self.IDS))
        view = self.rows()
        view["node3"] = [r for r in view["node3"] if r["remote"] != "02aa"]
        self.assertFalse(kcponly.kcp_only(view, NAMES, self.IDS))

    def test_lan_ip(self):
        self.assertEqual(kcponly.lan_ip({"lan_endpoint": "tcp:127.0.0.1:1791"}), "127.0.0.1")


if __name__ == "__main__":
    unittest.main()
