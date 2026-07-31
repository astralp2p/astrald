# nat-punch

Story 0005. Two symmetric-NAT'd peers hole-punch into a direct kcp link.

- **env** netsim · **start** `two-nodes` · **saves** `two-nodes-nat`
- **steps** `enable-tor`, `enter-nat`, `configure-nat-tor`, `add-reflector`,
  then the driver.
- **oracle** `verify.py` — a direct **kcp** link on both ends. A punch that
  leaves only the tor signaling link is a failed punch.

Step order is load-bearing: `configure-nat-tor` restarts astrald, and
`add-reflector` arms the `nat` module through an in-memory reflected
endpoint. Arming before the last restart wipes it and the punch aborts with
`does not support NAT traversal`. Arm last.

The trigger is `nodes.new_link -strategies nat`, which drives NATLinkStrategy
end to end. `nat.punch` registers a Hole only and yields no link.

Script-driven only: there is no `prompt.md`, because the netsim `punch-nat`
task was script-only too.

The reflector VM is provisioned by the `add-reflector` vmop rather than by a
recipe state (migration plan, open decision 5): `start` stays `two-nodes`, and
a recipe state waits until a second test needs a NAT'd pair.
