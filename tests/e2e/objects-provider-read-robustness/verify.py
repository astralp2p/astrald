#!/usr/bin/env python3
"""Oracle: bad candidates neither win a read nor stall it past node1's timers,
find names each identity once, and a liar's SourceID is replaced.

Ground truth comes from the nodes, through astral-py: node2's `local`
repository holds O1 and its bytes hash to O1; node1's `local` does not hold
it, so a read of O1 on node1 has to cross the link; neither node's `main`
holds O2. Then the oracle judges the driver's measurements against its own
bounds:

- G (silent reader) and H (hanging finder) each hold their query until a 15 s
  timer of node1 ends it. read(O1) under 10 s is therefore met only by a
  concurrent winner, and read(O2) under 25 s only by a node that ends both
  waits.
- With H answering at once and no silent candidate, read(O2) is rejected
  plainly (code 1) under 8 s: D (no handler), R (known to no node), node1 and node2 all fail
  promptly.
- find(O1) names no identity twice and node2 once, although node1's swarm
  finder and F (twice) all name node2. F's claims are relayed unverified, so
  G and R are named too.
- G is dialed once for O2 although F claimed it twice, and node1 closes that
  silent attempt within 20 s. The caller cannot see this: objects.read is
  answered before the 15 s timer runs out (see README), so G measures it.
- The descriptor carrying L's label, and the search result for X, each come
  once and carry L's identity, not the node2 identity L put on them.

Last, the oracle stores a fresh O1' on node2 and reruns the whole flow with
fresh apps (`driver verify`), every check binding.
"""
import asyncio
import os
import subprocess
import sys
from pathlib import Path

import astral
from astral.objectid import object_id_of_bytes

from lib.sessionio import as_bytes, load

ROOT = Path(__file__).resolve().parents[3]
DEADLINE = 15

READ_O1_BOUND = 10
FIND_BOUND = 25
READ_O2_HANG_BOUND = 25
READ_O2_BOUND = 8
HOLD_BOUND = 20
CODE_REJECTED = 1  # objects.read answers every failure with a plain rejection


async def rejected(coro) -> bool:
    try:
        await coro
    except astral.QueryRejected:
        return True
    return False


async def ground_truth(doc: dict, o1: str, o2: str) -> int:
    n1, n2 = doc["nodes"]["node1"], doc["nodes"]["node2"]
    async with await astral.connect(n2["endpoint"], token=n2["token"]) as c:
        held = as_bytes(await c.objects.read(o1, repo="local", timeout=DEADLINE))
        assert str(object_id_of_bytes(held)) == o1, (
            f"node2's local copy of O1 hashes to {object_id_of_bytes(held)}, not {o1}")
        assert await rejected(c.objects.read(o2, repo="main", zone="dv", timeout=DEADLINE)), (
            "node2's main repository served O2, which nobody stores")
    async with await astral.connect(n1["endpoint"], token=doc["facts"]["user_token"]) as c:
        assert await rejected(c.objects.read(o1, repo="local", timeout=DEADLINE)), (
            "node1's local repository holds O1, so a read of O1 need not cross the link")
        assert await rejected(c.objects.read(o2, repo="main", zone="dv", timeout=DEADLINE)), (
            "node1's main repository served O2, which nobody stores")
    return len(held)


def judge_find(f: dict, who: dict) -> None:
    named = [f["names"].get(i, i) for i in f["find_a"]]
    call = f["find_a_call"]
    assert call["err"] == "", f"find(O1) failed: {call['err']}"
    repeats = sorted({n for n in named if named.count(n) > 1})
    assert not repeats, f"find(O1) named {repeats} more than once: {named}"
    assert named.count("node2") == 1, f"find(O1) named node2 {named.count('node2')} times: {named}"
    assert "G" in named and "R" in named, (
        f"find(O1) did not relay F's unverified claims G and R: {named}")
    assert call["seconds"] < FIND_BOUND, (
        f"find(O1) took {call['seconds']:.1f} s, past {FIND_BOUND} s; H holds it about 15 s")


