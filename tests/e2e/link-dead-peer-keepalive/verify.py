#!/usr/bin/env python3
"""Oracle: a frozen peer's links close within N + T, and what rode them ends.

From the `dead_peer_keepalive` fact, with N and T this world's tcp and kcp
link_idle_ping and link_ping_timeout (test.toml):

- node1 held a settled link to node2 before the freeze;
- node1 lists no link to node2 within N + T + 2 s of the freeze;
- node1 logged each link to node2 closed for `ping timeout`;
- the `log.listen` stream node1 held open on node2 ended within N + T + 2 s;
- the query sent to node2 at the freeze ended with an error, not an answer;
- once node2 resumed, both ends hold a link to each other within 60 s.

todo: once astral-docs core-definitions/query.md Routing outcomes lands in
astrald (ticket k8mv9KqtUy), a query waiting on a link that closes ends as
unavailable at once; then the query must also end within N + T + 2 s. Until
then it waits out its 60 s routing deadline, and the oracle prints when it
ended.
"""
import tomllib
from pathlib import Path

from lib.sessionio import load

SLACK = 2.0


def seconds(value: str) -> float:
    assert value.endswith("s"), f"{value!r} is not in seconds"
    return float(value[:-1])


def bound() -> float:
    """N + T + SLACK, the largest of tcp's and kcp's for node1."""
    cfg = tomllib.loads((Path(__file__).parent / "test.toml").read_text())["config"]["node1"]
    return max(seconds(cfg[m]["link_idle_ping"]) + seconds(cfg[m]["link_ping_timeout"])
               for m in ("tcp", "kcp")) + SLACK


fact = load()["facts"]["dead_peer_keepalive"]
limit = bound()
print(f"oracle: links {fact['before']} gone at {fact['gone_at']} s (limit {limit} s); "
      f"stream {fact['stream']}; query {fact['query']}; relinked after {fact['relinked_after']} s")

assert fact["settled"] and fact["before"], (
    f"node1 held no settled link to node2 before the freeze: {fact['before']}")
assert fact["gone_at"] is not None and fact["gone_at"] <= limit, (
    f"node1 still listed a link to node2 {fact['gone_at'] or 'past 30'} s after "
    f"node2 froze; want none within {limit} s")

st = fact["tap"]
assert st["refused"] is None and st["broken"] is None, f"node1's log tap is not whole ({st})"
timeouts = [line for line in fact["closed"] if "ping timeout" in line]
assert len(timeouts) >= len(fact["before"]), (
    f"node1 logged {len(timeouts)} link(s) to node2 closed for ping timeout, "
    f"want one per link it held ({fact['before']}): {fact['closed']}")

assert fact["stream_open_before_freeze"], (
    f"the log.listen stream on node2 ended before the freeze: {fact['stream']}")
ended = fact["stream"]["ended_at"]
assert ended is not None and ended <= limit, (
    f"the stream over the dead link ended {ended} s after the freeze "
    f"({fact['stream']['how']}); want within {limit} s")

assert not fact["query"]["answered"], f"the frozen node2 answered a query: {fact['query']}"

assert fact["relinked_after"] is not None, "node1 and node2 did not relink within 60 s of the resume"

print(f"oracle: node1 closed {len(fact['before'])} link(s) to the frozen node2 "
      f"{fact['gone_at']} s after the freeze; the stream over them ended at {ended} s")
