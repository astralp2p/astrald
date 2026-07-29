"""results.json / events.jsonl writer — the system's one stable output."""
import json
import time
from pathlib import Path

STATUSES = {"pass", "fail", "skipped"}
FAILURE_KINDS = {"driver", "verify", "environment"}
KINDS = {"test", "fixture"}


class RunResults:
    def __init__(self, dir: Path, astrald_ref: str, host: str,
                 sandbox: str, lanes: list):
        self.dir = Path(dir)
        self.dir.mkdir(parents=True, exist_ok=True)
        self._t0 = time.monotonic()
        self.header = {
            "astrald_ref": astrald_ref,
            "host": host,
            "sandbox": sandbox,
            "lanes": list(lanes),
            "hermetic": True,
            "started": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
            "wall_time_s": 0.0,
        }
        self.entries = []

    def record(self, *, test: str, kind: str, lane: str, driver: str,
               status: str, duration_s: float, artifacts: str,
               failure_kind: str | None = None) -> None:
        if status not in STATUSES:
            raise ValueError(f"bad status {status!r}")
        if kind not in KINDS:
            raise ValueError(f"bad kind {kind!r}")
        if status == "fail":
            if failure_kind not in FAILURE_KINDS:
                raise ValueError(f"fail requires failure_kind, got {failure_kind!r}")
        elif failure_kind is not None:
            raise ValueError("failure_kind only valid on fail")
        entry = {"test": test, "kind": kind, "lane": lane, "driver": driver,
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
        return 0 if all(e["status"] == "pass" for e in self.entries) else 1
