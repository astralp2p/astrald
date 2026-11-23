# swarm-churn-leave

The F3 chain's leave step: node4 stops for good. The other apps' follows
drop node4's offering within the tcp keepalive bound, no call to node4's app
hangs to the routing deadline, and the 20 pairs among the remaining five apps
answer every call.

- **after** `swarm-churn-late-app` · **stop** `["node4"]` (implies
  destroys) · **host_listener** — the mesh apps serve on the test host.
- **driver** `script.py` — records its end time and node4's app id
  (`churn_leave`); the harness then stops node4.
- **oracle** `verify.py` — watches 35 s from its start: node4 is absent from
  the session view; every remaining follow removed node4's offering
  (`member_lost` or `member_removed`) within 27 s and does not show it again;
  no call to node4's app took over 27 s, and each remaining app kept calling
  it; the 20 remaining pairs answered every call in the window. It stops
  node4's app.

Spec: `.ai/system/protocols/services/types/services.removed.md`,
`.ai/system/core-definitions/query.md` (Routing outcomes). Bound: D11, tcp
N + T + 2 = 27 s.

## Spec conflict

The plan names `member_lost` alone. node4 stops gracefully, and a node that
shuts down may retract its hosted offerings on the carry before its links
close; a member's removal reaches the caller as `member_removed`
(`services.removed.md`, Cause). The oracle accepts both.
