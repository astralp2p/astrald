#!/usr/bin/env python3
"""Driver: resolve node2's configured endpoint on node1 and dial it."""
import asyncio

import astral
import astral.api.endpoints  # noqa: F401  registers the endpoint types

from lib.sessionio import load, write_facts

CONFIGURED = "198.51.100.12:1791"


def rows(eps):
    return [[e.endpoint.network(), e.endpoint.address(), e.ttl] for e in eps]


async def main():
    doc = load()
    n1, n2 = doc["nodes"]["node1"], doc["nodes"]["node2"]
    token = doc["facts"]["user_token"]
    lan = n2.get("lan_endpoint") or ""
    oct_ = lan.rsplit(":", 1)[0].rsplit(".", 1)[-1]
    if not CONFIGURED.startswith(f"198.51.100.{oct_}:"):
        raise SystemExit(f"driver: node2 LAN endpoint {lan!r} does not match "
                         f"the configured alias {CONFIGURED}")

    async with await astral.connect(n2["endpoint"], token=n2["token"]) as c:
        own = rows(await c.nodes.resolve_endpoints(n2["identity"],
                                                   experimental=True))
    print(f"driver: node2 resolves itself: {own}")

    async with await astral.connect(n1["endpoint"], token=token) as c:
        seen = rows(await c.nodes.resolve_endpoints(n2["identity"],
                                                    experimental=True))
        print(f"driver: node1 resolves node2: {seen}")
        info = await c.nodes.new_link(n2["identity"],
                                      endpoint=f"tcp:{CONFIGURED}",
                                      experimental=True, timeout=120)
        link = {"network": info.network,
                "remote": info.remote_endpoint.address()
                if info.remote_endpoint is not None else None,
                "remote_identity": str(info.remote_identity)}
        print(f"driver: node1 linked to node2: {link}")

    write_facts({"resolved_on_node1": seen, "resolved_on_node2": own,
                 "dial_link": link})


asyncio.run(main())
