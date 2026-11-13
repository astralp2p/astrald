"""The planner: manifests and a selection in, worlds out.

Pure: it reads loaded manifests and returns a plan. It starts nothing and
writes nothing. The runner runs every plan, world by world, and `--plan`
prints one.

A test names the test whose world it continues (`after`), and the worlds
follow from that tree:

1. A test's lineage is its `after` chain. Each world starts fresh and runs
   part of one lineage tree.
2. At each test P, P's observers run first, in suite order. An observer is a
   child of P that no test continues and that destroys nothing.
3. Then P's producers: the first continues the world, and each further one
   gets a fork that replays P's lineage as prereqs.
4. One destroyer may end the world, provided the world's kind suits it and
   nothing has ended the world yet. A root destroyer, one with no `after`,
   also needs a world no producer has built on, since a fresh world is the
   only state it can name. Every other destroyer forks.
5. A world's kind follows from what runs in it: an op needs a VM world;
   `--vm`, `--docker` or the agent driver needs a heavy world, a VM or a
   Docker one (`heavy`, which the runner picks for the host); and a
   local_only test needs a local one. A Docker run refuses an op. A test
   whose roster names a node the VM lab does not run (`LAB_NODES`), that
   stops or restarts a node, or that serves a listener a node dials on the
   test host (`host_listener`), never enters a VM world, and stays local
   unless the run asks for Docker; with ops as well, it cannot run at all.
6. `isolate` gives every selected test a world of its own.
7. Under the agent driver, selected tests are operator-driven; prereqs and
   replays run by script unless `prereqs_agent`. A test with no prompt.md
   that would be operator-driven is a plan error, on purpose: a test an
   operator can drive is exactly one that ships the prompt it works from.
8. Under `--target attach`, the plan is one world on the daemon already up,
   which the harness never stops or restarts. A host_listener test plans
   there; the runner fails its prepare when the daemon is remote.
9. `done` is the history of a saved world the run boots (`--from`): the plan
   is that one world, its first steps are exactly that history, and they are
   marked done rather than run.
10. A world's node config is the union of its tests' `[config.<node>.<module>]`
    overrides. A test that sets a key otherwise than the world forks, and one
    that clashes with its own lineage cannot run at all.
"""
import json
from dataclasses import dataclass, field

WIDTH = 79          # a rendered plan wraps its worlds at this column

# op -> where it writes a module's config file in its guests: "" for the
# VMs its bare words (or --vm) name, a flag for the VM that flag's value
# names, and None for every VM, on a line that names none: a bare
# `enable-tor` runs on every VM the simulation runs.
# why the planner knows: a VM world pushes its overrides when it boots and
# runs ops after, so an op's file would silently replace an override. A test
# that would bring the two together forks, and a lineage that holds both is
# refused.
OP_WRITES = {"configure-gateway": {"": {"gateway"},
                                   "--gateway": {"tcp", "gateway"}},
             "enable-tor": {"": {"tor"}, None: {"tor"}}}


class PlanError(ValueError):
    """A selection no plan can run, and why."""


# The machines the VM lab runs (netsim/labs/two-node-plus-external/lab.story).
LAB_NODES = frozenset({"node1", "node2", "reflector"})


def off_lab(test) -> list:
    """The roster names a VM world cannot give test: nodes its lab lacks.

    why a rule and not local_only: such a test needs no host — a local or a
    Docker world starts any name — only a world that is not a VM
    """
    return [n for n in test.nodes if n not in LAB_NODES]


def vm_refusal(test) -> str | None:
    """Why a VM world cannot run test, or None when one can.

    why stop, restart and host_listener are here and not local_only: a local
    and a Docker world stop and restart a node and give it a host_ip; a VM
    world does none of these (lib/worlds)
    """
    return _lab_refusal(test) or (
        "serves a listener on the test host, and a VM world gives its nodes "
        "no host_ip to dial it at" if test.host_listener else None)


def _lab_refusal(test) -> str | None:
    """Why neither a VM world nor an attached daemon can run test: its roster
    or the stop or restart it asks of a node."""
    strays = off_lab(test)
    if strays:
        return f"names {', '.join(strays)}, which the VM lab does not run"
    if test.stop:
        return "stops a node, which a VM world cannot"
    if test.restart:
        return "restarts a node, which a VM world cannot"
    return None


@dataclass
class Step:
    test: str
    kind: str       # "test", or "prereq": run to build what a test continues
    driver: str     # "script" or "agent"
    done: bool = False  # already in the saved world the run boots: not run


@dataclass
class World:
    name: str
    kind: str | None = None   # "local", "vm", "docker" or "attach"; None until a step needs one
    steps: list = field(default_factory=list)
    ended: bool = False       # a destroyer ran: only its descendants may follow
    config: dict = field(default_factory=dict)   # node -> module -> keys


