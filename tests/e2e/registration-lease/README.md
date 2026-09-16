# registration-lease

Two external describers, alike in everything but renewal: one lapses and falls
out of the fan-out, the other renews and stays in it.

## What it covers

`objects.register_describer` used to bind a registrant for as long as the node
ran. The op answered an `ack`, and only a failing authorization check ever
removed an entry, so an app that crashed while still authorized stayed
registered and every later `objects.describe` dialled an identity with nothing
behind it.

Registration is now a lease. The test holds both halves of that claim apart:

* `gone` registers, closes its connection, and never renews. The node dials it
  while its lease is live and stops once the lease has lapsed.
* `kept` registers and re-registers before its lease ends. A describe after its
  *first* lease would have ended still reaches it, so re-registering refreshed
  the existing entry rather than being ignored or adding a second one.

The two differ in nothing but renewal, which is what keeps the test about the
lease rather than about the node forgetting registrations generally.

## Why the node log, and not timings

A departed registrant is **cheap** to dial: the route is simply not there, and
the fan-out gets `route not found` in about a millisecond. Both describes are
therefore prompt, and timings separate nothing. What separates the two cases is
whether the node reached for the registrant at all, and the node's own log is
where that is visible — one routed query per fan-out, per registrant.

This is worth stating plainly because the opposite is easy to assume: the
15-second external-call timeout is paid by a registrant that is **reachable and
silent**, not by one whose process is gone.

## The timeline

    t=0   both register, leases end at t=6
    t=0   describe #1 — both are live, both are dialled
    t=4   `kept` renews, its lease now ends at t=10
    t=6   `gone` expires; the 1s sweep removes and logs it by t=7
    t=8   describe #2 — `gone` is two seconds gone, `kept` has two left

Both margins are two seconds, so neither side is a race.

## Notes

* Leases are requested short rather than configured short. The node clamps a
  request down and never up, so a small lease is granted exactly as asked.
* The world's nodes run with `external_registration_sweep_interval: 1s`
  (`tests/lib/nodeconfig.py`); the node's own default is a minute, longer than
  most tests live. Only the removal and its log line are hurried — the fan-out
  skips an expired registration whatever the sweep is set to.
* Both registrants drive the register op directly, so what this test judges is
  the node's own lease and refresh. The SDK-side renewal loop that saves an app
  from writing one is covered by unit tests in astral-go and astral-py.
