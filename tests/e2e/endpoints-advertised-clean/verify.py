#!/usr/bin/env python3
"""Oracle: no bridge or link-local endpoint of node2 is advertised; its LAN and public endpoints are, with their TTLs.

From the `advertised_clean` fact:

- control: node2 holds the bridge address 172.17.0.1 and an fe80:: address
  among its interface addresses, so both filters had something to drop;
- neither node2's own rows nor node1's rows for node2, written after a
  fresh link, hold an endpoint in 172.17.0.0/16 or fe80::/10. astral-docs
  protocols/nodes/README.md, Announcing a change: "A node never advertises an
  IPv6 link-local address". The spec is silent on container bridges; the code
  drops the addresses of host-only interfaces (mod/ip/src/module.go LocalIPs,
  mod/ip/src/advertisable.go);
- both list node2's LAN tcp and kcp endpoints with a TTL of at most 2 h, and
  the configured public endpoint with a TTL above 2 h (astral-docs
  protocols/tcp/README.md, configEndpoints: 7 days for a configured entry, 2 h
  for an interface endpoint on a non-public address);
- node1's rows were written by the pull over the new link, and node1 holds no
  link with an end in either range.
"""
import ipaddress

from lib.sessionio import load

PRIVATE_TTL = 7200
PUBLIC = "tcp:203.0.113.5:40000"
BRIDGE = ipaddress.ip_network("172.17.0.0/16")
LINK_LOCAL = ipaddress.ip_network("fe80::/10")


def ip_of(endpoint: str | None):
    """The IP of a `<network>:<ip>:<port>` endpoint, or None for any other shape."""
    if not endpoint:
        return None
    host = endpoint.split(":", 1)[-1].rsplit(":", 1)[0].strip("[]").split("%")[0]
    try:
        return ipaddress.ip_address(host)
    except ValueError:
        return None


def banned(endpoint: str | None) -> bool:
    ip = ip_of(endpoint)
    return ip is not None and any(ip.version == n.version and ip in n
                                  for n in (BRIDGE, LINK_LOCAL))


def check_control(f: dict) -> None:
    addrs = f["local_addrs"]
    assert "172.17.0.1" in addrs, (
        f"node2 does not hold the bridge address 172.17.0.1 ({addrs}): add-bridge "
        "left nothing for the filter to drop")
    link_local = [a for a in addrs if ":" in a
                  and ipaddress.ip_address(a.split("%")[0]) in LINK_LOCAL]
    assert link_local, (
        f"node2 holds no fe80:: address ({addrs}): the link-local filter is not exercised")


def check_rows(who: str, rows: dict, lan: str) -> None:
    leaked = sorted(k for k in rows if banned(k))
    assert not leaked, f"{who} lists node2 at a bridge or link-local endpoint: {leaked}"
    lan_ip = ip_of(lan)
    for network in ("tcp", "kcp"):
        own = {k: t for k, t in rows.items()
               if k.startswith(f"{network}:") and ip_of(k) == lan_ip}
        assert own, f"{who} lists no {network} endpoint of node2 at its LAN address {lan_ip}: {rows}"
        long = {k: t for k, t in own.items() if t is None or t > PRIVATE_TTL}
        assert not long, (
            f"{who} lists node2's LAN endpoints {long} with a TTL above {PRIVATE_TTL} s; "
            "a private address is advertised for 2 h")
    assert PUBLIC in rows, f"{who} lists no {PUBLIC} for node2: {sorted(rows)}"
    ttl = rows[PUBLIC]
    assert ttl is None or ttl > PRIVATE_TTL, (
        f"{who} lists {PUBLIC} with a TTL of {ttl} s; a configured entry is advertised for 7 days")


def check_node1(f: dict) -> None:
    r, held = f["relink"], f["node1"]
    assert r["linked_at"] is not None, (
        f"node1 did not link to node2 again within the driver's wait after closing "
        f"{r['closed']}: {r['error']}")
    assert held is not None and held["fresh"], (
        f"node1's rows for node2 were not written by a pull over the new link "
        f"(at {r['linked_at']} s): {held}")
    check_rows("node1", held["rows"], f["lan_endpoint"])
    bad = [link for link in f["links"]
           if banned(link["local_endpoint"]) or banned(link["remote_endpoint"])]
    assert not bad, f"node1 holds a link to node2 on a bridge or link-local address: {bad}"


def main():
    f = load()["facts"]["advertised_clean"]
    check_control(f)
    check_rows("node2 for itself", f["own"], f["lan_endpoint"])
    check_node1(f)
    print(f"oracle: node2 holds {f['local_addrs']} and advertises {sorted(f['own'])}; "
          f"node1 holds {sorted(f['node1']['rows'])} after relinking at "
          f"{f['relink']['linked_at']} s; no bridge or link-local endpoint on either")


main()
