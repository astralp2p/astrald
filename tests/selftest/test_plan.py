"""The planner, held to its invariants over the shipped manifests.

Every single test, every ordered pair and every suite is planned shared and
isolated, and each plan must keep the rules a world depends on. The suites'
plans are pinned in golden/, so a manifest change that moves a test shows up
as a diff someone reads.
"""
import argparse
import itertools
import tempfile
import tomllib
import unittest
from pathlib import Path
from types import SimpleNamespace
from unittest import mock

from lib import runner
from lib.manifest import load_all
from lib.nodeconfig import PORTS_PER_NODE
from lib.plan import PlanError, chain, plan, render, vm_refusal
from lib.suites import load

TESTS = Path(__file__).resolve().parent.parent
GOLDEN = Path(__file__).resolve().parent / "golden"

# suite -> the driver its golden plan is drawn under
SUITES = {"main": "script", "tor": "script", "nat": "script",
          "gateway": "script", "agent": "agent", "integration": "script",
          "nearby": "script"}


def fake(after=None, destroys=False, ops=(), local_only=None,
         nodes=("node1",), prompt=False, config=None, stop=()):
    return SimpleNamespace(after=after,
                           destroys=destroys or bool(ops) or bool(stop),
                           ops=list(ops), local_only=local_only,
                           nodes=list(nodes), prompt=prompt,
                           config=config or {}, stop=list(stop))


def mcp(bind: str) -> dict:
    """An override that binds node1's MCP server to bind."""
    return {"node1": {"mcp": {"bind_mcp": bind}}}


class Invariants:
    """What every plan of the shipped tree must hold."""

    def check(self, tests, selection, worlds, isolate):
        producers = {t.after for t in tests.values()} - {None}
        ran = [s.test for w in worlds for s in w.steps if s.kind == "test"]
        self.assertEqual(sorted(ran), sorted(set(selection)),
                         "each selected test runs exactly once as a test")
        for w in worlds:
            names = [s.test for s in w.steps]
            self.assertIn("test", [s.kind for s in w.steps],
                          f"{w.name} runs no selected test")
            if isolate:
                self.assertEqual([s.kind for s in w.steps].count("test"), 1)
            for i, n in enumerate(names):
                t = tests[n]
                self.assertTrue(set(chain(tests, n)[:-1]) <= set(names[:i]),
                                f"{w.name}: {n} runs before its lineage")
                if w.steps[i].kind == "prereq":
                    self.assertTrue(any(
                        s.kind == "test" and n in chain(tests, s.test)
                        for s in w.steps[i + 1:]),
                        f"{w.name}: prereq {n} feeds no later test")
                if w.kind == "vm":
                    self.assertIsNone(t.local_only,
                                      f"{w.name}: local-only {n} in a VM")
                if t.ops:
                    self.assertEqual(w.kind, "vm",
                                     f"{w.name}: {n}'s ops outside a VM")
                if t.destroys:
                    for later in names[i + 1:]:
                        self.assertIn(n, chain(tests, later),
                                      f"{w.name}: {later} follows {n}")
                    if t.after is None:
                        for before in names[:i]:
                            b = tests[before]
                            self.assertTrue(
                                b.after is None and before not in producers
                                and not b.destroys,
                                f"{w.name}: {n} meets {before}'s world")
                if n not in producers and not t.destroys:
                    # an observer runs before its parent's producers
                    for before in names[:i]:
                        self.assertFalse(
                            tests[before].after == t.after
                            and before in producers,
                            f"{w.name}: observer {n} after producer {before}")


