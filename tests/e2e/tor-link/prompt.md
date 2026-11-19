Your swarm peer `{node2.alias}` (identity `{node2.identity}`) is reachable
over Tor. Re-establish your link to it over Tor.

Then ask `{node2.alias}` for its user info (`user.info` with target
`{node2.identity}`) from your node, again after every failure, until it
answers. Record in `first_answer` how many seconds after you started the
first query it answered (`after`), the networks your links to it ran over at
that moment (`networks`), the failed queries before it (`failures`), and
`tcp` as an empty object.
