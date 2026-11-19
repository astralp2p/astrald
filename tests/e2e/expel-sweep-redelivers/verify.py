#!/usr/bin/env python3
"""Oracle: node1's expulsion sweep gave node2 back a ban node2 had lost,
over the link node2 already held.

- the driver deleted exactly one ban row on node2;
- node2, asked with its own token, lists the ban on the subject, issued by the
  User, and it came back within RECOVER seconds of the deletion;
- every link node2 holds to node1 is one the driver recorded before the
  deletion: no new link, so no link event, carried the ban; the sweep did.
"""
import asyncio

import astral

from lib.sessionio import load

RECOVER = 30.0     # the driver's budget: a 5 s sweep period plus margin


def bans(expelled) -> dict:
    return {str(e.expulsion.subject): str(e.expulsion.issuer)
            for e in expelled
            if e.expulsion is not None and e.expulsion.subject is not None}


async def main():
    doc = load()
    n1, n2 = doc["nodes"]["node1"], doc["nodes"]["node2"]
    facts = doc["facts"]
    subject, user_id = facts["subject_id"], facts["user_id"]
    print(f"oracle: driver facts deleted_rows={facts['deleted_rows']} "
          f"recovered_after={facts['recovered_after']} links_before={facts['links_before']}")

    assert facts["deleted_rows"] == 1, (
        f"the driver deleted {facts['deleted_rows']} ban rows on node2; want 1")
    assert facts["recovered_after"] is not None, (
        f"node2 did not list the ban on {subject} again within {RECOVER} s")
    assert facts["recovered_after"] <= RECOVER

    async with await astral.connect(n2["endpoint"], token=n2["token"]) as c:
        held = bans(await c.user.list_expelled())
        links_now = {str(l.id) for l in await c.nodes.links(experimental=True)
                     if str(l.remote_identity) == n1["identity"]}

    assert held.get(subject) == user_id, (
        f"node2 holds no ban on {subject} issued by the User {user_id}: {held}")
    new = links_now - set(facts["links_before"])
    assert not new, (
        f"node2 holds new link(s) {sorted(new)} to node1; the ban may have come "
        "with a link event rather than the sweep")
    print(f"oracle: node2 got the ban back after {facts['recovered_after']} s "
          f"over its existing link(s) {sorted(links_now)}")


asyncio.run(main())
