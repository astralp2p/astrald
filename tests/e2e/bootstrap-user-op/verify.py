#!/usr/bin/env python3
"""Oracle: one user.bootstrap call claims a node, from given or generated words.

node1's first call must answer one mod.user.bootstrap_result naming
EXPECTED_USER_ID with no words and a token; its second must be rejected with
code 2; acting as the User with that token, user.info must name that User as
issuer and node1 as subject. node2's call must answer 24 words that derive the
identity it answered, and a token. No node log may hold any of the words.
"""
import asyncio
from pathlib import Path

import astral

from lib.sessionio import load

# The identity BIP-39 + BIP-32 m/1791'/0'/0'/0/0 derives from the driver's
# words, computed outside astrald.
EXPECTED_USER_ID = (
    "02564ba75b0fc5869e12c0c89e56a9c4fbec9d23ed01e6d240aac80532fcb10af1")
WORDS = ("horse soldier imitate stool square buyer verb party enjoy "
         "result jazz rabbit trigger file benefit cloth term change")


def check_log_free_of(node: dict, words: str) -> None:
    """The node's log holds no two adjacent words of words.

    why pairs: single words such as "change" or "file" occur in log text.
    """
    log = Path(node["root"]) / "astrald.log"
    assert log.exists(), f"no node log at {log}"
    text = log.read_text(errors="replace")
    ws = words.split()
    for pair in zip(ws, ws[1:]):
        assert " ".join(pair) not in text, f"{log} holds {' '.join(pair)!r}"


async def check_node1(n1: dict, facts: dict) -> None:
    first = facts["first"]
    assert not first["refused"], f"first call refused: {first['detail']}"
    assert len(first["answer"]) == 1, f"first call answered {first['answer']}"
    result = first["answer"][0]
    assert result["Type"] == "mod.user.bootstrap_result" and result["Object"] == {
        "Identity": EXPECTED_USER_ID, "Mnemonic": "", "Token": result["Object"].get("Token")
    }, f"first call answered {first['answer']}"
    assert result["Object"]["Token"], "first call answered no token"
    assert facts["user_token"] == result["Object"]["Token"], (
        "the driver did not act with the token the claim returned")

    second = facts["second"]
    assert second["refused"] and "code 2" in second["detail"], (
        f"second call on a claimed node: {second}")

    async with await astral.connect(n1["endpoint"],
                                    token=facts["user_token"]) as c:
        who = await c.apphost.whoami()
        assert str(who) == EXPECTED_USER_ID, f"whoami {who}"
        info = await c.user.info()
        assert str(info.user_id) == EXPECTED_USER_ID, (
            f"contract issuer {info.user_id}")
        assert str(info.node_id) == n1["identity"], (
            f"contract subject {info.node_id} != node {n1['identity']}")
    check_log_free_of(n1, WORDS)


def check_node2(n2: dict, facts: dict) -> None:
    generated = facts["generated"]
    assert not generated["refused"], f"generate refused: {generated['detail']}"
    assert len(generated["answer"]) == 1, f"generate answered {generated['answer']}"
    result = generated["answer"][0]
    assert result["Type"] == "mod.user.bootstrap_result", f"generate answered {result}"
    words = result["Object"]["Mnemonic"]
    assert len(words.split()) == 24, f"generate answered {len(words.split())} words"
    assert facts["derived"] == result["Object"]["Identity"], (
        f"the words derive {facts['derived']}, "
        f"the reply names {result['Object']['Identity']}")
    assert result["Object"].get("Token"), "generate answered no token"
    check_log_free_of(n2, words)


async def main():
    doc = load()
    await check_node1(doc["nodes"]["node1"], doc["facts"])
    check_node2(doc["nodes"]["node2"], doc["facts"])
    print("oracle: given words claimed node1 for the pinned user, a second call "
          "was refused with code 2; generated words claimed node2 and derive "
          "its user; no node log holds the words")


asyncio.run(main())
