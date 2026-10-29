"""The shipped manifests and suites, checked against each other.

The other selftests exercise the rules on synthetic trees; this one holds the
repository to them, so a manifest typo or a suite reordered into an
unsatisfiable walk fails here rather than after a daemon has booted.
"""
import ast
import unittest
from pathlib import Path

from lib.manifest import load_all, resolve, validate_order
from lib.suites import load

TESTS = Path(__file__).resolve().parent.parent


def _is_facts(node) -> bool:
    return (isinstance(node, ast.Subscript)
            and isinstance(node.slice, ast.Constant)
            and node.slice.value == "facts")


def _read(node, names: set) -> str | None:
    """The fact `node` reads off a session's top-level facts, if it does:
    facts["x"] or facts.get("x"), on doc["facts"] or a name bound to it."""
    if isinstance(node, ast.Subscript):
        base, key = node.value, node.slice
    elif (isinstance(node, ast.Call) and isinstance(node.func, ast.Attribute)
          and node.func.attr == "get" and node.args):
        base, key = node.func.value, node.args[0]
    else:
        return None
    on_facts = _is_facts(base) or (isinstance(base, ast.Name)
                                   and base.id in names)
    if on_facts and isinstance(key, ast.Constant) and isinstance(key.value, str):
        return key.value
    return None


def oracle_reads(source: str) -> set:
    """The fact names a verify.py reads, scope by scope: names bound to
    doc["facts"], and parameters a call hands doc["facts"] to.

    note: a name rebound to a nested dict (services-swarm's facts) is not a
    top-level read, so only a binding to doc["facts"] itself counts.
    """
    tree = ast.parse(source)
    funcs = {f.name: f for f in ast.walk(tree)
             if isinstance(f, ast.FunctionDef)}
    given = {}
    for call in ast.walk(tree):
        if isinstance(call, ast.Call) and getattr(call.func, "id", None) in funcs:
            for i, arg in enumerate(call.args):
                if _is_facts(arg):
                    given.setdefault(call.func.id, set()).add(
                        funcs[call.func.id].args.args[i].arg)
    out = set()
    for scope in [tree, *funcs.values()]:
        nodes = [n for stmt in scope.body for n in ast.walk(stmt)]
        names = set(given.get(getattr(scope, "name", None), ()))
        names |= {t.id for n in nodes if isinstance(n, ast.Assign)
                  and _is_facts(n.value) for t in n.targets
                  if isinstance(t, ast.Name)}
        out |= {k for n in nodes if (k := _read(n, names))}
    return out


class TestShipped(unittest.TestCase):
    def setUp(self):
        self.all = load_all(TESTS / "e2e")

    def test_every_suite_walks(self):
        suites = sorted((TESTS / "suites").glob("*.suite"))
        self.assertTrue(suites, "no suites ship")
        for path in suites:
            with self.subTest(suite=path.name):
                validate_order(self.all, load(path))

    def test_every_test_resolves_alone(self):
        for name in sorted(self.all):
            with self.subTest(test=name):
                plan = resolve(self.all, [name])
                self.assertEqual(plan[-1][0].name, name)
                self.assertEqual(plan[-1][1], "test")

    def test_every_test_ships_its_four_files(self):
        for name, t in sorted(self.all.items()):
            with self.subTest(test=name):
                for f in ("test.toml", "script.py", "verify.py", "README.md"):
                    self.assertTrue((t.dir / f).is_file(), f"{name}/{f} missing")

    def test_every_test_is_listed_in_a_suite_or_says_why_not(self):
        # why: a test no suite lists is never executed, and nothing else in the
        # tree notices. apphost-origin landed unlisted because the state it
        # starts from was broken that week, and stayed unlisted — and unrun —
        # after the fix. The partition is the guard: listed, or carrying the
        # reason it is not.
        listed = set()
        for path in sorted((TESTS / "suites").glob("*.suite")):
            listed.update(load(path))

        for name, t in sorted(self.all.items()):
            with self.subTest(test=name):
                if t.unlisted:
                    self.assertNotIn(
                        name, listed,
                        f"{name} declares unlisted ({t.unlisted!r}) and a "
                        f"suite lists it anyway — the reason is stale")
                else:
                    self.assertIn(
                        name, listed,
                        f"{name} is in no suite and declares no `unlisted` "
                        f"reason, so nothing ever runs it")

    def test_a_mutator_is_last_in_every_suite_that_lists_it(self):
        # why: the walk already rejects a consumer placed after a mutator, but
        # a mutator mid-suite means every following test was skipped by design
        # rather than by accident — say it once, here.
        for path in sorted((TESTS / "suites").glob("*.suite")):
            listed = load(path)
            for i, name in enumerate(listed[:-1]):
                with self.subTest(suite=path.name, test=name):
                    self.assertFalse(self.all[name].mutates,
                                     f"{name} mutates and is not last")

    def test_no_suite_lists_both_tor_link_and_nat_punch(self):
        # why: tor-link's leave-lan downs node2's LAN interface, and nat-punch's
        # enter-nat then finds none to move behind NAT — the two cannot share
        # one world, which substrate.suite asked them to
        for path in sorted((TESTS / "suites").glob("*.suite")):
            with self.subTest(suite=path.name):
                self.assertFalse({"tor-link", "nat-punch"} <= set(load(path)))

    def test_every_fact_an_oracle_reads_is_declared(self):
        # why: `facts` is the contract the agent driver's prompt footer is
        # built from, so an oracle reading a fact no manifest on its lineage
        # declares would fail an operator that did everything it was told
        for name, t in sorted(self.all.items()):
            declared, cur = set(t.facts), t
            while cur.after:
                cur = self.all[cur.after]
                declared |= set(cur.facts)
            with self.subTest(test=name):
                read = oracle_reads((t.dir / "verify.py").read_text())
                self.assertEqual(read - declared, set(),
                                 f"{name}'s oracle reads undeclared facts")

    def test_planner_keys_agree_with_the_state_chain(self):
        # why: until planned worlds run, the runner reads the state-chain keys
        # and `--plan` reads the planner's. A manifest that changed one and not
        # the other would plan one order and run another.
        producers = {t.saves: n for n, t in self.all.items() if t.saves}
        for name, t in sorted(self.all.items()):
            with self.subTest(test=name):
                self.assertEqual(t.after, producers.get(t.start))
                self.assertEqual(t.ops, [s.removeprefix("vm:")
                                         for s in t.steps if s != "driver"])
                if t.mutates:
                    self.assertTrue(t.destroys, "a mutator destroys")
                if t.restart:
                    self.assertTrue(t.local_only, "a restart is local only")
                self.assertEqual(t.prompt, "agent" in t.drivers)


if __name__ == "__main__":
    unittest.main()
