#!/usr/bin/env python3
"""Oracle: one manager is appointed on the fresh node, the gate holds the
unclaimed node to its list, an ordinary app cannot claim, and the manager's
claim keeps the appointment.
"""
from lib.sessionio import load

NOT_FRESH = "node is not fresh"


def main():
    f = load()["facts"]["manager_setup"]
    manager = f["manager"]

    assert f["fresh"] == {"state": "fresh", "manager": ""}, f"before: {f['fresh']}"
    assert f["unclaimed"] == {"state": "unclaimed", "manager": manager}, (
        f"after the appointment: {f['unclaimed']}; want unclaimed naming {manager}")
    assert f["claimed"] == {"state": "claimed", "manager": manager}, (
        f"after the claim: {f['claimed']}; want claimed naming {manager}")

    # why two refusals: the unclaimed gate list omits sys.register_manager,
    # and on the claimed node the op itself refuses.
    second = f["second"]
    assert second["refused"] and second["detail"].startswith("Denied"), (
        f"second appointment on the unclaimed node: {second['detail']} — want denied")
    third = f["third"]
    assert third["refused"] and NOT_FRESH in third["detail"], f"third appointment: {third['detail']}"

    g = f["outside_gate"]
    assert g["refused"] and g["detail"].startswith("Denied"), (
        f"the manager's apphost.list_tokens on the unclaimed node: {g['detail']}")

    # why the node: the caller queried with the node's token, so the handler
    # answers the node's identity.
    s = f["served"]
    node_id = load()["nodes"]["managed"]["identity"]
    assert not s["refused"] and s["detail"] == node_id, (
        f"the manager serving on the unclaimed node: {s['detail']}; want {node_id}")

    # why "not permitted" and not "Denied": the gate admits the op on the
    # unclaimed node, and the op's own admin_network check refuses the app.
    e = f["endpoints"]
    assert not e["refused"] and e["answer"], (
        f"the manager's nodes.resolve_endpoints on the unclaimed node: {e['detail']}, "
        f"{e.get('answer')}; want the node's endpoints")
    e = f["app_endpoints"]
    assert e["refused"] and "not permitted" in e["detail"] \
        and not e["detail"].startswith("Denied"), (
        f"an ordinary app's nodes.resolve_endpoints on the unclaimed node: "
        f"{e['detail']} — want the op's not permitted")

    a = f["app_claim"]
    assert a["refused"] and "not permitted" in a["detail"], (
        f"an ordinary app's user.bootstrap: {a['detail']}")

    assert f["claim"]["user"] and f["claim"]["token"], f"the manager's claim: {f['claim']}"

    print(f"oracle: one manager {manager[:8]}… appointed; fresh → unclaimed → claimed; "
          "the manager served and resolved the node's endpoints before the claim, "
          "and an ordinary app was not permitted to; later appointments refused; "
          "an ordinary app could not claim")


main()
