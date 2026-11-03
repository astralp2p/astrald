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


def verdict(doc: dict) -> str:
    """PASS, FAIL or INCOMPLETE — the one verdict every reader prints.

    why: a skipped test is not a pass — the run reports no verdict about it,
    so the run must not read as green. A run that ended on an exception or
    Ctrl-C is not one either, even when every test it recorded passed.
    """
    statuses = {e["status"] for e in doc["entries"]}
    if "fail" in statuses:
        return "FAIL"
    if "skipped" in statuses or doc.get("aborted"):
        return "INCOMPLETE"
    return "PASS"


def render(doc: dict, run_dir_name: str) -> str:
    """The whole run as markdown. Pure: takes the results doc, returns text."""
    entries = doc["entries"]
    tests = [e for e in entries if e["kind"] == "test"]
    passed = [e for e in entries if e["status"] == "pass"]
    failed = [e for e in entries if e["status"] == "fail"]
    skipped = [e for e in entries if e["status"] == "skipped"]

    envs = sorted({e["env"] for e in entries}) or ["-"]
    drivers = sorted({e["driver"] for e in entries}) or ["-"]
    target = doc.get("target", "fresh")

    lines = [f"# Integration test run — {verdict(doc)}", ""]
    if doc.get("aborted"):
        lines += [f"The run ended early: {doc['aborted']}.", ""]

    tally = f"**{len(passed)} of {len(entries)} passed**"
    if failed:
        tally += f" · {len(failed)} failed"
    if skipped:
        tally += f" · {len(skipped)} skipped"
    lines += [f"{tally} · {doc['wall_time_s']:.2f} s · {doc['started']}", ""]

    driver_cell = ", ".join(drivers)
    # why: when an AI operator drove the run, which model it was is the first
    # thing a reader asks — a red under one model says nothing about another.
    if doc.get("agent_model"):
        driver_cell += f" (`{doc['agent_model']}`)"

    lines += _table([
        ["Selection", f"`{doc.get('selection') or 'main.suite'}`"],
        ["Environment", ", ".join(envs)],
        ["Driver", driver_cell],
        ["Target", f"{target} ({'hermetic' if doc['hermetic'] else 'not hermetic'})"],
        ["Host", doc["host"]],
        ["astrald", f"`{doc['astrald_ref']}`"],
        ["astral-py", f"`{doc['astral_py_ref']}`"],
    ], ["", ""])
    lines += [""]

    lines += ["## Tests", ""]
    # why only for several: one world says nothing a reader needs, and runs
    # from before worlds carry none
    worlds = len({e.get("world") for e in entries}) > 1
    rows = []
    for e in entries:
        note = e.get("failure_kind", "")
        if e["kind"] == "prereq":
            note = (note + " " if note else "") + "_(prereq)_"
        rows.append([e["test"]] + ([e.get("world", "")] if worlds else [])
                    + [e["env"], e["driver"], STATUS_MARK[e["status"]],
                       f"{e['duration_s']:.2f} s", note])
    head = (["Test"] + (["World"] if worlds else [])
            + ["Env", "Driver", "Result", "Time", "Note"])
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
            # why: the kind says which layer broke, the phase which step it
            # broke in. A driver or verify failure happens in the step of the
            # same name, so only an environment failure needs the phase
            # spelled out: it can break any step.
            phase = e.get("phase")
            at = f" in {phase}" if phase and phase != kind else ""
            lines += [f"### {e['test']} — {kind}{at}", "",
                      KIND_MEANING.get(kind, ""), "",
                      f"Logs: `{run_dir_name}/{e['artifacts']}`", ""]

    if skipped:
        lines += ["## Skipped", "",
                  "A skipped test did not finish, so the run reports no "
                  "verdict about it.", ""]
        lines += [f"- `{e['test']}`"
                  + (f" ({e['world']})" if worlds and e.get("world") else "")
                  + (f" — {e['reason']}" if e.get("reason") else "")
                  for e in skipped]
        lines += [""]

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
    parts = [f"{passed} passed"]
    if failed:
        parts.append(f"{failed} failed")
    if skipped:
        parts.append(f"{skipped} skipped")
    line = f"{verdict(doc)} — {', '.join(parts)} in {doc['wall_time_s']:.2f}s"
    if doc.get("aborted"):
        line += f" (ended early: {doc['aborted']})"
    return line
