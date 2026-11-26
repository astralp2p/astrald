#!/usr/bin/env python3
"""Oracle: a token-less session set up claimer and joined joiner, and reaches
nothing else of the claimed node."""
from lib.sessionio import load

# The step-by-step setup ops; the tokenless list refuses them with denied.
STEP_BY_STEP = ("bip137sig.new_entropy", "bip137sig.seed", "bip137sig.derive_key",
                "crypto.public_key", "objects.store", "user.new_node_contract",
                "auth.sign_contract", "user.accept_contract")

# Ops the E2E tokenless list admits on the claimed node, and ops it refuses
# with denied.
ADMITTED = ("user.info", "apphost.register", "user.derive_identity")
DENIED = ("shell.spec", "apphost.list_tokens")


def main():
    doc = load()
    nodes, facts = doc["nodes"], doc["facts"]["token_less_setup"]
    claimer, joiner = nodes["claimer"], nodes["joiner"]

    u = facts["unclaimed"]
    assert u["user.info"].get("state") == "fresh", (
        f"token-less user.info on the unclaimed node: {u['user.info']}; want state fresh")
    for op in STEP_BY_STEP:
        assert u[op]["refused"] and u[op]["detail"].startswith("Denied"), (
            f"token-less {op} on the unclaimed node: {u[op]['detail']} — want denied")

    c = facts["claim"]
    assert c["words"] == 24, f"user.derive_identity -generate answered {c['words']} words"
    assert c["preview"] == c["claimed"], (
        f"previewed {c['preview']}, claimed {c['claimed']} — the preview is not the claim")
    assert c["mnemonic_in_claim"] == "", "user.bootstrap echoed the words the caller sent"
    assert c["token"], "user.bootstrap answered no token"
    assert c["token_whoami"] == c["claimed"], (
        f"the bootstrap token authenticates as {c['token_whoami']}, not the User {c['claimed']}")

    for op in ADMITTED:
        r = facts["probes"][op]
        assert not r["refused"], f"token-less {op} on the claimed node: {r['detail']}"
    for op in DENIED:
        r = facts["probes"][op]
        assert r["refused"] and r["detail"].startswith("Denied"), (
            f"token-less {op} on the claimed node: {r['detail']} — want denied")

    a = facts["admin"]
    assert a["list_tokens"] == "answered", (
        f"the app registered for {a['app']} without a token cannot list tokens: "
        f"{a['list_tokens']} — the asked permit was not written")
    assert a["whoami"] == a["app"], (
        f"an empty caller from the app's session reads as {a['whoami']}, not the app {a['app']}")
    assert a["whoami"] != claimer["identity"], "an empty caller with a token acts as the node"

    j = facts["join"]
    assert j["before"].get("state") == "fresh", (
        f"joiner answered user.info before the join: {j['before']}; want state fresh")
    assert not j["joined"]["refused"], f"user.join: {j['joined']['detail']}"
    # note: since G7 a Member's membership is issued by its carrier, claimer,
    # carrying out the Approval of its JoinRequest; user.info answers that
    # contract, so it names the carrier and not the User (open finding on G7)
    assert j["issuer"] == claimer["identity"] and j["subject"] == joiner["identity"], (
        f"joiner's contract {j['issuer']} -> {j['subject']}; want "
        f"{claimer['identity']} -> {joiner['identity']}")
    assert j["user_after"] == claimer["identity"], (
        f"joiner answers with a contract from {j['user_after'] or j['after']['detail']}, "
        "not its carrier's membership")
    assert j["again_anon"]["refused"] and "node already claimed" in j["again_anon"]["detail"], (
        f"a token-less user.join on the claimed joiner: {j['again_anon']['detail']}")
    assert j["again"]["refused"] and "node already claimed" in j["again"]["detail"], (
        f"user.join with joiner's token on the claimed joiner: {j['again']['detail']}")

    print("oracle: preview = claim, token is the User, tokenless list holds; "
          "joiner joined the User's swarm and refuses a second join")


main()
