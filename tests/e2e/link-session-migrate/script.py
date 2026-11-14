#!/usr/bin/env python3
"""Driver: move a live 32 MiB read onto a second link, started on the reader.

The world holds node1 and node2, adopted and linked (adopt-node). As the
User on node1, the driver:

1. stores a 32 MiB object on node2, generated from a seed (lib/blobgen.py);
2. opens a second link to node2 at node2's lan_endpoint: CreateLinkTask adds
   a new link whatever node1 already holds (astrald
   mod/nodes/src/create_link_task.go);
3. opens objects.read of the object on node2 and reads it with a paced reader
   (64 KiB reads, 8 ms apart, 15 s stall timeout, a sha256 at every MiB);
4. at 8 MiB, finds the stream's session in node1's nodes.sessions (link Ls)
   and node2's (the same id), and calls nodes.migrate_session(start=true)
   on node1 with the other link Lt;
5. right after the ack, records both nodes' session rows, both links'
   cumulative byte counters, and the error of a second migrate to Lt;
6. after 2 MiB more, closes Ls on node1 and reads to EOF.

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
CLOSE_AFTER = 2 * MiB
GONE_WAIT = 10.0


async def main():
    doc = load()
    n1, n2 = doc["nodes"]["node1"], doc["nodes"]["node2"]
    id1, id2 = n1["identity"], n2["identity"]
    user_token = doc["facts"]["user_token"]
    seed = f"link-session-migrate {time.time_ns()}"
    data = blobgen.blob(seed, SIZE)
    facts = {"seed": seed, "object_size": SIZE}

    async with await astral.connect(n1["endpoint"], token=n1["token"]) as c1, \
            await astral.connect(n2["endpoint"], token=n2["token"]) as c2, \
            await astral.connect(n1["endpoint"], token=user_token) as cu:
        before = await linkview.links(c1)
        facts["links_before"] = linkview.to(before, id2)
        print(f"driver: node1 holds {sorted(linkview.ids(before, id2))} to node2")

        # why first: the 32 MiB write rides the existing link, not Lt
        object_id = await blobgen.write_object(cu, data, target=id2)
        facts["object_id"] = object_id

        async def stream_rows(c) -> list:
            return linkview.carrying(await linkview.sessions(c), object_id)
        print(f"driver: stored {SIZE} B on node2 as {object_id[:20]}…")

        info = await cu.nodes.new_link(id2, endpoint=n2["lan_endpoint"],
                                       strategies="basic", experimental=True,
                                       timeout=60.0)
        new_id = str(info.id)
        facts["link_new_created"] = new_id
        facts["link_new_seen"] = await linkview.wait_both(c1, c2, id1, id2, new_id)
        print(f"driver: second link {new_id}, on both ends "
              f"{facts['link_new_seen']}")

        mig: dict = {}
        async with cu.objects.open_read(object_id, target=id2) as stream:
            reader = blobgen.PacedReader(stream)
            task = asyncio.create_task(reader.run())
            reached = await reader.reached(MIGRATE_AT, 60.0)
            mig["offset"] = reader.total
            rows1 = await stream_rows(c1)
            rows2 = await stream_rows(c2)
            lrows = await linkview.throughput(c1)
            mig.update(reached=reached, rows1_before=rows1,
                       rows2_before=rows2, links_before=lrows)
            print(f"driver: at {reader.total} B node1 rows {rows1}, "
                  f"node2 rows {rows2}")
            if not reached or len(rows1) != 1:
                task.cancel()
                facts["migrate"] = mig
                facts["stream"] = reader.facts()
                write_facts(facts)
                raise SystemExit(f"driver: no single stream session at 8 MiB "
                                 f"(reached={reached}, rows {rows1})")
            session_id, old = rows1[0]["id"], rows1[0]["link_id"]
            others = [r["id"] for r in linkview.to(lrows, id2) if r["id"] != old]
            target = new_id if new_id in others else (others or [None])[0]
            facts.update(session_id=session_id, link_old=old, link_new=target)
            if target is None:
                task.cancel()
                facts["migrate"] = mig
                facts["stream"] = reader.facts()
                write_facts(facts)
                raise SystemExit(f"driver: node1 holds no second link beside "
                                 f"{old}: {lrows}")

            mig.update(await linkview.migrate(cu, session_id, target))
            mig["ack_offset"] = reader.total
            # why at once: a migrating session is not listed (op_sessions.go)
            mig["rows1_after"] = await stream_rows(c1)
            mig["rows2_after"] = await stream_rows(c2)
            mig["links_after"] = await linkview.throughput(c1)
            again = await linkview.migrate(cu, session_id, target)
            mig["again"] = again
            print(f"driver: migrate {session_id} {old} → {target}: {mig}")

            close: dict = {}
            close_at = mig["ack_offset"] + CLOSE_AFTER
            close["reached"] = await reader.reached(close_at, 60.0)
            close["offset"] = reader.total
            close["links_before"] = await linkview.throughput(c1)
            if close["reached"]:
                try:
                    await cu.nodes.close_link(old, experimental=True)
                    close["result"] = "ack"
                except Exception as e:  # noqa: BLE001 — recorded
                    close["result"] = f"{type(e).__name__}: {e}"
                deadline = time.monotonic() + GONE_WAIT
                while (old in linkview.ids(await linkview.links(c1), id2)
                       and time.monotonic() < deadline):
                    await asyncio.sleep(0.25)
                close["gone"] = old not in linkview.ids(
                    await linkview.links(c1), id2)
            print(f"driver: close {old} at {close['offset']} B: "
                  f"{close.get('result')}, gone {close.get('gone')}")

            await task
            st = reader.facts()
            if st["stalled"] or st["error"]:
                st["zombie1"] = await linkview.sessions(c1)
                st["zombie2"] = await linkview.sessions(c2)
            print(f"driver: read {st['total']} B, eof {st['eof']}, stalled "
                  f"{st['stalled']}, error {st['error']}, max gap "
                  f"{st['max_gap']} s")

    facts.update(migrate=mig, close_old=close, stream=st)
    write_facts(facts)


asyncio.run(main())
