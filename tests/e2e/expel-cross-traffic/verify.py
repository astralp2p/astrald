#!/usr/bin/env python3
"""Oracle: after a ban, no relay push, discovery or relayed query crosses it.

From the driver's facts (its own reads of each node and the node logs it
tapped):

- setup: both swarm follows held node1's provider before the ban, node2 named
  node3 as the host of node3's app, and node3's carry to node1 and node2's
  carry to node3 ran (a services.discover entry each), so the paths judged
  below were live;
- the tap of every node stayed whole across the flow;
- every node received the ban: node1 logged the expulsion, node2 and node3
  logged the removal of their former siblings;
- from GRACE after the ban on each node: node1 and node2 log no
  services.discover with node3, and no relay contract pushed by node3
  rejected; node3 logs no services.discover with node1 or node2, and no relay
  contract pushed by either rejected (acceptance criteria 1 and 2);
- once the ban reached node2: apphost.hosts on node2 names node1 for node1's
  app and nothing for node3's app; node1's app answers node2 within 5 s; the
  query to node3's app fails, node3's app serves nothing, and node2 opens no
  link to node3 while it routes (A2G8; acceptance criterion 7, for one app on
  each node, since one app identity is hosted on one node: astral-docs
  topics/app-routing.md, The contract an app signs);
- links: node1 logged node3's answer to its ban within the window, since the
  expelling node pushes the ban to node3 until node3 answers (astral-docs
  protocols/user/README.md:142-152; astrald mod/user/src/expel.go Expel,
  onBanAnswered). On node1, and on node2 when it logged the answer,
  from GRACE after the answer the node logs no new link with node3, and at
  the end of the 30 s window it lists no link to node3 (the same section: a
  node admits an expelled node's links until that node answers a push of its
  own ban from this node; astrald mod/user/src/link_admitter.go
  onBanAnswered closes them). node2 pushes the ban to node3 only on a new
  link or in its hourly sweep (mod/user/src/sync.go syncExpulsions,
  reconcile_task.go), so a node2 that logged no answer is judged on pushes
  and discovery alone: the spec admits its links.
"""
from lib.sessionio import load

# why a grace: a push or a discovery already in flight when the ban lands
# ends after it, and only a new one is a failure.
GRACE = 1.0
REACH = 5.0          # acceptance criterion 7: a member host answers within 5 s
PUSH_REJECTED = "rejecting pushed contract from "
# astrald mod/user/src/link_admitter.go onBanAnswered
BAN_ANSWERED = " answered its ban, refusing its links"
# astrald mod/nodes/src/link_pool.go: "added %v-link with %v (%v)", logged
# once the link passed admission
LINK_ADDED = ("added ", "-link with ")


def first(entries: list, *needles: str) -> float | None:
    for e in entries:
        if all(n in e["text"] for n in needles):
            return e["t"]
    return None


def last(entries: list, *needles: str) -> float | None:
    t = None
    for e in entries:
        if all(n in e["text"] for n in needles):
            t = e["t"]
    return t


def crossing(entries: list, since: float, peers: list) -> list:
    """Entries at or after since that tell a discovery with a peer or a
    rejected push from a peer."""
    out = []
    for e in entries:
        if e["t"] < since:
            continue
        text = e["text"]
        discovery = "services.discover" in text and any(p in text for p in peers)
        push = any(PUSH_REJECTED + p in text for p in peers)
        if discovery or push:
            out.append(e)
    return out


def judge_log(log: dict, id1: str, id2: str, id3: str) -> None:
    for node, state in log["taps"].items():
        assert state["refused"] is None and state["broken"] is None, (
            f"{node}'s log tap is incomplete: {state}")

    n1, n2, n3 = log["node1"], log["node2"], log["node3"]
    assert first(n3, "services.discover", id1) is not None, (
        "node3 logged no services.discover with node1 before the ban, so "
        "its follow's carry never ran and the check below proves nothing")
    assert first(n2, "services.discover", id3) is not None, (
        "node2 logged no services.discover with node3 before the ban, so "
        "its follow's carry never ran and the check below proves nothing")

    bans = {"node1": first(n1, "expelled", id3),
            "node2": first(n2, "removing sibling", id3),
            "node3": max([t for t in (last(n3, "removing sibling", id1),
                                      last(n3, "removing sibling", id2))
                          if t is not None], default=None)}
    for node, t in bans.items():
        assert t is not None, f"{node} logged no ban of node3: {bans}"

    for node, entries, peers in (("node1", n1, [id3]), ("node2", n2, [id3]),
                                 ("node3", n3, [id1, id2])):
        late = crossing(entries, bans[node] + GRACE, peers)
        assert not late, (
            f"{node} logged a discovery or a rejected relay push across the "
            f"ban, more than {GRACE} s after it landed there: "
            f"{[(round(e['t'] - bans[node], 3), e['text'][:200]) for e in late]}")