class TestShippedPlans(Invariants, unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.tests = load_all(TESTS / "e2e")
        cls.suites = {p.stem: load(p) for p in
                      sorted((TESTS / "suites").glob("*.suite"))}

    def selections(self):
        names = sorted(self.tests)
        yield from (([n], ()) for n in names)
        yield from ((list(p), ()) for p in itertools.permutations(names, 2))
        yield from ((s, s) for s in self.suites.values())

    def test_every_selection_keeps_the_invariants(self):
        for isolate in (False, True):
            for selection, order in self.selections():
                with self.subTest(selection=selection, isolate=isolate):
                    worlds = plan(self.tests, selection, order,
                                  isolate=isolate)
                    self.check(self.tests, selection, worlds, isolate)

    def test_every_vm_selection_keeps_the_invariants(self):
        for selection, order in self.selections():
            with self.subTest(selection=selection):
                lineage = {n for s in selection for n in chain(self.tests, s)}
                # why either refusal: a selection may hold a local_only test
                # and one a VM world cannot run, and the first checked names
                # its own reason
                if any(self.tests[n].local_only or vm_refusal(self.tests[n])
                       for n in lineage):
                    with self.assertRaisesRegex(
                            PlanError, "local only|the VM lab does not run"
                            "|a VM world cannot"):
                        plan(self.tests, selection, order, vm=True)
                    continue
                worlds = plan(self.tests, selection, order, vm=True)
                self.assertEqual({w.kind for w in worlds}, {"vm"})
                self.check(self.tests, selection, worlds, False)

    def test_a_roster_off_the_lab_runs_local_or_in_docker_never_in_a_vm(self):
        # why: a VM world can give a test only the lab's machines, while a
        # local or Docker world starts any name it is handed
        for t in ("token-less-setup", "introduce-node", "signer-app"):
            with self.subTest(test=t):
                self.assertFalse(self.tests[t].local_only)
                self.assertEqual({w.kind for w in plan(self.tests, [t])},
                                 {"local"})
                self.assertEqual({w.kind for w in plan(self.tests, [t],
                                                       docker=True)},
                                 {"docker"})
                with self.assertRaisesRegex(PlanError, "does not run"):
                    plan(self.tests, [t], vm=True)
        # a lab roster still enters VMs
        self.assertEqual({w.kind for w in plan(self.tests, ["adopt-node"],
                                               vm=True)}, {"vm"})

    def test_a_stop_runs_local_or_in_docker_never_in_a_vm(self):
        # why: a local and a Docker world stop a node, and a VM world has no
        # stop; the roster here is the lab's, so the stop alone decides
        tests = {"stopper": fake(nodes=("node1", "node2"), stop=["node1"])}
        self.assertEqual({w.kind for w in plan(tests, ["stopper"])},
                         {"local"})
        self.assertEqual({w.kind for w in plan(tests, ["stopper"],
                                               docker=True)}, {"docker"})
        with self.assertRaisesRegex(PlanError, "^stopper stops a node, which "
                                    "a VM world cannot, and this run puts "
                                    "every world in VMs$"):
            plan(tests, ["stopper"], vm=True)

    def test_a_prereq_stranded_by_a_fork_is_dropped(self):
        # why: mcp-origin fixes w0 local, tor-link forks a VM world, and the
        # bootstrap and adopt-node that led to it would feed nothing in w0
        worlds = plan(self.tests, ["mcp-origin", "tor-link"])
        self.assertEqual([[s.test for s in w.steps] for w in worlds],
                         [["mcp-origin"],
                          ["bootstrap-user-software-key", "adopt-node",
                           "tor-link"]])

    def test_golden_suite_plans(self):
        self.assertEqual(set(SUITES), set(self.suites))
        for suite, driver in SUITES.items():
            with self.subTest(suite=suite):
                listed = self.suites[suite]
                got = render(plan(self.tests, listed, listed, driver=driver))
                # why --vm: an agent plan's heavy kind follows the host
                flag = " --driver agent --vm" if driver == "agent" else ""
                self.assertEqual(
                    got, (GOLDEN / f"{suite}.plan").read_text(),
                    f"{suite}.suite plans differently; if intended, run "
                    f"./tests/run --plan{flag} {suite}.suite > "
                    f"tests/selftest/golden/{suite}.plan")

    def test_every_shipped_local_or_docker_world_fits_the_port_span(self):
        # why: a local or Docker world refuses a node past the span only
        # when the run reaches it; this tells the test author at selftest
        # time
        span = tomllib.loads((TESTS / "config.toml")
                             .read_text())["ports"]["span"]
        limit = span // PORTS_PER_NODE
        plans = {f"{suite}.suite": plan(self.tests, self.suites[suite],
                                        self.suites[suite], driver=driver)
                 for suite, driver in SUITES.items()}
        plans["every test"] = plan(self.tests, sorted(self.tests))
        plans["every Docker test"] = plan(
            self.tests, sorted(n for n, t in self.tests.items()
                               if not t.ops and not t.local_only),
            docker=True)
        plans["agent.suite in Docker"] = plan(
            self.tests, self.suites["agent"], self.suites["agent"],
            driver="agent", heavy="docker")
        for name, worlds in plans.items():
            for w in worlds:
                if w.kind not in ("local", "docker"):
                    continue
                nodes = {n for s in w.steps for n in self.tests[s.test].nodes}
                with self.subTest(plan=name, world=w.name):
                    self.assertLessEqual(
                        len(nodes), limit,
                        f"{name} starts {len(nodes)} nodes in {w.name}, and "
                        f"[ports] span in tests/config.toml gives a world "
                        f"{limit}")

    def test_main_suite_is_one_world_and_a_world_per_destroyer(self):
        # note: every test that only builds or observes shares w0; each
        # further world opens for a destroyer that cannot end w0
        listed = self.suites["main"]
        worlds = plan(self.tests, listed, listed)
        # why destroyers, not one test per world: a destroyer's lineage of
        # destroyers (swarm-roster-sync → expel-propagation) shares the
        # world it forks into
        for w in worlds[1:]:
            with self.subTest(world=w.name):
                self.assertEqual([s.kind for s in w.steps][-1], "test")
                self.assertTrue(all(self.tests[s.test].destroys
                                    for s in w.steps if s.kind == "test"))
        ran = [s.test for w in worlds for s in w.steps if s.kind == "test"]
        self.assertEqual(sorted(ran), sorted(listed))

    def test_root_observers_run_before_bootstrap(self):
        w, = plan(self.tests, ["adopt-node", "claim-window"])
        self.assertEqual([s.test for s in w.steps],
                         ["claim-window", "bootstrap-user-software-key",
                          "adopt-node"])

    def test_a_claiming_destroyer_never_follows_bootstrap(self):
        # why: both claim node1, so the destroyer needs a world of its own
        # even when no sibling destroyer ended the shared one first
        for selection in (["bootstrap-user-software-key",
                           "import-user-software-key"],
                          ["adopt-node", "bootstrap-user-op"]):
            with self.subTest(selection=selection):
                worlds = plan(self.tests, selection)
                self.assertEqual(len(worlds), 2)
                self.assertEqual([s.test for s in worlds[1].steps],
                                 [selection[1]])

    def test_two_vm_destroyers_get_a_vm_world_each(self):
        worlds = plan(self.tests, ["tor-link", "nat-punch"])
        self.assertEqual([w.kind for w in worlds], ["vm", "vm"])
        for w, test in zip(worlds, ["nat-punch", "tor-link"]):
            self.assertEqual([(s.test, s.kind) for s in w.steps],
                             [("bootstrap-user-software-key", "prereq"),
                              ("adopt-node", "prereq"), (test, "test")])

    def test_all_tests_plan_into_one_world_per_destroyer(self):
        worlds = plan(self.tests, sorted(self.tests))
        self.assertEqual(
            [(w.kind, w.steps[-1].test) for w in worlds],
            [("local", "app-node-restart-reconnect"),
             ("local", "expel-node"), ("vm", "gateway-relay"),
             ("local", "hold-purge"), ("vm", "nat-punch"),
             ("local", "expel-propagation"),
             ("local", "swarm-member-restart"), ("vm", "tor-link"),
             ("local", "hold-isolation"), ("local", "indexer-cursor"),
             ("local", "repo-removal-sticks"),
             ("local", "bootstrap-user-op"),
             ("local", "distribution-gate-setup"),
             ("local", "import-user-software-key"),
             ("local", "introduce-node"), ("vm", "nearby-relink")])

    def test_a_local_test_never_enters_a_vm_world(self):
        # why app-query and services-app-call: each serves an app on the
        # host, which a node in a VM cannot dial
        for local in ("mcp-peer", "app-query", "services-app-call"):
            with self.subTest(test=local):
                worlds = plan(self.tests, [local, "tor-link"])
                self.assertEqual([w.kind for w in worlds], ["local", "vm"])
        with self.assertRaisesRegex(PlanError, "local only"):
            plan(self.tests, ["mcp-peer"], vm=True)
        with self.assertRaisesRegex(PlanError, "local only"):
            plan(self.tests, ["fs-watch"], driver="agent")

    def test_agent_drives_selected_tests_and_script_replays(self):
        w, = plan(self.tests, ["object-store"], driver="agent")
        self.assertEqual(w.kind, "vm")
        self.assertEqual([(s.test, s.driver) for s in w.steps],
                         [("bootstrap-user-software-key", "script"),
                          ("adopt-node", "script"), ("object-store", "agent")])
        w, = plan(self.tests, ["object-store"], driver="agent",
                  prereqs_agent=True)
        self.assertEqual({s.driver for s in w.steps}, {"agent"})
        with self.assertRaisesRegex(PlanError, "no prompt.md"):
            plan(self.tests, ["smoke"], driver="agent")

    def test_attach_plans_one_world_and_builds_nothing(self):
        w, = plan(self.tests, ["smoke"], target="attach")
        self.assertEqual((w.kind, [s.test for s in w.steps]),
                         ("attach", ["smoke"]))
        with self.assertRaisesRegex(PlanError, "build 'bootstrap"):
            plan(self.tests, ["adopt-node"], target="attach")
        with self.assertRaisesRegex(PlanError, "local only"):
            plan(self.tests, ["mcp-origin"], target="attach")
        with self.assertRaisesRegex(PlanError, "no operator"):
            plan(self.tests, ["bootstrap-user-software-key"], driver="agent",
                 target="attach")
        with self.assertRaisesRegex(PlanError, "one world"):
            plan(self.tests, ["smoke"], target="attach", isolate=True)

    def test_unknown_test(self):
        with self.assertRaisesRegex(PlanError, "unknown test"):
            plan(self.tests, ["smok"])


class TestSyntheticPlans(unittest.TestCase):
    def test_attach_refuses_a_second_node_and_vm_ops(self):
        tests = {"pair": fake(nodes=("node1", "node2")),
                 "op": fake(ops=["enable-tor node1"])}
        with self.assertRaisesRegex(PlanError, "one daemon"):
            plan(tests, ["pair"], target="attach")
        with self.assertRaisesRegex(PlanError, "no simulation"):
            plan(tests, ["op"], target="attach")

    def test_suite_order_is_a_preference_among_siblings(self):
        tests = {"root": fake(), "a": fake(after="root"),
                 "b": fake(after="root")}
        w, = plan(tests, ["root", "a", "b"], ["b", "root", "a"])
        self.assertEqual([s.test for s in w.steps], ["root", "b", "a"])
        w, = plan(tests, ["root", "a", "b"])
        self.assertEqual([s.test for s in w.steps], ["root", "a", "b"])

    def test_a_second_producer_forks_and_replays_the_lineage(self):
        tests = {"root": fake(), "p1": fake(after="root"),
                 "p2": fake(after="root"), "c1": fake(after="p1"),
                 "c2": fake(after="p2")}
        worlds = plan(tests, ["c1", "c2"])
        self.assertEqual([[(s.test, s.kind) for s in w.steps] for w in worlds],
                         [[("root", "prereq"), ("p1", "prereq"),
                           ("c1", "test")],
                          [("root", "prereq"), ("p2", "prereq"),
                           ("c2", "test")]])

    def test_a_destroyers_descendants_follow_it(self):
        tests = {"root": fake(), "d": fake(after="root", destroys=True),
                 "kid": fake(after="d"), "obs": fake(after="root")}
        w, = plan(tests, ["kid", "obs"])
        self.assertEqual([s.test for s in w.steps], ["root", "obs", "d", "kid"])


class TestResumedPlans(unittest.TestCase):
    """Rule 9: a saved world's history is where its one world starts."""

    HISTORY = ["bootstrap-user-software-key", "adopt-node"]

    @classmethod
    def setUpClass(cls):
        cls.all = load_all(TESTS / "e2e")

    def resume(self, *selection):
        return plan(self.all, list(selection), vm=True, done=self.HISTORY)

    def test_the_history_is_marked_done_and_the_rest_runs(self):
        [w] = self.resume("read-swarm")
        self.assertEqual([(s.test, s.done) for s in w.steps],
                         [("bootstrap-user-software-key", True),
                          ("adopt-node", True), ("object-store-peer", False),
                          ("read-swarm", False)])
        self.assertEqual(render([w]).split("\n")[0],
                         "w0 vm: bootstrap-user-software-key= → adopt-node= "
                         "→ object-store-peer*")

    def test_a_world_that_does_not_start_with_the_history_is_refused(self):
        with self.assertRaisesRegex(PlanError, "starts smoke"):
            self.resume("smoke")

    def test_a_selection_of_two_worlds_is_refused(self):
        with self.assertRaisesRegex(PlanError, "plans 2"):
            self.resume("tor-link", "nat-punch")

    def test_a_test_the_history_already_ran_is_refused(self):
        with self.assertRaisesRegex(PlanError, "adopt-node already ran"):
            self.resume("adopt-node")


class TestConfigPlans(unittest.TestCase):
    def test_a_world_carries_the_union_of_its_tests_overrides(self):
        tests = {"root": fake(), "a": fake(after="root", config=mcp("")),
                 "b": fake(after="root", config={
                     "node1": {"objects": {"sweep": "2s"}}})}
        w, = plan(tests, ["root", "a", "b"])
        self.assertEqual(w.config, {"node1": {"mcp": {"bind_mcp": ""},
                                              "objects": {"sweep": "2s"}}})
        self.assertIn('config node1.mcp.bind_mcp = ""', render([w]))

    def test_the_same_value_shares_a_world(self):
        tests = {"a": fake(config=mcp("")), "b": fake(config=mcp(""))}
        w, = plan(tests, ["a", "b"])
        self.assertEqual([s.test for s in w.steps], ["a", "b"])

    def test_values_python_calls_equal_still_clash(self):
        # why: 0 == False and 1 == 1.0 in Python, and a module reads each
        # pair as two settings
        for a, b in ((0, False), (1, True), (1, 1.0)):
            with self.subTest(a=a, b=b):
                tests = {"a": fake(config={"node1": {"m": {"k": a}}}),
                         "b": fake(config={"node1": {"m": {"k": b}}})}
                self.assertEqual(len(plan(tests, ["a", "b"])), 2)

    def test_a_dotted_roster_name_names_the_clash(self):
        on = {"n.1": {"mcp": {"bind_mcp": "x"}}}
        tests = {"root": fake(nodes=("n.1",), config={"n.1": {"mcp": {
                     "bind_mcp": ""}}}),
                 "kid": fake(after="root", nodes=("n.1",), config=on)}
        with self.assertRaisesRegex(PlanError, "n.1.mcp.bind_mcp otherwise "
                                    "than root"):
            plan(tests, ["kid"])

    def test_conflicting_overrides_plan_into_different_worlds(self):
        tests = {"root": fake(), "a": fake(after="root", config=mcp("")),
                 "b": fake(after="root", config=mcp("tcp:127.0.0.1:1"))}
        worlds = plan(tests, ["a", "b"])
        self.assertEqual([[(s.test, s.kind) for s in w.steps] for w in worlds],
                         [[("root", "prereq"), ("a", "test")],
                          [("root", "prereq"), ("b", "test")]])
        self.assertEqual([w.config for w in worlds],
                         [mcp(""), mcp("tcp:127.0.0.1:1")])

    def test_a_clash_with_the_lineage_is_a_plan_error(self):
        tests = {"root": fake(config=mcp("")),
                 "kid": fake(after="root", config=mcp("tcp:127.0.0.1:1"))}
        with self.assertRaisesRegex(PlanError, "kid sets node1.mcp.bind_mcp "
                                    "otherwise than root"):
            plan(tests, ["kid"])
        with self.assertRaisesRegex(PlanError, "otherwise than root"):
            plan(tests, ["kid"], isolate=True)

    def test_a_trimmed_prereq_takes_its_overrides_with_it(self):
        # mcp-origin fixes w0 local, so tor-link forks a VM world and the
        # prereqs w0 built for it are trimmed, overrides and all
        tests = {"obs": fake(local_only="host"),
                 "boot": fake(config=mcp("")),
                 "dvm": fake(after="boot", ops=["enable-tor node1"])}
        worlds = plan(tests, ["obs", "dvm"])
        self.assertEqual([(w.kind, [s.test for s in w.steps], w.config)
                          for w in worlds],
                         [("local", ["obs"], {}),
                          ("vm", ["boot", "dvm"], mcp(""))])

    def test_a_vm_world_carries_and_shows_its_overrides(self):
        tests = {"boot": fake(config=mcp("")),
                 "dvm": fake(after="boot", ops=["enable-tor node1"])}
        w, = plan(tests, ["dvm"])
        self.assertEqual((w.kind, w.config), ("vm", mcp("")))
        self.assertEqual(render([w]),
                         "w0 vm: boot* → dvm\n"
                         '       config node1.mcp.bind_mcp = ""\n')

    GW = "configure-gateway node2 --gateway r --peer node1"

    def test_an_override_a_sibling_op_would_overwrite_forks(self):
        # why: a VM world pushes its overrides at boot and runs ops after, so
        # configure-gateway's gateway.yaml would replace the override unseen
        tests = {"boot": fake(),
                 "obs": fake(after="boot",
                             config={"node2": {"gateway": {"x": 1}}}),
                 "gw": fake(after="boot", ops=[self.GW])}
        worlds = plan(tests, ["obs", "gw"])
        self.assertEqual([[s.test for s in w.steps] for w in worlds],
                         [["boot", "obs"], ["boot", "gw"]])

    def test_only_the_vms_an_op_writes_on_clash(self):
        # configure-gateway writes tcp only on the gateway, gateway.yaml on it
        # and its clients, and nothing on a peer
        for vm, module, shared in (("r", "tcp", False), ("node2", "tcp", True),
                                   ("node1", "gateway", True),
                                   ("r", "gateway", False)):
            with self.subTest(vm=vm, module=module):
                tests = {"boot": fake(),
                         "obs": fake(after="boot",
                                     config={vm: {module: {"x": 1}}}),
                         "gw": fake(after="boot", ops=[self.GW])}
                self.assertEqual(len(plan(tests, ["obs", "gw"])) == 1, shared)

    def test_an_op_over_its_own_lineages_override_is_a_plan_error(self):
        tests = {"boot": fake(config={"r": {"tcp": {"x": 1}}}),
                 "gw": fake(after="boot", ops=[self.GW])}
        with self.assertRaisesRegex(PlanError, "gw's ops write r's tcp config, "
                                    "which boot overrides"):
            plan(tests, ["gw"])

    def test_enable_tor_over_a_tor_override_forks(self):
        # why: enable-tor writes tor.yaml on each VM it names, --vm or bare,
        # and on every VM when it names none
        for line, vm, shared in (("enable-tor node1 node2", "node1", False),
                                 ("enable-tor --vm node2", "node2", False),
                                 ("enable-tor --vm node2", "node1", True),
                                 ("enable-tor", "reflector", False)):
            with self.subTest(line=line, vm=vm):
                tests = {"boot": fake(),
                         "obs": fake(after="boot", config={
                             vm: {"tor": {"listen": False}}}),
                         "tor": fake(after="boot", ops=[line])}
                self.assertEqual(len(plan(tests, ["obs", "tor"])) == 1, shared)

    def test_nat_punch_over_its_own_tor_override_is_a_plan_error(self):
        tests = load_all(TESTS / "e2e")
        tests["nat-punch"].config = {"node1": {"tor": {"listen": False}}}
        with self.assertRaisesRegex(PlanError, "nat-punch's ops write node1's "
                                    "tor config, which nat-punch overrides"):
            plan(tests, ["nat-punch"])

    def test_an_observer_with_a_gateway_override_forks_from_gateway_relay(self):
        tests = load_all(TESTS / "e2e")
        tests["sign-contract-guard"].config = {"node2": {"gateway": {"x": 1}}}
        worlds = plan(tests, ["sign-contract-guard", "gateway-relay"])
        self.assertEqual([(w.kind, [s.test for s in w.steps if s.kind == "test"])
                          for w in worlds],
                         [("local", ["sign-contract-guard"]),
                          ("vm", ["gateway-relay"])])

    def test_attach_refuses_an_override(self):
        tests = {"a": fake(config=mcp(""))}
        with self.assertRaisesRegex(PlanError, "harness-rendered config"):
            plan(tests, ["a"], target="attach")


class TestRunPlan(unittest.TestCase):
    def args(self, **kw):
        base = dict(selection=["tor-link", "nat-punch"], driver="script",
                    target="fresh", keep=False, plan=True, isolate=False,
                    prereqs="script")
        return argparse.Namespace(**{**base, **kw})

    def test_plan_builds_and_starts_nothing(self):
        boom = mock.Mock(side_effect=AssertionError("--plan built or leased"))
        with mock.patch.object(runner, "ensure_binary", boom), \
             mock.patch.object(runner, "lease_ports", boom), \
             mock.patch.object(runner, "fresh_run_dir", boom), \
             mock.patch("builtins.print") as out:
            self.assertEqual(runner.main(self.args()), 0)
        printed = "".join(c.args[0] for c in out.call_args_list)
        self.assertIn("w1 vm: bootstrap-user-software-key* → adopt-node*",
                      printed)

    def test_a_refused_plan_exits_2(self):
        with mock.patch("builtins.print"):
            self.assertEqual(runner.main(self.args(
                selection=["adopt-node"], target="attach")), 2)

    def test_prereqs_agent_without_the_agent_driver_is_refused(self):
        boom = mock.Mock(side_effect=AssertionError("refused run built"))
        with mock.patch.object(runner, "ensure_binary", boom), \
             mock.patch("builtins.print"):
            self.assertEqual(runner.main(self.args(
                plan=False, selection=["smoke"], prereqs="agent")), 2)

    def test_save_refuses_a_run_that_ends_in_a_local_world(self):
        # why: only a VM world is a netsim stage that can be saved
        with mock.patch("builtins.print"):
            self.assertEqual(runner.main(self.args(selection=["mcp-origin"],
                                                   save="gw")), 2)

    def test_a_name_netsim_would_not_take_is_refused_up_front(self):
        # why: netsim refuses it only after the run, and a --from name is a
        # file name on the host
        for opt in ({"save": "Gate Way"}, {"save": "-gw"},
                    {"from_stage": "../gw"}):
            with self.subTest(**opt), mock.patch("builtins.print"):
                self.assertEqual(runner.main(self.args(
                    selection=["adopt-node"], vm=True, **opt)), 2)

    def test_from_refuses_a_world_this_host_never_saved(self):
        with tempfile.TemporaryDirectory() as tmp, \
                mock.patch.object(runner, "SNAPSHOTS", Path(tmp)), \
                mock.patch("builtins.print"):
            self.assertEqual(runner.main(self.args(selection=["gateway-relay"],
                                                   from_stage="gw")), 2)


if __name__ == "__main__":
    unittest.main()


class TestDockerPlans(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.tests = load_all(TESTS / "e2e")
        cls.agent = load(TESTS / "suites" / "agent.suite")

    def test_the_agent_driver_takes_the_heavy_kind_it_is_given(self):
        worlds = plan(self.tests, self.agent, self.agent, driver="agent",
                      heavy="docker")
        self.assertEqual({w.kind for w in worlds}, {"docker"})

    def test_golden_agent_docker_plan(self):
        got = render(plan(self.tests, self.agent, self.agent, driver="agent",
                          heavy="docker"))
        self.assertEqual(
            got, (GOLDEN / "agent-docker.plan").read_text(),
            "agent.suite plans differently in Docker worlds; if intended, "
            "run ./tests/run --plan --driver agent --docker agent.suite > "
            "tests/selftest/golden/agent-docker.plan")

    def test_golden_integration_docker_plan(self):
        listed = load(TESTS / "suites" / "integration.suite")
        got = render(plan(self.tests, listed, listed, docker=True))
        self.assertEqual(
            got, (GOLDEN / "integration-docker.plan").read_text(),
            "integration.suite plans differently in Docker worlds; if "
            "intended, run ./tests/run --plan --docker integration.suite > "
            "tests/selftest/golden/integration-docker.plan")

    def test_docker_forces_every_world(self):
        worlds = plan(self.tests, ["smoke"], docker=True)
        self.assertEqual([w.kind for w in worlds], ["docker"])

    def test_a_docker_run_refuses_vm_operations(self):
        with self.assertRaisesRegex(PlanError, "tor-link runs VM operations"
                                               ".*add --vm"):
            plan(self.tests, ["tor-link"], docker=True)

    def test_a_docker_run_refuses_a_local_only_test(self):
        tests = {"a": fake(local_only="watches a host directory")}
        with self.assertRaisesRegex(PlanError, "Docker containers"):
            plan(tests, ["a"], docker=True)

    def test_vm_and_docker_together_are_refused(self):
        with self.assertRaisesRegex(PlanError, "choose one"):
            plan(self.tests, ["smoke"], vm=True, docker=True)

    def test_without_the_agent_driver_heavy_changes_nothing(self):
        listed = load(TESTS / "suites" / "main.suite")
        self.assertEqual(
            render(plan(self.tests, listed, listed, heavy="docker")),
            render(plan(self.tests, listed, listed)))
