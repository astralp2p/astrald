#!/usr/bin/env python3
"""Oracle: a requested discovery scope admits its service alone, a second
scope adds up beside it, and revoke by ID withdraws one of the two."""
from lib.sessionio import load

ACK = "ack"
DISCOVERY = "mod.services.service_discovery_action"


def main():
    f = load()["facts"]

    assert f["set_accept_all"] == ACK, f"set_grant_request_policy -mode accept_all: {f['set_accept_all']}"
    assert f["inference_before"] == "not granted", f"inference before any grant: {f['inference_before']}"
    assert f["request_inference"] == ACK, f"request for the inference scope: {f['request_inference']}"
    assert f["inference_after"] == "granted", f"inference after its grant: {f['inference_after']}"
    assert f["chat_after_inference"] == "not granted", f"chat after the inference grant: {f['chat_after_inference']}"
    assert f["request_unscoped"].startswith(f"invalid permit: {DISCOVERY}: "), (
        f"an unscoped discovery request answered {f['request_unscoped']!r}")
    assert f["request_chat"] == ACK, f"request for the chat scope: {f['request_chat']}"

    two = f["grants_two"]
    assert sorted(g["services"][0] for g in two) == ["chat", "inference"], (
        f"list_grants after two requests: {two}")
    assert len({g["id"] for g in two}) == 2, f"the two grants share an ID: {two}"
    assert all(g["expires_at"] is None for g in two), f"a zero duration expires: {two}"

    assert f["revoke_chat"] == ACK, f"revoke -id of the chat grant: {f['revoke_chat']}"
    left = f["grants_left"]
    assert [g["services"] for g in left] == [["inference"]], f"grants after the revoke: {left}"
    assert f["inference_after_revoke"] == "granted", "revoking chat withdrew inference"
    assert f["chat_after_revoke"] == "not granted", "chat stays granted after its revoke"

    assert f["restore_policy"] == ACK, f"restoring the bundled policy: {f['restore_policy']}"


if __name__ == "__main__":
    main()
