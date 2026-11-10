Set up the astral node `{node1.alias}` (identity `{node1.identity}`), the node
your `astral-query` reaches. It is new and belongs to nobody, and it runs with
the default access lists, so work as a person with no access token would:
appoint a manager for the node, then claim the node with the manager's token for
the User these seed phrase words derive, and keep the manager's id, the user's id
and an access token for the User:

```
abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon art
```

Read the node's state with `user.info` three times: before the manager is
appointed, after it is, and after the claim. Keep the three states as an object
with the keys `before`, `appointed` and `claimed`.

Also, with no token, ask the node to run `user.bootstrap` with those words twice:
once before the manager is appointed and once after (do not claim the node that
way; the node should refuse). Keep the exact error text the node answers each
time, as an object whose keys are `fresh/tokenless/user.bootstrap` and
`unclaimed/tokenless/user.bootstrap`.
