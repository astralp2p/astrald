#!/usr/bin/env python3
"""Oracle: three fresh nodes of one User form one swarm: the LAN pair by nearby, the third by one introduction.

From the `forms_and_finds` fact (times from node2's claim):

- the reflector moved to its 10.78.0 address before the claims;
- user.bootstrap claimed node1, node2 and the reflector for one User, and
  node1 and node2 shared no link just before node2's claim (an unowned node
  is never contacted, astral-docs protocols/user/README.md);
- node1 and node2 shared a link within LAN_WITHIN s of node2's claim, with
  no endpoint given (astral-docs protocols/nearby/README.md: a status every
  85-95 s; nearby-stealth-meet-ports' bound of two statuses and a dial), and
  node1 or node2 logged its nearby contact with the other
  (protocols/user/README.md, nearby contact);
- the reflector's `user.introduce(node1)` answered `ack`; the reflector
  shared a link with node1 within NODE1_WITHIN s of the answer
  (protocols/user/ops/user.introduce.md: a 30 s push wait) and with node2
  within NODE2_WITHIN s of it, with no endpoint of node2 given
  (protocols/nodes/README.md, Endpoint exchange in the local swarm: the
  pull on a pushed contract and the first-hand notice, each pull bounded to
  20 s). A reflector that met them through nearby before the introduction
  meets the bounds at once; the oracle says so;
- no node lists a `172.17.0.0/16` or `fe80::/10` endpoint for any node,
  itself included, and no link has an end in either range
  (protocols/nodes/README.md, Announcing a change: "A node never advertises
  an IPv6 link-local address"; the spec is silent on container bridges, the
  code drops their addresses, mod/ip/src/module.go LocalIPs and
  mod/ip/src/advertisable.go);
- every node's user.swarm_status lists exactly the three, the other two
  linked.
"""
import ipaddress
import re

from lib.sessionio import load

ROLES = ("node1", "node2", "reflector")
# why: two status intervals of at most 95 s and 10 s for the dial
LAN_WITHIN = 2 * 95 + 10
NODE1_WITHIN = 30.0
NODE2_WITHIN = 60.0
BANNED = (ipaddress.ip_network("172.17.0.0/16"), ipaddress.ip_network("fe80::/10"))
# mod/user/src/peer_seen.go: a contact that dials the peer
CONTACT = re.compile(r"introduced to nearby peer ([0-9a-f]{66})"
                     r"|sibling ([0-9a-f]{66}) is nearby at")


def banned(endpoint) -> bool:
    if not isinstance(endpoint, str):
        return False
    host = endpoint.split(":", 1)[-1].rsplit(":", 1)[0].strip("[]").split("%")[0]
    try:
        ip = ipaddress.ip_address(host)
    except ValueError:
        return False
    return any(ip.version == n.version and ip in n for n in BANNED)


def check_claims(f: dict) -> None:
    assert f["moved"]["moved"], (
        f"the reflector never held {f['target']}: {f['moved']['addrs']}")
    users = set(f["claims"].values())
    assert len(users) == 1, f"user.bootstrap claimed the nodes for different Users: {f['claims']}"
    assert f["before"] == [], f"node1 and node2 shared links {f['before']} before node2's claim"


def contacts(f: dict) -> list:
    """[(role, identity)] of every nearby contact line a role's log holds."""
    return sorted({(k, next(g for g in m.groups() if g)) for k, lines in f["log"].items()
                   for e in lines if (m := CONTACT.search(e["text"]))})


def check_lan(f: dict, ids: dict) -> None:
    at = f["lan"]["node1-node2"]
    assert at is not None, (
        f"node1 and node2 shared no link within the driver's wait; contact lines {f['log']}")
    assert at <= LAN_WITHIN, (
        f"node1 and node2 linked {at} s after node2's claim; want within {LAN_WITHIN} s")
    met = contacts(f)
    assert ("node1", ids["node2"]) in met or ("node2", ids["node1"]) in met, (
        f"node1 and node2 linked with no nearby contact line naming the other: {met}")
    print(f"oracle: node1 and node2 linked through nearby {at} s after node2's claim")


def check_introduce(f: dict, ids: dict) -> None:
    intro, found = f["introduce"], f["found"]
    assert intro["answer"] == ["ack"], (
        f"user.introduce(node1) on the reflector answered {intro['answer']}: {intro['detail']}")
    early = sorted(k for k in ("node1", "node2") if ids[k] in intro["before"])
    if early:
        print(f"oracle: the reflector held links to {early} before the introduction: "
              "it met them through nearby across the subnets")
    for peer, bound in (("node1", NODE1_WITHIN), ("node2", NODE2_WITHIN)):
        at = found[f"reflector-{peer}"]
        assert at is not None, f"the reflector shared no link with {peer} after the introduction"
        assert at - intro["acked_at"] <= bound, (
            f"the reflector linked to {peer} {at - intro['acked_at']:.1f} s after the "
            f"introduction answered; want within {bound} s")
    print(f"oracle: introduce acked at {intro['acked_at']} s; reflector linked {found}")


def check_clean(f: dict) -> None:
    for k in ROLES:
        for role, rows in f["endpoints"][k].items():
            assert isinstance(rows, list), f"{k} could not resolve {role}: {rows}"
            leaked = [e for e in rows if banned(e)]
            assert not leaked, f"{k} lists {role} at a bridge or link-local endpoint: {leaked}"
        bad = [link for link in f["links"][k]
               if banned(link["local_endpoint"]) or banned(link["remote_endpoint"])]
        assert not bad, f"{k} holds a link on a bridge or link-local address: {bad}"


def check_swarm(f: dict, ids: dict) -> None:
    want = set(ids.values())
    for k in ROLES:
        rows = f["swarm"][k]
        got = {m["identity"] for m in rows}
        assert got == want, f"{k}'s swarm_status lists {sorted(got)}; want the three {sorted(want)}"
        unlinked = [m["identity"] for m in rows if m["identity"] != ids[k] and not m["linked"]]
        assert not unlinked, f"{k}'s swarm_status lists {unlinked} unlinked"


def main():
    doc = load()
    ids = {k: doc["nodes"][k]["identity"] for k in ROLES}
    f = doc["facts"]["forms_and_finds"]
    check_claims(f)
    check_lan(f, ids)
    check_introduce(f, ids)
    check_clean(f)
    check_swarm(f, ids)
    print(f"oracle: one swarm of three; no bridge or link-local endpoint; "
          f"nearby contacts {contacts(f)}")


main()
