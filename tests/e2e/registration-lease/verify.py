#!/usr/bin/env python3
"""Oracle: a lease that is not renewed lapses, and one that is renewed holds.

Four independent claims, each falsifiable on its own.

1. The node granted a lease, and granted the one it was asked for. A node that
   answered an `ack` would have failed the driver already; one that answered a
   lease of some other length is not honouring the request it was given.

2. The node dialled `gone` exactly once -- during the describe that ran while
   its lease was live, and not during the one that ran after it lapsed.

   why a count and not a duration: a departed registrant is cheap to dial,
   because the route is simply not there, so both describes are prompt and
   timings separate nothing. Whether the node reached for the registrant at all
   is the difference the lease makes.

3. The node dialled `kept` on both describes. The second ran after `kept`'s
   first lease would have ended, so re-registering refreshed the existing entry
   rather than being ignored. Together with claim 2 this is what makes the test
   about the lease and not about the node forgetting registrations generally:
   the two registrants differ in nothing but renewal.

4. The node recorded `gone`'s expiry and said nothing about `kept`. The
   operator's only view of a registration disappearing is the log, so a silent
   removal is one nobody can debug -- and a log line for a registrant that is
   still registered would be worse than none.
"""
import re
from pathlib import Path

from lib.sessionio import load

ANSI = re.compile(r"\x1b\[[0-9;]*m")


def short(identity: str) -> str:
    """The node logs an identity as its first and last four bytes."""
    return f"{identity[:8]}:{identity[-8:]}"


def main():
    doc = load()
    f = doc["facts"]

    # 1. the lease is real, and is the one that was asked for
    assert f["granted_seconds"] == f["lease_seconds"], (
        f"granted {f['granted_seconds']}s for a request of {f['lease_seconds']}s: "
        f"a request under the node's maximum is granted unchanged")
    assert f["expires_at"], "the lease carries no ExpiresAt"
    assert f["renewed_expires_at"] > f["expires_at"], (
        f"renewal moved the expiry to {f['renewed_expires_at']}, not past the "
        f"original {f['expires_at']}")

    log = ANSI.sub("", (Path(doc["nodes"]["node1"]["root"]) / "astrald.log")
                   .read_text(errors="replace"))

    def dials(identity: str) -> list:
        tag = short(identity)
        return [ln for ln in log.splitlines()
                if f"-> {tag}" in ln and "objects.describe" in ln]

    gone, kept = dials(f["gone_identity"]), dials(f["kept_identity"])

    # 2. the unrenewed registrant fell out of the fan-out
    assert len(gone) == 1, (
        f"the node dialled the unrenewed describer {len(gone)} times, want 1: "
        f"once while its lease was live, never after it lapsed.\n"
        + "\n".join(gone))

    # the one dial is the control: it proves the registration really was in the
    # fan-out, so its absence afterwards is the lease working and not the
    # registration never having been there.
    assert "route not found" in gone[0], (
        f"the dial while leased did not fail as a departed registrant should: "
        f"{gone[0]}")

    # 3. the renewed registrant stayed in it
    assert len(kept) == 2, (
        f"the node dialled the renewed describer {len(kept)} times, want 2: "
        f"renewal refreshes the entry, so it is still there for the second "
        f"describe.\n" + "\n".join(kept))

    # 4. the expiry is recorded, and only for the one that expired
    assert re.search(rf"external describer {re.escape(short(f['gone_identity']))}"
                     rf": registration expired", log), (
        "the node log records no expiry for the unrenewed describer; an "
        "operator watching a registration disappear has nothing to read")
    assert not re.search(rf"external describer "
                         rf"{re.escape(short(f['kept_identity']))}"
                         rf": registration expired", log), (
        "the node logged an expiry for the renewed describer, which it kept "
        "dialling: the log and the fan-out disagree about what is registered")

    print(f"oracle: lease {f['granted_seconds']}s granted; unrenewed describer "
          f"dialled once then dropped and logged; renewed describer dialled "
          f"twice and never expired "
          f"({f['leased_seconds']:.3f}s / {f['lapsed_seconds']:.3f}s)")


main()
