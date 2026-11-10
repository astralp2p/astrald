#!/usr/bin/env python3
"""Driver: the User bans node2 while node2 is reading a large object from node1.

1. The User stores a large object on node1, in its `local` repository.
2. node2, a swarm member with its own token, opens a streaming read of it
   from node1 and takes the first megabyte at a throttled pace.
3. The User expels node2 on node1, with the read still open.
4. node2 keeps reading at the same pace until the stream ends, breaks or
   stalls. What happened is the `transfer` fact: recorded for the report,
   never judged. The docs do not say whether a stream authorized before the
   ban may finish.

The oracle judges the ban and a fresh read by its own calls.
"""
import asyncio
import hashlib
import time

import astral

from lib.sessionio import load, write_facts

SIZE = 32 * 1024 * 1024    # large enough that the link cannot buffer it whole
CHUNK = 256 * 1024
PACE = 0.05                # s between chunks: ~5 MB/s, a 6 s transfer
BEFORE = 1024 * 1024       # bytes read before the ban
STALL = 20.0               # one read may wait this long before the stream counts as stalled
BUDGET = 90.0              # the whole transfer, after the ban


def block(i: int) -> bytes:
    return hashlib.sha256(b"expel-during-transfer %d" % i).digest() * (CHUNK // 32)


async def main():
    doc = load()
    n1, n2 = doc["nodes"]["node1"], doc["nodes"]["node2"]
    user_token = doc["facts"]["user_token"]

    async with await astral.connect(n1["endpoint"], token=user_token) as c:
        async with c.objects.create(repo="local") as w:
            for i in range(SIZE // CHUNK):
                await w.write(block(i))
            object_id = str(await w.commit())

    got = 0
    transfer = {"size": SIZE, "ended": None}
    async with await astral.connect(n2["endpoint"], token=n2["token"]) as c2:
        async with c2.objects.open_read(object_id, target=n1["identity"]) as s:
            while got < BEFORE:
                data = await s.read_bytes(CHUNK, timeout=STALL)
                assert data, f"the stream ended after {got} B, before the ban"
                got += len(data)
                await asyncio.sleep(PACE)
            transfer["before_ban"] = got

            async with await astral.connect(n1["endpoint"],
                                            token=user_token) as c:
                signed = await c.user.expel(n2["identity"])
            expelled = str(signed.expulsion.subject)
            t0 = time.monotonic()

            while time.monotonic() - t0 < BUDGET:
                try:
                    data = await s.read_bytes(CHUNK, timeout=STALL)
                except Exception as e:  # noqa: BLE001 — a cut stream is the finding
                    transfer["ended"] = f"error: {type(e).__name__}: {e}"
                    break
                if not data:
                    transfer["ended"] = "eof"
                    break
                got += len(data)
                await asyncio.sleep(PACE)
            else:
                transfer["ended"] = "budget"
            transfer["after_ban_s"] = round(time.monotonic() - t0, 1)

    transfer["received"] = got
    transfer["complete"] = got == SIZE
    write_facts({"expelled_id": expelled, "object_id": object_id,
                 "transfer": transfer})
    print(f"driver: node2 {expelled[:16]}… expelled after {transfer['before_ban']} "
          f"of {SIZE} B; stream ended {transfer['ended']} with {got} B")


asyncio.run(main())
