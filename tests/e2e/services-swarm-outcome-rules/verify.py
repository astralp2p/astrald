#!/usr/bin/env python3
"""Oracle: what discovery returns to a stranger and across the swarm.

The Go driver records, for node2's providers, how each asker's discovery came
back. The oracle judges those records against the current rules, then judges a
second set it asks the driver to record again (`driver verify`), made with
fresh app identities and fresh bindings.

why the oracle asks again: astral-py cannot speak the Services wire, so the
only way to see a discovery's outcome is through the Go client, and an oracle
that trusts only the driver's record would have no view of its own. The second
run also proves the driver left no binding behind: it advertises the same
names, and node2 refuses a name that is still bound.

The rules, from astral-docs protocols/services/ops/services.discover.md and
mod/services/src (op_discover.go, visibility.go):

* a caller that is not a swarm caller and holds no permit is not refused: it
  sees nothing, and reads the service in `services.not_granted`;
* a node that is a member of the swarm sees an open provider on another member;
* a discovery in an app's name (`for`) is accepted only from a node that may
  relay for that app, so a stranger's is refused with `error_message`;
* `reach=swarm` over a link is refused, so a stranger cannot make node1 carry
  its discovery to node2;
* a swarm member that reports a service incomplete makes the host's outcome
  incomplete for that service alone; a member that does not conclude inside the
  host's budget makes it incomplete for the member's unresolved services.
"""
import json
import os
import subprocess
import sys
from pathlib import Path

from lib.sessionio import load

ROOT = Path(__file__).resolve().parents[3]

OPEN, SLOW, GONE = "ssor-open", "ssor-slow", "ssor-gone"

# why: node1's initial budget is 5 s (coordinator.DefaultConfig). A host that
# gave up on a member waited for it; an earlier end is the member's own report.
BUDGET_FLOOR_MS = 4000

OUTCOMES = ("member_plain", "stranger_plain", "member_for_user",
            "stranger_for_user", "stranger_swarm", "swarm_answered",
            "swarm_silent", "swarm_lost")


def judge(obs: dict, label: str) -> None:
    assert set(OUTCOMES) <= set(obs), f"{label}: recorded {sorted(obs)}"
    held = f"{obs['provider']}/{OPEN}"

    def at(name: str) -> dict:
        return obs[name]

    def fail(name: str, what: str) -> str:
        return f"{label} {name}: {what}; recorded {json.dumps(obs[name])}"

    # A swarm member sees node2's open provider; nothing is refused or missing.
    o = at("member_plain")
    assert not o["error"] and o["complete"] and o["providers"] == [held] \
        and o["not_granted"] == [], fail(
            "member_plain", f"node1 asking node2 should see {held} alone, complete")

    # The stranger is not refused: it sees nothing and is told it is not granted.
    o = at("stranger_plain")
    assert not o["error"], fail(
        "stranger_plain", "a caller without a permit is no longer refused")
    assert o["providers"] == [], fail(
        "stranger_plain", "the stranger saw an offering of node2's open provider")
    assert o["not_granted"] == [OPEN] and o["complete"], fail(
        "stranger_plain", f"want services.not_granted [{OPEN}] and a bare eos")

    # node2 carries a discovery for the User from a member, not from a stranger.
    o = at("member_for_user")
    assert not o["error"] and o["complete"] and o["providers"] == [held] \
        and o.get("opens") == [True], fail(
            "member_for_user", f"node1 discovering for the User should get {held} tagged open")
    o = at("stranger_for_user")
    assert o["error"].startswith("not permitted"), fail(
        "stranger_for_user", "the stranger's discovery for the User should be refused as not permitted")
    assert o["providers"] == [] and not o["complete"], fail(
        "stranger_for_user", "the refused discovery returned offerings or an outcome")

    # A stranger cannot make node1 carry a discovery to the swarm.
    o = at("stranger_swarm")
    assert "reach=swarm" in o["error"], fail(
        "stranger_swarm", "reach=swarm over a link should be refused")
    assert o["providers"] == [] and not o["complete"], fail(
        "stranger_swarm", "the refused discovery returned offerings or an outcome")

    # An app on node1 sees node2's provider across the swarm, complete.
    o = at("swarm_answered")
    assert not o["error"] and o["complete"] and o["providers"] == [held] \
        and o["incomplete"] == [], fail(
            "swarm_answered", f"want {held} and a bare eos")

    # node2 does not conclude: node1 gives up at its budget. The service that
    # never answered is incomplete, and what node2 did offer is kept.
    o = at("swarm_silent")
    assert not o["error"] and not o["complete"], fail(
        "swarm_silent", "a member that never concludes should leave the outcome incomplete")
    assert SLOW in o["incomplete"], fail(
        "swarm_silent", f"{SLOW} never answered on node2 and must be named incomplete")
    assert set(o["incomplete"]) <= {OPEN, SLOW}, fail(
        "swarm_silent", "incomplete names a service nobody asked for")
    assert o["providers"] == [held], fail(
        "swarm_silent", f"the offering received before the budget ended ({held}) was lost")
    assert o["elapsed_ms"] >= BUDGET_FLOOR_MS, fail(
        "swarm_silent", f"ended after {o['elapsed_ms']} ms, before node1's budget")

    # node2 reports the service incomplete itself: only that service is named.
    o = at("swarm_lost")
    assert not o["error"] and not o["complete"], fail(
        "swarm_lost", "a service node2 lost while evaluating should leave the outcome incomplete")
    assert o["incomplete"] == [GONE], fail(
        "swarm_lost", f"want incomplete [{GONE}] alone: node2 resolved {OPEN}")
    assert o["providers"] == [held], fail(
        "swarm_lost", f"want the answered offering {held} kept")


def second_record() -> dict:
    proc = subprocess.run(
        ["go", "run", "./tests/e2e/services-swarm-outcome-rules/driver", "verify",
         os.environ["ASTRAL_TESTS_SESSION"]],
        cwd=ROOT, capture_output=True, text=True,
        env={**os.environ, "GOFLAGS": os.environ.get("GOFLAGS", "-buildvcs=false")},
    )
    sys.stderr.write(proc.stderr)
    assert proc.returncode == 0, (
        f"the oracle's own run failed (exit {proc.returncode}); a name still bound "
        f"by the driver fails here: {proc.stderr.strip()[-300:]}")
    lines = [l for l in proc.stdout.splitlines() if l.startswith("{")]
    assert lines, "the oracle's own run printed no record"
    return json.loads(lines[-1])


facts = load()["facts"].get("services_swarm_outcome_rules")
assert facts, "the driver recorded nothing"
judge(facts, "driver")
again = second_record()
judge(again, "oracle")
assert again["provider"] != facts["provider"] and again["consumer"] != facts["consumer"], (
    "the oracle's run reused the driver's identities")

print("oracle: a stranger linked to node2 sees nothing and reads not_granted, is refused a "
      "discovery for the User, and cannot start a swarm discovery on node1; a member sees "
      "node2's open provider; incomplete names the silent service, and only the lost "
      "one when node2 reports it — in the driver's run and in the oracle's own")