def judge_links(log: dict, id3: str) -> list:
    """node1, and node2 when it logged node3's ban answer: from GRACE after
    the answer, no new link with node3, and none listed at the window's end."""
    judged = []
    for node in ("node1", "node2"):
        answered = first(log[node], id3 + BAN_ANSWERED)
        # why node1 must answer: the expelling node pushes the ban to node3
        # until node3 answers (protocols/user/README.md:142-152;
        # mod/user/src/expel.go Expel, onBanAnswered)
        assert answered is not None or node != "node1", (
            "node1, the expelling node, logged no answer of node3 to its ban "
            "within the window, so its links to node3 were never refused")
        if answered is None:
            continue
        judged.append(node)
        late = [e for e in log[node] if e["t"] >= answered + GRACE
                and all(n in e["text"] for n in LINK_ADDED) and id3 in e["text"]]
        assert not late, (
            f"{node} added a link with the expelled node3 more than {GRACE} s "
            f"after node3 answered its ban there: "
            f"{[(round(e['t'] - answered, 3), e['text'][:200]) for e in late]}")
        held = log["links_at_end"][node]
        assert not held, (
            f"{node} still listed link(s) {held} to the expelled node3 at the "
            f"end of the window, after node3 answered its ban there")
    return judged


def judge_routing(routing: dict, id1: str, id3: str) -> None:
    ready, probe = routing["ready"], routing["probe"]
    assert id3 in ready["hosts_before_node3_app"], (
        f"node2 named {ready['hosts_before_node3_app']} as hosts of node3's "
        "app before the ban; node3's relay contract never reached node2")
    assert routing["ban_on_node2"], "node2 never held the ban on node3"

    assert probe["hosts_app_node1"] == [id1], (
        f"node2's apphost.hosts named {probe['hosts_app_node1']} for node1's "
        f"app; want node1 alone")
    assert probe["hosts_app_node3"] == [], (
        f"node2's apphost.hosts named {probe['hosts_app_node3']} for the "
        "expelled node's app; want none")

    assert probe["app_node1_err"] == "" and probe["app_node1_seconds"] <= REACH, (
        f"node1's app did not answer node2 within {REACH} s: "
        f"{probe['app_node1_err']!r} after {probe['app_node1_seconds']:.2f} s")
    assert probe["app_node1_served_during_probe"] == 1, (
        f"node1's app served {probe['app_node1_served_during_probe']} "
        "queries during the probe; want 1")

    assert probe["app_node3_err"] != "", (
        "node2 reached the expelled node's app through node3 after the ban")
    assert probe["app_node3_served_during_probe"] == 0, (
        f"the expelled node's app served "
        f"{probe['app_node3_served_during_probe']} queries from node2 after "
        "the ban; want none")
    new = sorted(set(routing["links_after"]) - set(routing["links_before"]))
    assert not new, (
        f"node2 opened link(s) {new} to the expelled node3 while it routed "
        "the probe")


def main():
    doc = load()
    nodes, facts = doc["nodes"], doc["facts"]
    id1, id2, id3 = (nodes[n]["identity"] for n in ("node1", "node2", "node3"))
    assert facts["cross_traffic_expelled_id"] == id3, (
        f"expelled {facts['cross_traffic_expelled_id']}; want node3 {id3}")

    routing = facts["routing_probe"]
    print(f"oracle: probe {routing['probe']}")
    judge_routing(routing, id1, id3)
    judge_log(facts["cross_ban_log"], id1, id2, id3)
    judged = judge_links(facts["cross_ban_log"], id3)
    p = routing["probe"]
    print(f"oracle: node2 reached node1's app in {p['app_node1_seconds']:.2f} s, "
          f"found no route to the expelled node's app "
          f"({p['app_node3_err']!r} after {p['app_node3_seconds']:.2f} s), and "
          "no node logged a discovery or a rejected relay push across the ban")
    print(f"oracle: links judged on {judged} (node3 answered its ban "
          "there); none relinked node3 after the answer, none held a link at the end")


main()
