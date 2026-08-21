# noop

Boots the world and changes nothing.

netsim starts a simulation from a stage only through `task` or `story`, and
both need something to run — an empty story is refused with
`error: story has no tasks`. The netsim executor needs a plain boot, so this
is the smallest task that provides one.

Takes no arguments and touches no VM.
