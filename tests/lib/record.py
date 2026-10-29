"""The world record: what one world ran and what its passed tests learned.

The runner is its only writer. An executor owns machines and reports their
nodes; it never sees a test's facts or verdict.

    results/<stamp>/w0/world.json
    {"world": "w0", "run": "<stamp>",
     "nodes":   {<name>: <node entry>},           the nodes as last seen
     "history": [{"test", "kind", "status"}],     every test that ran, in order
     "facts":   {<test>: {<fact>: <value>}},      only tests that passed
     "config":  {<node>: {<module>: {<key>: …}}}} the overrides it started with
"""
import json
from pathlib import Path


class WorldRecord:
    def __init__(self, run_dir: Path, world: str = "w0"):
        self.path = Path(run_dir) / world / "world.json"
        self.path.parent.mkdir(parents=True, exist_ok=True)
        self.world = world
        self.run = Path(run_dir).name
        self.nodes = {}
        self.history = []
        self.facts = {}
        self.config = {}

    def facts_of(self, lineage: list) -> dict:
        """The facts the passed tests among `lineage` wrote, oldest first,
        so the nearest ancestor wins a name two of them share."""
        out = {}
        for name in reversed(lineage):
            out.update(self.facts.get(name, {}))
        return out

    def add(self, test: str, kind: str, status: str, facts: dict) -> None:
        """One test ran here. Its facts join only on a pass.

        why: a failed producer's facts reached every later test while facts
        were one flat dict merged before the oracle, so a dependent could
        read a value its oracle had just rejected.
        """
        self.history.append({"test": test, "kind": kind, "status": status})
        if status == "pass":
            self.facts[test] = facts
        self.write()

    def doc(self) -> dict:
        return {"world": self.world, "run": self.run, "nodes": self.nodes,
                "history": self.history, "facts": self.facts,
                "config": self.config}

    def write(self) -> None:
        self.path.write_text(json.dumps(self.doc(), indent=2) + "\n")
