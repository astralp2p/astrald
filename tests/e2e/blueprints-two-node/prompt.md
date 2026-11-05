On the astral node `{node1.alias}` (identity `{node1.identity}`), the node
your `astral-query` reaches, register a new object type named
`test.blueprints.probe`: a struct with two fields, `Label` of type `string16`
and `Serial` of type `uint64`. Then try to register the same type a second
time, and keep the text of what the node answered.

Store one object of that type in `{node1.alias}`'s `local` repository, with
`Label` set to `a type the SDK was never built against` and `Serial` set to
`20260813`. Keep the type's name, the `Label` and the `Serial` you stored
(the `Serial` as a number), and the object's id.

Do all of this on `{node1.alias}` alone; do nothing on `{node2.alias}`.