def _writes(line: str) -> set:
    """The (vm, module) config files one op line writes in its guests; a vm
    of None is every VM."""
    op, *args = line.split()
    where = OP_WRITES.get(op, {})
    out, words, named = set(), iter(args), False
    for a in words:
        # why --vm reads as a bare word: run_op hands each bare word to its
        # op as --vm, so a line may pass the flag itself
        flag = "" if a == "--vm" or not a.startswith("-") else a
        vm = next(words, None) if a.startswith("-") else a
        named = named or flag == ""
        out |= {(vm, m) for m in where.get(flag, ()) if vm}
    if not named:
        out |= {(None, m) for m in where.get(None, ())}
    return out


def _covers(writes: set, override: tuple) -> bool:
    """Whether a (vm, module) override is a file writes replaces."""
    return override in writes or (None, override[1]) in writes


def merge_config(into: dict, config: dict) -> None:
    """Fold one test's overrides into a world's."""
    for node, modules in config.items():
        for module, keys in modules.items():
            into.setdefault(node, {}).setdefault(module, {}).update(keys)


def _same(a, b) -> bool:
    """Whether two override values render to the same config.

    why not ==: Python holds 0 == False and 1 == 1.0, and a module reads
    `false` and `0` as different settings.
    """
    return json.dumps(a, sort_keys=True) == json.dumps(b, sort_keys=True)


def _clash(have: dict, config: dict) -> tuple | None:
    """The first (node, module, key) config sets otherwise than have."""
    for node, modules in config.items():
        for module, keys in modules.items():
            set_ = have.get(node, {}).get(module, {})
            for key, value in keys.items():
                if key in set_ and not _same(set_[key], value):
                    return node, module, key
    return None


def chain(tests: dict, name: str) -> list:
    """name's lineage, oldest first, ending with name itself."""
    out = [name]
    while tests[out[0]].after is not None:
        out.insert(0, tests[out[0]].after)
    return out


def plan(tests: dict, selection: list, order=(), *, isolate=False,
         driver="script", vm=False, docker=False, heavy="vm",
         target="fresh", prereqs_agent=False, done=()) -> list:
    """The worlds a run of `selection` needs. `order` is a suite's listing,
    a preference among siblings; the rest sort by name. `done` is the
    history of the saved world the run boots, oldest first. `heavy` is the
    kind the agent driver gets, "vm" or "docker"; `vm` and `docker` force
    their kind on every world."""
    unknown = [n for n in selection if n not in tests]
    if unknown:
        raise PlanError(f"unknown test(s): {', '.join(unknown)}")
    if target == "attach" and driver == "agent":
        # why: the operator lives in the simulated world, and attach borrows
        # a daemon instead of bringing one
        raise PlanError("--driver agent has no operator under --target attach")
    if vm and docker:
        raise PlanError("--vm and --docker each put every world in their "
                        "kind: choose one")
    forced = ("docker" if docker else "vm" if vm
              else heavy if driver == "agent" else None)
    p = _Planner(tests, set(selection), order, driver, prereqs_agent,
                 forced=forced)
    worlds = p.isolated() if isolate else p.shared()
    for w in worlds:
        p.trim(w)
    if target == "attach":
        return _attach(p, worlds, isolate)
    for w in worlds:
        w.kind = w.kind or p.forced or "local"
    if done:
        _resume(worlds, list(done), set(selection))
    return worlds


def _resume(worlds: list, done: list, selected: set) -> None:
    """Rule 9: mark the steps a saved world already stands past.

    why a prefix and nothing looser: a saved world is exactly its history,
    and a world whose first steps differ from it would run its tests on a
    state the plan never named — a test that destroys, or one the snapshot
    never ran, would be assumed rather than run.
    """
    if len(worlds) != 1:
        raise PlanError(f"a saved world boots one world, and this selection "
                        f"plans {len(worlds)}")
    steps = worlds[0].steps
    first = [s.test for s in steps[:len(done)]]
    if first != done:
        raise PlanError(f"the saved world stands past {' → '.join(done)}, "
                        f"and this selection's world starts "
                        f"{' → '.join(first) or 'fresh'}")
    ran = sorted(selected & set(done))
    if ran:
        raise PlanError(f"{ran[0]} already ran in the saved world")
    for s in steps[:len(done)]:
        s.done = True


