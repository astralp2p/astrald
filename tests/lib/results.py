"""results.json / events.jsonl writer — the system's one stable output (v2).

v2.1 renames the record kind `fixture` to `prereq`: the value names a
full e2e test — own driver, own oracle — running to build the state another
test starts from, and "fixture" said none of that.

v2 vs M1: `env` replaces `lane` on every record, the header drops `lanes`
and gains `astral_py_ref`, and `hermetic` is set by the run's --target
rather than hard-coded.
"""
import json
import time
from dataclasses import dataclass
from pathlib import Path

from lib import report

STATUSES = {"pass", "fail", "skipped"}
FAILURE_KINDS = {"driver", "verify", "environment"}
KINDS = {"test", "prereq"}
ENVS = {"node", "netsim"}


def fresh_run_dir(results: Path) -> Path:
    """A directory this run owns alone.

    why: the stamp has one-second resolution, so two runs started in the same
    second shared a directory — results.json was overwritten and events.jsonl,
    opened for append, interleaved both runs into one file.
    """
    stamp = time.strftime("%Y%m%dT%H%M%SZ", time.gmtime())
    for suffix in ("", *(f"-{n}" for n in range(1, 100))):
        run_dir = Path(results) / f"{stamp}{suffix}"
        try:
            run_dir.mkdir(parents=True)
            return run_dir
        except FileExistsError:
            continue
    raise RuntimeError(f"{results}: 100 runs in one second")


@dataclass
class RunHeader:
    """What the run was, independent of any single test."""
    astrald_ref: str
    astral_py_ref: str
    host: str
    sandbox: str
    hermetic: bool = True
    # why: the report has to say what was run, not only how it went. These
    # default so a caller that only cares about the records stays unchanged.
    selection: str = ""
    target: str = "fresh"


class RunResults:
    def __init__(self, dir: Path, header: RunHeader):
        self.dir = Path(dir)
        self.dir.mkdir(parents=True, exist_ok=True)
        self._t0 = time.monotonic()
        self.header = {
            "astrald_ref": header.astrald_ref,
            # why: the astral-py checkout is mutable (config.toml path), so a
            # result is only reproducible if the run pins what it imported.
            "astral_py_ref": header.astral_py_ref,
            "host": header.host,
            "sandbox": header.sandbox,
            "hermetic": header.hermetic,
            "selection": header.selection,
            "target": header.target,
            "started": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
            "wall_time_s": 0.0,
        }
        self.entries = []

    def record(self, *, test: str, kind: str, env: str, driver: str,
               status: str, duration_s: float, artifacts: str,
               failure_kind: str | None = None) -> None:
        if status not in STATUSES:
            raise ValueError(f"bad status {status!r}")
        if kind not in KINDS:
            raise ValueError(f"bad kind {kind!r}")
        if env not in ENVS:
            raise ValueError(f"bad env {env!r}")
        if status == "fail":
            if failure_kind not in FAILURE_KINDS:
                raise ValueError(f"fail requires failure_kind, got {failure_kind!r}")
        elif failure_kind is not None:
            raise ValueError("failure_kind only valid on fail")
        entry = {"test": test, "kind": kind, "env": env, "driver": driver,
                 "status": status, "duration_s": round(duration_s, 3),
                 "artifacts": artifacts}
        if failure_kind:
            entry["failure_kind"] = failure_kind
        self.entries.append(entry)
        with (self.dir / "events.jsonl").open("a") as f:
            f.write(json.dumps(entry) + "\n")

    def finalize(self) -> int:
        self.header["wall_time_s"] = round(time.monotonic() - self._t0, 3)
        doc = dict(self.header, entries=self.entries)
        (self.dir / "results.json").write_text(json.dumps(doc, indent=2) + "\n")
        (self.dir / "report.md").write_text(report.render(doc, self.dir.name))
        self.doc = doc
        return 0 if all(e["status"] == "pass" for e in self.entries) else 1
