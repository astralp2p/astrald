#!/usr/bin/env python3
"""Oracle: a gw dial through gateways that reach each other only through each other ends, and does not loop.

Observed by the driver, judged here (fact `gw_cycle`, times on the host's
clock). Each of the 2 rounds to T:

- answered an error, no link: nothing in the chain exists;
- after at least DIALED s: a round that never dials the gw row answers at
  once, while one that dials it waits in the nested rounds for A and B until
  basic's 20 s gw budget ends them (mod/nodes/src/basic_link_strategy.go
  defaultRowBudgets);
- after at most BUDGET s, the caller's routing budget: a dial that loops or
  waits on itself runs into the driver's 90 s caller timeout instead.

node1 answered `apphost.whoami` within WHOAMI s after each round, and holds no
link to T, A or B.
"""
from lib.sessionio import load

ROUNDS = 2
DIALED = 15.0
BUDGET = 60.0
WHOAMI = 5.0


def main():
    f = load()["facts"]["gw_cycle"]
    rounds = f["rounds"]
    print(f"oracle: rows {f['rows']}")
    print(f"oracle: rounds {rounds}")

    assert len(rounds) == ROUNDS, f"the driver ran {len(rounds)} rounds, not {ROUNDS}"
    for r in rounds:
        assert "error" in r, f"round {r['round']} answered a link {r} through gateways that do not exist"
        assert DIALED <= r["elapsed"] <= BUDGET, (
            f"round {r['round']} answered after {r['elapsed']} s; want {DIALED}..{BUDGET} s: "
            "a round that skips the gw row answers at once, a looping one waits for the caller")
        assert r["whoami"] <= WHOAMI, f"node1 answered whoami {r['whoami']} s after round {r['round']}"
    assert not f["linked"], f"node1 holds links to strangers: {f['linked']}"

    print(f"oracle: {ROUNDS} rounds through the gateway cycle ended after "
          f"{', '.join(str(r['elapsed']) for r in rounds)} s with {rounds[-1]['error']}")


main()