class _Planner:
    def __init__(self, tests, selected, order, driver, prereqs_agent, forced):
        self.tests, self.selected, self.forced = tests, selected, forced
        self.driver, self.prereqs_agent = driver, prereqs_agent
        self.needed = {n for s in selected for n in chain(tests, s)}
        rank = {n: i for i, n in enumerate(order)}
        self.kids = {}
        for n in sorted(self.needed, key=lambda n: (rank.get(n, len(rank)), n)):
            self.kids.setdefault(tests[n].after, []).append(n)
        # why over every test and not the selection: a producer builds state
        # whether or not this run continues it, and bootstrap run alone still
        # claims the node1 a later sibling may need unclaimed
        self.producers = {t.after for t in tests.values()} - {None}
        self.worlds = []
        for n in sorted(self.needed):
            self._check(n)

    def _check(self, n: str) -> None:
        t = self.tests[n]
        refusal = vm_refusal(t)
        if t.ops and refusal:
            # why: ops put the test in a VM world, whatever the run forces
            raise PlanError(f"{n} runs VM operations and {refusal}")
        if self.forced == "vm" and refusal and not t.local_only:
            raise PlanError(f"{n} {refusal}, and this run puts every world "
                            "in VMs")
        if self.forced and t.local_only:
            where = "VMs" if self.forced == "vm" else "Docker containers"
            raise PlanError(f"{n} is local only ({t.local_only}), and this "
                            f"run puts every world in {where}")
        if self.forced == "docker" and t.ops:
            raise PlanError(f"{n} runs VM operations, which a Docker world "
                            "cannot: add --vm")
        kind = "test" if n in self.selected else "prereq"
        if self._driver(kind) == "agent" and not t.prompt:
            raise PlanError(f"{n} ships no prompt.md, so no operator can "
                            "drive it")

    def _driver(self, kind: str) -> str:
        """Who drives a step: an operator for a test under the agent driver,
        and for a prereq or a replay only when prereqs_agent asks."""
        agent = self.driver == "agent" and (kind == "test"
                                            or self.prereqs_agent)
        return "agent" if agent else "script"

    def _need(self, n: str) -> str | None:
        t = self.tests[n]
        if t.ops:
            return "vm"
        if self.forced:
            return self.forced
        return "local" if t.local_only or vm_refusal(t) else None

    def _fits(self, w: World, n: str) -> bool:
        need = self._need(n)
        return (not w.ended and (w.kind is None or need is None
                                 or w.kind == need)
                and _clash(w.config, self.tests[n].config) is None
                and self._overwritten(w, n) is None)

    def _overwritten(self, w: World, n: str) -> tuple | None:
        """The (vm, module) override an op would write over, were n to join
        w: the op and the override may come from n or from w's steps."""
        names = [s.test for s in w.steps] + [n]
        writes = {x for m in names for line in self.tests[m].ops
                  for x in _writes(line)}
        overrides = {(vm, module) for m in names
                     for vm, modules in self.tests[m].config.items()
                     for module in modules}
        return min((o for o in overrides if _covers(writes, o)), default=None)

    def _refusal(self, w: World, n: str, what: str) -> str:
        """Why n cannot <what> in w, a fork that already replays its lineage."""
        hit = self._overwritten(w, n)
        if hit is not None:
            vm, module = hit
            names = [s.test for s in w.steps] + [n]
            op = next(m for m in names if any(
                _covers(_writes(line), hit) for line in self.tests[m].ops))
            conf = next(m for m in names
                        if module in self.tests[m].config.get(vm, {}))
            return (f"{op}'s ops write {vm}'s {module} config, which {conf} "
                    f"overrides, and {n}'s lineage puts both in one world")
        clash = _clash(w.config, self.tests[n].config)
        if clash is None:
            return f"{n} cannot {what} in a {w.kind} world"
        node, module, key = clash
        other = next((s.test for s in w.steps if key in self.tests[s.test]
                      .config.get(node, {}).get(module, {})), "its world")
        return (f"{n} sets {node}.{module}.{key} otherwise than {other}, and "
                "its lineage puts both in one world")

    def _add(self, w: World, n: str, kind: str) -> None:
        w.kind = w.kind or self._need(n)
        merge_config(w.config, self.tests[n].config)
        w.steps.append(Step(n, kind, self._driver(kind)))

    def _fork(self, parent: str | None) -> World:
        """A fresh world that replays parent's lineage as prereqs."""
        w = World(f"w{len(self.worlds)}")
        self.worlds.append(w)
        for n in chain(self.tests, parent) if parent else []:
            if not self._fits(w, n):
                raise PlanError(self._refusal(w, n, "be replayed"))
            self._add(w, n, "prereq")
        return w

    def _place(self, n: str, w: World, share: bool) -> World:
        """The world n runs in: w when it may share it, else a fork."""
        if not (share and self._fits(w, n)):
            w = self._fork(self.tests[n].after)
            if not self._fits(w, n):
                raise PlanError(self._refusal(w, n, "run"))
        self._add(w, n, "test" if n in self.selected else "prereq")
        return w

    def _walk(self, parent: str | None, w: World) -> None:
        children = self.kids.get(parent, [])
        observers = [c for c in children
                     if c not in self.producers and not self.tests[c].destroys]
        # why builders first: a destroyer ends the world it runs in
        consumers = sorted((c for c in children if c not in observers),
                           key=lambda c: self.tests[c].destroys)
        for c in observers:
            self._place(c, w, share=True)
        for i, c in enumerate(consumers):
            t = self.tests[c]
            # why a root destroyer never runs after a sibling producer: a
            # fresh world is the only state it can name, and a producer has
            # changed it — bootstrap-user-software-key claims the node1 that
            # import-user-software-key and bootstrap-user-op set out to claim.
            # Root observers may still precede it, since they build nothing.
            share = i == 0 or (t.destroys and t.after is not None)
            cw = self._place(c, w, share)
            self._walk(c, cw)
            if t.destroys:
                cw.ended = True

    def shared(self) -> list:
        self._walk(None, self._fork(None))
        return self.worlds

    def isolated(self) -> list:
        """One world per selected test, in the order a shared run takes."""
        order = [s.test for w in self.shared() for s in w.steps
                 if s.kind == "test"]
        self.worlds = []
        for n in order:
            self._place(n, self._fork(self.tests[n].after), share=True)
        return self.worlds

    def trim(self, w: World) -> None:
        """Drop the prereqs no later test in w builds on, and refit its kind.

        why: a producer continues its parent's world even when the test it
        feeds forks away — an observer fixes the world local, and tor-link
        then needs a VM — so its prereqs would run for nothing.
        """
        feeds, kept = set(), []
        for s in reversed(w.steps):
            if s.kind == "test" or s.test in feeds:
                kept.append(s)
                feeds.update(chain(self.tests, s.test))
        w.steps = kept[::-1]
        w.kind = next((k for s in w.steps if (k := self._need(s.test))), None)
        w.config = {}
        for s in w.steps:
            merge_config(w.config, self.tests[s.test].config)


