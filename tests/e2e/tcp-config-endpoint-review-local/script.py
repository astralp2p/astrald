#!/usr/bin/env python3
"""Driver: forward a port to node2, resolve it on node1, and dial it."""
import asyncio

import astral
import astral.api.endpoints  # noqa: F401  registers the endpoint types

from lib.sessionio import load, write_facts

FORWARD_PORT = 26791


def rows(eps):
    return [[e.endpoint.network(), e.endpoint.address(), e.ttl] for e in eps]


async def pipe(r, w):
    try:
        while data := await r.read(65536):
            w.write(data)
            await w.drain()
    except (ConnectionError, asyncio.CancelledError):
        pass
    finally:
        w.close()


async def main():
    doc = load()
    n1, n2 = doc["nodes"]["node1"], doc["nodes"]["node2"]
    token = doc["facts"]["user_token"]
    host_ip = n1["host_ip"]
    target = n2["lan_endpoint"].split(":", 1)[1]  # tcp:<ip>:<port>
    t_host, t_port = target.rsplit(":", 1)
    forwarded = f"{host_ip}:{FORWARD_PORT}"

    async def handle(cr, cw):
        ur, uw = await asyncio.open_connection(t_host, int(t_port))
        await asyncio.gather(pipe(cr, uw), pipe(ur, cw))

    server = await asyncio.start_server(handle, host_ip, FORWARD_PORT)
    print(f"driver: forwarding {forwarded} -> {target}")

    async with await astral.connect(n2["endpoint"], token=n2["token"]) as c:
        own = rows(await c.nodes.resolve_endpoints(n2["identity"],
                                                   experimental=True))
    print(f"driver: node2 resolves itself: {own}")

    async with await astral.connect(n1["endpoint"], token=token) as c:
        seen = rows(await c.nodes.resolve_endpoints(n2["identity"],
                                                    experimental=True))
        print(f"driver: node1 resolves node2: {seen}")
        info = await c.nodes.new_link(n2["identity"],
                                      endpoint=f"tcp:{forwarded}",
                                      experimental=True, timeout=60)
        link = {"network": info.network,
                "remote": info.remote_endpoint.address()
                if info.remote_endpoint is not None else None,
                "remote_identity": str(info.remote_identity)}
        print(f"driver: node1 linked to node2: {link}")

    server.close()
    write_facts({"resolved_on_node1": seen, "resolved_on_node2": own,
                 "dial_link": link})


asyncio.run(main())
