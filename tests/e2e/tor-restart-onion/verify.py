#!/usr/bin/env python3
"""Oracle: after Tor restarts under a running node2, its onion is withdrawn while Tor is down and reachable again.

From the `tor_restart_onion` fact (times on the host's clock; `down_seen` is
when the driver first saw node2's `is down: tor control connection closed`
line, and Tor starts again --down s after it):

- node2 resolved a tor row for itself before the stop;
- every sample after `down_seen`, up to TOR_START_SLACK s before Tor was due
  back, held no tor row, and there was at least one such sample;
- the tor row was back within BACK_WITHIN s of Tor's start, at the address it
  held before;
- a tor link node1→node2 formed within LINK_WITHIN s of the row's return,
  both ends list it under one id, and node2 answered `user.info` from node1
  as the User while it was up;
- node2's log tap stayed whole, so astrald did not restart: a restart ends
  the stream (lib/nodelog.py).

The spec is silent on the onion's withdrawal and re-registration. The
oracle follows mod/tor/src/server.go (startServer retries Run with a backoff
from 1 s to 1 min; Run reuses tor.key; accept withdraws the endpoint before
it logs the loss) and mod/tor/src/endpoint_resolver.go (no row while the
service is down), from commit 8535c9cb, ticket MVuHGbOzi5.
"""
from lib.sessionio import load

# why 75 s: the 1 min backoff cap of startServer, and 15 s for Tor to open its
# control port and astrald to register the service again (derived)
BACK_WITHIN = 75.0
# why: the down line reaches the host up to one tap read late, so Tor's start
# is estimated late by as much
TOR_START_SLACK = 2.0
# why 240 s: a tor dial runs for 60 s per attempt and a fresh descriptor may
# need minutes to publish (derived, as tor-link's 300 s link wait)
LINK_WITHIN = 240.0


def check_withdrawal(f: dict) -> tuple:
    """The onion before the stop and Tor's start time, after checking the down window."""
    down = f["down_seen"]
    assert down is not None, (
        f"node2 never logged the loss of its onion service while the driver watched; "
        f"tap {f['tap']}, lines {f['log']}")
    before = [s["onion"] for s in f["samples"] if s["t"] < down and s.get("onion")]
    assert before, f"node2 resolved no tor row for itself before Tor stopped: {f['samples'][:5]}"
    assert len(set(before)) == 1, f"node2's onion changed before Tor stopped: {sorted(set(before))}"
    start = down + f["op"]["down"]
    window = [s for s in f["samples"] if down < s["t"] <= start - TOR_START_SLACK
              and "error" not in s]
    assert window, f"no sample of node2's rows fell between the stop ({down} s) and Tor's start ({start} s)"
    held = [s for s in window if s.get("onion")]
    assert not held, f"node2 still advertised its onion while Tor was down: {held}"
    print(f"oracle: node2's onion {before[0]} gone at {down} s; {len(window)} samples "
          f"without it until Tor's start at {start} s")
    return before[0], start


def check_return(f: dict, onion: str, start: float) -> None:
    back = f["back"]
    assert back is not None, (
        f"node2 resolved no tor row again after Tor started at {start} s; lines {f['log']}")
    assert back["t"] - start <= BACK_WITHIN, (
        f"node2's tor row came back {back['t'] - start:.1f} s after Tor started; "
        f"want within {BACK_WITHIN} s")
    assert back["onion"] == onion, (
        f"node2's onion after the restart is {back['onion']}, before it was {onion}; "
        "tor.key is reused, so the address is the same")
    print(f"oracle: node2's onion back at {back['t']} s, {back['t'] - start:.1f} s after Tor's start")


def check_link(f: dict) -> None:
    link, back = f["link"], f["back"]
    assert link and link["at"] is not None, (
        f"node1 formed no tor link to node2 within the driver's wait: {link}")
    assert link["at"] - back["t"] <= LINK_WITHIN, (
        f"node1's tor link to node2 formed {link['at'] - back['t']:.1f} s after the onion "
        f"returned; want within {LINK_WITHIN} s")
    shared = set(link["ids"]) & set(link.get("node2_ids") or [])
    assert shared, (
        f"node1 lists tor links {link['ids']} to node2 and node2 lists {link.get('node2_ids')}: "
        "no link held on both ends")
    assert link["info"]["ok"], f"node2 did not answer user.info over the tor link: {link['info']}"
    print(f"oracle: tor link {sorted(shared)} at {link['at']} s; user.info answered with "
          f"links over {link['info']['networks']}")


def main():
    f = load()["facts"]["tor_restart_onion"]
    onion, start = check_withdrawal(f)
    check_return(f, onion, start)
    check_link(f)
    tap = f["tap"]
    assert tap["refused"] is None and tap["broken"] is None, (
        f"node2's log tap did not stay whole ({tap}): astrald restarted or the record is incomplete")
    print(f"oracle: node2's log tap whole, {tap['entries']} entries; astrald ran throughout")


main()