def _attach(p: _Planner, worlds: list, isolate: bool) -> list:
    """Rule 8: the daemon already up is the one world, and the harness checks
    what it stands at but never builds or reshapes it."""
    built = sorted(p.needed - p.selected)
    if built:
        # why: building a prereq runs its flow at the operator's own daemon —
        # bootstrap stores a key and attempts a contract on a live node
        raise PlanError(f"--target attach would have to build {built[0]!r} "
                        "on the attached daemon; attach checks a start "
                        "state, it never builds one")
    for n in sorted(p.selected):
        t = p.tests[n]
        if len(t.nodes) != 1:
            raise PlanError(f"{n} wants {len(t.nodes)} nodes, and --target "
                            "attach has one daemon")
        if t.restart:
            raise PlanError(f"{n} restarts a node, and --target attach never "
                            "restarts the daemon it borrows")
        if t.local_only or _lab_refusal(t):
            raise PlanError(f"{n} is local only "
                            f"({t.local_only or 'it ' + _lab_refusal(t)}),"
                            " and the attached daemon has no harness-rendered"
                            " config")
        if t.ops:
            raise PlanError(f"{n} runs VM operations, and --target attach "
                            "has no simulation")
        if t.config:
            raise PlanError(f"{n} overrides node config, and the attached "
                            "daemon has no harness-rendered config")
    if isolate or len(worlds) != 1:
        raise PlanError("--target attach has one daemon, so the selection "
                        "has to run in one world")
    worlds[0].kind = "attach"
    return worlds


def render(worlds: list) -> str:
    """The plan as text, one world per paragraph; `*` marks a prereq and
    `=` a step the saved world already stands past. A world's node config
    overrides follow its steps, one key per line."""
    heads = [f"{w.name} {w.kind}:" for w in worlds]
    pad = max(map(len, heads), default=0) + 1
    out = []
    for head, w in zip(heads, worlds):
        line = head.ljust(pad)
        for i, s in enumerate(w.steps):
            mark = "=" if s.done else "*" if s.kind == "prereq" else ""
            label = (s.test + mark
                     + (" (agent)" if s.driver == "agent" else ""))
            piece = label if i == 0 else f" → {label}"
            if i and len(line) + len(piece) > WIDTH:
                out.append(line)
                line = " " * pad + f"→ {label}"
            else:
                line += piece
        out.append(line)
        out += [" " * pad + f"config {node}.{module}.{key} = {json.dumps(v)}"
                for node, modules in sorted(w.config.items())
                for module, keys in sorted(modules.items())
                for key, v in sorted(keys.items())]
    return "\n".join(out) + "\n"
