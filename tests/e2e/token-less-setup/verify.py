#!/usr/bin/env python3
"""Oracle: a token-less session set up claimer and joined joiner, and reaches
nothing else of the claimed node."""
from lib.sessionio import load

# The step-by-step setup ops; the unclaimed list refuses them with denied.
STEP_BY_STEP = ("bip137sig.new_entropy", "bip137sig.seed", "bip137sig.derive_key",
                "crypto.public_key", "objects.store", "user.new_node_contract",
                "auth.sign_contract", "user.accept_contract")

# Ops the claimed list admits, and ops it refuses with denied.
ADMITTED = ("user.info", "apphost.register")
DENIED = ("user.derive_identity", "shell.spec", "apphost.list_tokens")


def main():
    doc = load()
    nodes, facts = doc["nodes"], doc["facts"]["token_less_setup"]
    claimer, joiner = nodes["claimer"], nodes["joiner"]

    u = facts["unclaimed"]
    assert u["user.info"]["refused"] and "code 2" in u["user.info"]["detail"], (
        f"token-less user.info on the unclaimed node: {u['user.info']['detail']}")
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
    assert j["before"]["refused"] and "code 2" in j["before"]["detail"], (
        f"joiner answered user.info before the join: {j['before']['detail']}")
    assert not j["joined"]["refused"], f"user.join: {j['joined']['detail']}"
    assert j["issuer"] == c["claimed"] and j["subject"] == joiner["identity"], (
        f"joiner's contract {j['issuer']} -> {j['subject']}; want "
        f"{c['claimed']} -> {joiner['identity']}")
    assert j["user_after"] == c["claimed"], (
        f"joiner answers as {j['user_after'] or j['after']['detail']}, not the User")
    assert j["again_anon"]["refused"] and j["again_anon"]["detail"].startswith("Denied"), (
        f"a token-less user.join on the claimed joiner: {j['again_anon']['detail']} — want denied")
    assert j["again"]["refused"] and "code 2" in j["again"]["detail"], (
        f"user.join with joiner's token on the claimed joiner: {j['again']['detail']}")

    print("oracle: preview = claim, token is the User, claimed list holds; "
          "joiner joined the User's swarm and refuses a second join")


main()
