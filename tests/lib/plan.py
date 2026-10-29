"""The planner: manifests and a selection in, worlds out.

Pure: it reads loaded manifests and returns a plan. It starts nothing and
writes nothing. The runner runs a plan whose worlds are all local, and
`--plan` prints any plan; a run with a VM world still takes the executor
path.

A test names the test whose world it continues (`after`), and the worlds
follow from that tree:

1. A test's lineage is its `after` chain. Each world starts fresh and runs
   part of one lineage tree.
2. At each test P, P's observers run first, in suite order. An observer is a
   child of P that no test continues and that destroys nothing.
3. Then P's producers: the first continues the world, and each further one
   gets a fork that replays P's lineage as prereqs.
4. One destroyer may end the world, provided the world's kind suits it and
   nothing has ended the world yet. Every other destroyer forks.
5. A world's kind follows from what runs in it: an op, `--vm` or the agent
   driver needs a VM world, and a local_only test needs a local one.
6. `isolate` gives every selected test a world of its own.
7. Under the agent driver, selected tests are operator-driven; prereqs and
   replays run by script unless `prereqs_agent`. A test with no prompt.md
   that would be operator-driven is a plan error, on purpose: it mirrors
   the runner's refusal of a test that declares no agent driver.
8. Under `--target attach`, the plan is one world on the daemon already up.
"""
from dataclasses import dataclass, field

WIDTH = 79          # a rendered plan wraps its worlds at this column


class PlanError(ValueError):
    """A selection no plan can run, and why."""


@dataclass
class Step:
    test: str
    kind: str       # "test", or "prereq": run to build what a test continues
    driver: str     # "script" or "agent"


@dataclass
class World:
    name: str
    kind: str | None = None   # "local", "vm" or "attach"; None until a step needs one
    steps: list = field(default_factory=list)
    ended: bool = False       # a destroyer ran: only its descendants may follow


def chain(tests: dict, name: str) -> list:
    """name's lineage, oldest first, ending with name itself."""
    out = [name]
    while tests[out[0]].after is not None:
        out.insert(0, tests[out[0]].after)
    return out


def plan(tests: dict, selection: list, order=(), *, isolate=False,
         driver="script", vm=False, target="fresh",
         prereqs_agent=False) -> list:
    """The worlds a run of `selection` needs. `order` is a suite's listing,
    a preference among siblings; the rest sort by name."""
    unknown = [n for n in selection if n not in tests]
    if unknown:
        raise PlanError(f"unknown test(s): {', '.join(unknown)}")
    if target == "attach" and driver == "agent":
        # why: the operator lives in the simulated world, and attach borrows
        # a daemon instead of bringing one
        raise PlanError("--driver agent has no operator under --target attach")
    p = _Planner(tests, set(selection), order, driver, prereqs_agent,
                 forced="vm" if vm or driver == "agent" else None)
    worlds = p.isolated() if isolate else p.shared()
    for w in worlds:
        p.trim(w)
    if target == "attach":
        return _attach(p, worlds, isolate)
    for w in worlds:
        w.kind = w.kind or p.forced or "local"
    return worlds


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
        if self.forced and t.local_only:
            raise PlanError(f"{n} is local only ({t.local_only}), and this "
                            "run puts every world in VMs")
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
        if t.ops or self.forced:
            return "vm"
        return "local" if t.local_only else None

    def _fits(self, w: World, n: str) -> bool:
        need = self._need(n)
        return not w.ended and (w.kind is None or need is None
                                or w.kind == need)

    def _add(self, w: World, n: str, kind: str) -> None:
        w.kind = w.kind or self._need(n)
        w.steps.append(Step(n, kind, self._driver(kind)))

    def _fork(self, parent: str | None) -> World:
        """A fresh world that replays parent's lineage as prereqs."""
        w = World(f"w{len(self.worlds)}")
        self.worlds.append(w)
        for n in chain(self.tests, parent) if parent else []:
            if not self._fits(w, n):
                raise PlanError(f"{n} cannot be replayed in a "
                                f"{w.kind} world")
            self._add(w, n, "prereq")
        return w

    def _place(self, n: str, w: World, share: bool) -> World:
        """The world n runs in: w when it may share it, else a fork."""
        if not (share and self._fits(w, n)):
            w = self._fork(self.tests[n].after)
            if not self._fits(w, n):
                raise PlanError(f"{n} cannot run in a {w.kind} world")
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
        if t.local_only:
            raise PlanError(f"{n} is local only ({t.local_only}), and the "
                            "attached daemon has no harness-rendered config")
        if t.ops:
            raise PlanError(f"{n} runs VM operations, and --target attach "
                            "has no simulation")
    if isolate or len(worlds) != 1:
        raise PlanError("--target attach has one daemon, so the selection "
                        "has to run in one world")
    worlds[0].kind = "attach"
    return worlds


def render(worlds: list) -> str:
    """The plan as text, one world per paragraph; `*` marks a prereq."""
    heads = [f"{w.name} {w.kind}:" for w in worlds]
    pad = max(map(len, heads), default=0) + 1
    out = []
    for head, w in zip(heads, worlds):
        line = head.ljust(pad)
        for i, s in enumerate(w.steps):
            label = (s.test + ("*" if s.kind == "prereq" else "")
                     + (" (agent)" if s.driver == "agent" else ""))
            piece = label if i == 0 else f" → {label}"
            if i and len(line) + len(piece) > WIDTH:
                out.append(line)
                line = " " * pad + f"→ {label}"
            else:
                line += piece
        out.append(line)
    return "\n".join(out) + "\n"
