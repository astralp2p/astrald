"""report.md — the run as one document a person can read or present.

why: results.json is the stable machine output and events.jsonl is the live
stream, but neither answers "how did the run go?" without a reader that knows
the schema. The report is that answer in one page: the verdict, what was run,
every test, and — when something is red — which of the three layers broke and
where its log is.
"""

# why: a failure_kind is a verdict about WHICH layer broke, so the report
# spells it out rather than making the reader remember the vocabulary.
KIND_MEANING = {
    "driver": "the flow never happened — the driver could not perform it",
    "verify": "astrald misbehaved — the flow ran and the oracle rejected it",
    "environment": "the world broke — no verdict about astrald is possible",
}

STATUS_MARK = {"pass": "pass", "fail": "FAIL", "skipped": "skip"}


def _table(rows: list[list[str]], head: list[str]) -> list[str]:
    out = ["| " + " | ".join(head) + " |",
           "|" + "|".join("---" for _ in head) + "|"]
    out += ["| " + " | ".join(r) + " |" for r in rows]
    return out


def render(doc: dict, run_dir_name: str) -> str:
    """The whole run as markdown. Pure: takes the results doc, returns text."""
    entries = doc["entries"]
    tests = [e for e in entries if e["kind"] == "test"]
    passed = [e for e in entries if e["status"] == "pass"]
    failed = [e for e in entries if e["status"] == "fail"]
    skipped = [e for e in entries if e["status"] == "skipped"]

    # why: a skipped test is not a pass — the run reports no verdict about it,
    # so the document must not read as green.
    verdict = "FAIL" if failed else ("INCOMPLETE" if skipped else "PASS")

    envs = sorted({e["env"] for e in entries}) or ["-"]
    drivers = sorted({e["driver"] for e in entries}) or ["-"]
    target = doc.get("target", "fresh")

    lines = [f"# Integration test run — {verdict}", ""]

    tally = f"**{len(passed)} of {len(entries)} passed**"
    if failed:
        tally += f" · {len(failed)} failed"
    if skipped:
        tally += f" · {len(skipped)} skipped"
    lines += [f"{tally} · {doc['wall_time_s']:.2f} s · {doc['started']}", ""]

    lines += _table([
        ["Selection", f"`{doc.get('selection') or 'main.suite'}`"],
        ["Environment", ", ".join(envs)],
        ["Driver", ", ".join(drivers)],
        ["Target", f"{target} ({'hermetic' if doc['hermetic'] else 'not hermetic'})"],
        ["Host", doc["host"]],
        ["astrald", f"`{doc['astrald_ref']}`"],
        ["astral-py", f"`{doc['astral_py_ref']}`"],
    ], ["", ""])
    lines += [""]

    lines += ["## Tests", ""]
    rows = []
    for e in entries:
        note = e.get("failure_kind", "")
        if e["kind"] == "fixture":
            note = (note + " " if note else "") + "_(fixture)_"
        rows.append([e["test"], e["env"], e["driver"],
                     STATUS_MARK[e["status"]], f"{e['duration_s']:.2f} s",
                     note])
    head = ["Test", "Env", "Driver", "Result", "Time", "Note"]
    # why: an all-green run has nothing to note, and an empty trailing column
    # reads as a missing value rather than as "nothing to say".
    if not any(r[-1] for r in rows):
        head = head[:-1]
        rows = [r[:-1] for r in rows]
    lines += _table(rows, head)
    lines += [""]

    if failed:
        lines += ["## Failures", ""]
        for e in failed:
            kind = e.get("failure_kind", "?")
            lines += [f"### {e['test']} — {kind}", "",
                      KIND_MEANING.get(kind, ""), "",
                      f"Logs: `{run_dir_name}/{e['artifacts']}`", ""]

    if skipped:
        names = ", ".join(f"`{e['test']}`" for e in skipped)
        lines += ["## Skipped", "",
                  "A test is skipped when the state it starts from was never "
                  "built, so the run reports no verdict about it: " + names, ""]

    lines += ["## Artifacts", "",
              f"Full results: `{run_dir_name}/results.json` · "
              f"event stream: `{run_dir_name}/events.jsonl` · "
              f"per-test driver and oracle logs, and each node's astrald log, "
              f"sit under `{run_dir_name}/`.", ""]

    return "\n".join(lines)


def summary_line(doc: dict) -> str:
    """One line for the terminal — the same verdict, without the document."""
    entries = doc["entries"]
    passed = sum(1 for e in entries if e["status"] == "pass")
    failed = sum(1 for e in entries if e["status"] == "fail")
    skipped = sum(1 for e in entries if e["status"] == "skipped")
    verdict = "FAIL" if failed else ("INCOMPLETE" if skipped else "PASS")
    parts = [f"{passed} passed"]
    if failed:
        parts.append(f"{failed} failed")
    if skipped:
        parts.append(f"{skipped} skipped")
    return f"{verdict} — {', '.join(parts)} in {doc['wall_time_s']:.2f}s"
