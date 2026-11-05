An astral object with id `{facts.peer_object_id}` is stored in your swarm, on
a node other than `{node1.alias}`. Working on `{node1.alias}` (identity
`{node1.identity}`), the node your `astral-query` reaches:

1. Read the object without naming the node that holds it, and keep how many
   bytes you read, as a number.
2. Try the same read again, limited to `{node1.alias}`'s own device and
   virtual zones — no network — and keep whether that read was refused, as
   `true` or `false`.
3. Ask which nodes can provide the object, and keep the list of their
   identities.
