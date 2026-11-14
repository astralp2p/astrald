#!/usr/bin/env python3
"""Driver: move a live 32 MiB read onto a second link, started on the
serving node.

The world holds node1 and node2, adopted and linked (adopt-node). As the
User on node1, the driver stores a seeded 32 MiB object on node2
(lib/blobgen.py), opens a second link to node2 at node2's lan_endpoint, and
reads the object with a paced reader (64 KiB reads, 8 ms apart, 15 s stall
timeout, a sha256 at every MiB).

At 8 MiB the reader holds: the stream back-pressures to a standstill with
the session still open. The driver records both nodes' session rows for the
stream (link Ls), then calls nodes.migrate_session(start=true) on node2, with
node2's own token, onto the other link Lt. On node2 the session is inbound,
relayed for the User: its logical peer is the User, its link peer node1
(astrald mod/nodes/src/mux.go handleRelayQuery). Right after the answer it
records both nodes' rows again, releases the reader and reads to EOF. No link
is closed.

The driver judges nothing. It records what it saw for the oracle, and the
bytes it read as a length, a final digest and per-MiB running digests.
"""
import asyncio
import time

import astral

from lib import blobgen, linkview
from lib.blobgen import MiB
from lib.sessionio import load, write_facts

SIZE = 32 * MiB
MIGRATE_AT = 8 * MiB
# why: the stream must stand still with the session open before the move
HOLD = 1.0


async def main():
    doc = load()
    n1, n2 = doc["nodes"]["node1"], doc["nodes"]["node2"]
    id1, id2 = n1["identity"], n2["identity"]
    user_token = doc["facts"]["user_token"]
    seed = f"link-session-migrate-serving-side {time.time_ns()}"
    data = blobgen.blob(seed, SIZE)
    facts = {"seed": seed, "object_size": SIZE}

    async with await astral.connect(n1["endpoint"], token=n1["token"]) as c1, \
            await astral.connect(n2["endpoint"], token=n2["token"]) as c2, \
            await astral.connect(n1["endpoint"], token=user_token) as cu:
        # why first: the 32 MiB write rides the existing link, not Lt
        object_id = await blobgen.write_object(cu, data, target=id2)
        facts["object_id"] = object_id

        async def stream_rows(c) -> list:
            return linkview.carrying(await linkview.sessions(c), object_id)

        info = await cu.nodes.new_link(id2, endpoint=n2["lan_endpoint"],
                                       strategies="basic", experimental=True,
                                       timeout=60.0)
        new_id = str(info.id)
        facts["link_new_created"] = new_id
        facts["link_new_seen"] = await linkview.wait_both(c1, c2, id1, id2,
                                                          new_id)
        print(f"driver: stored {object_id[:20]}… on node2; second link "
              f"{new_id}, on both ends {facts['link_new_seen']}")

        mig: dict = {}
        async with cu.objects.open_read(object_id, target=id2) as stream:
            reader = blobgen.PacedReader(stream)
            task = asyncio.create_task(reader.run())
            mig["reached"] = await reader.reached(MIGRATE_AT, 60.0)
            reader.hold()
            await asyncio.sleep(HOLD)
            mig["offset"] = reader.total
            mig["rows1_before"] = await stream_rows(c1)
            mig["rows2_before"] = await stream_rows(c2)
            rows2 = mig["rows2_before"]
            print(f"driver: held at {reader.total} B; node1 rows "
                  f"{mig['rows1_before']}, node2 rows {rows2}")
            if not mig["reached"] or len(rows2) != 1:
                reader.release()
                task.cancel()
                facts.update(migrate=mig, stream=reader.facts())
                write_facts(facts)
                raise SystemExit(f"driver: no single stream session on node2 "
                                 f"at 8 MiB (rows {rows2})")
            session_id, old = rows2[0]["id"], rows2[0]["link_id"]
            others = [i for i in linkview.ids(await linkview.links(c2), id1)
                      if i != old]
            target = new_id if new_id in others else (sorted(others) or [None])[0]
            facts.update(session_id=session_id, link_old=old, link_new=target)
            if target is None:
                reader.release()
                task.cancel()
                facts.update(migrate=mig, stream=reader.facts())
                write_facts(facts)
                raise SystemExit(f"driver: node2 holds no second link beside "
                                 f"{old}")

            # why node2's own token: the serving node starts the move
            mig.update(await linkview.migrate(c2, session_id, target))
            mig["rows1_after"] = await stream_rows(c1)
            mig["rows2_after"] = await stream_rows(c2)
            print(f"driver: migrate on node2 {session_id} {old} → {target}: "
                  f"ok {mig['ok']} {mig.get('error')} in {mig['elapsed']} s; "
                  f"node1 rows {mig['rows1_after']}, node2 rows "
                  f"{mig['rows2_after']}")

            reader.release()
            await task
            st = reader.facts()
            if st["stalled"] or st["error"]:
                st["zombie1"] = await linkview.sessions(c1)
                st["zombie2"] = await linkview.sessions(c2)
            print(f"driver: read {st['total']} B, eof {st['eof']}, stalled "
                  f"{st['stalled']}, error {st['error']}, max gap "
                  f"{st['max_gap']} s")

    facts.update(migrate=mig, stream=st)
    write_facts(facts)


asyncio.run(main())