def judge_reads(f: dict, held_len: int) -> None:
    r = f["read_o1"]
    assert r["err"] == "" and f["read_o1_is_o1"] and f["read_o1_bytes"] == held_len, (
        f"read(O1) answered {f['read_o1_bytes']} B (hash to O1: {f['read_o1_is_o1']}), "
        f"err {r['err']!r}; want node2's {held_len} B")
    assert r["seconds"] < READ_O1_BOUND, (
        f"read(O1) took {r['seconds']:.1f} s, past {READ_O1_BOUND} s: node2 did not win "
        f"while G and the rest were still pending")
    a = f["read_o2_a"]
    assert a["rejected"], f"read(O2) with G and H in play was not rejected: {a['err']!r}"
    assert a["seconds"] < READ_O2_HANG_BOUND, (
        f"read(O2) with G and H in play took {a['seconds']:.1f} s, past {READ_O2_HANG_BOUND} s")
    assert f["g_read_o2"] == 1, (
        f"G was dialed {f['g_read_o2']} times for O2; F claimed it twice, want once")
    assert 0 <= f["g_hold_o2_s"] < HOLD_BOUND, (
        f"node1 held G's silent read of O2 for {f['g_hold_o2_s']:.1f} s (-1: never "
        f"closed); its first-byte timer must close it under {HOLD_BOUND} s")
    b = f["read_o2_b"]
    assert b["rejected"] and b["code"] == CODE_REJECTED, (
        f"read(O2) with node1, node2, D and R ended with {b['err']!r}; want a plain "
        f"rejection, code {CODE_REJECTED}")
    assert b["seconds"] < READ_O2_BOUND, (
        f"read(O2) with node1, node2, D and R took {b['seconds']:.1f} s, past {READ_O2_BOUND} s")


def judge_stamps(f: dict, who: dict) -> None:
    show = lambda ids: [f["names"].get(i, i) for i in ids]
    assert f["describe_err"] == "", f"describe(O1) failed: {f['describe_err']}"
    assert f["describe_sources"] == [who["L"]], (
        f"the descriptors carrying L's label came from {show(f['describe_sources'])}; want L once")
    assert f["search_err"] == "", f"search failed: {f['search_err']}"
    assert f["search_sources"] == [who["L"]], (
        f"the search results for X came from {show(f['search_sources'])}; want L once")


async def store_fresh_o1(doc: dict) -> str:
    n1, n2 = doc["nodes"]["node1"], doc["nodes"]["node2"]
    blob = b"objects-provider-read-robustness O1' " + os.urandom(16).hex().encode()
    async with await astral.connect(n1["endpoint"], token=doc["facts"]["user_token"]) as c:
        async with c.objects.create(target=n2["identity"]) as w:
            await w.write(blob)
            return str(await w.commit())


def main():
    doc = load()
    f = doc["facts"]["objects_provider_read_robustness"]
    who = {name: ident for ident, name in f["names"].items()}

    held_len = asyncio.run(ground_truth(doc, f["o1"], f["o2"]))
    judge_find(f, who)
    judge_reads(f, held_len)
    judge_stamps(f, who)
    failed = [k for k, ok in f["checks"].items() if not ok]
    assert not failed, f"driver checks that did not hold: {failed}"

    fresh = asyncio.run(store_fresh_o1(doc))
    proc = subprocess.run(
        ["go", "run", "./tests/e2e/objects-provider-read-robustness/driver", "verify",
         os.environ["ASTRAL_TESTS_SESSION"], fresh],
        cwd=ROOT, capture_output=True, text=True,
        env={**os.environ, "GOFLAGS": os.environ.get("GOFLAGS", "-buildvcs=false")},
    )
    sys.stderr.write(proc.stderr)
    assert proc.returncode == 0, f"the flow with fresh apps and O1' failed: exit {proc.returncode}"

    print(f"oracle: read(O1) {f['read_o1']['seconds']:.1f} s from node2; find(O1) "
          f"{f['find_a_call']['seconds']:.1f} s, each identity once; read(O2) rejected in "
          f"{f['read_o2_a']['seconds']:.1f} s past G and H, {f['read_o2_b']['seconds']:.1f} s "
          f"without them; G dialed once; L's descriptor and result restamped to L")


main()
