# claimed-node-keeps-owner

A node that belongs to a User refuses to become another User's node, whoever
asks and by whichever op.

- **env** node · **after** `bootstrap-user-software-key` · **saves** nothing
- **driver** `script.py` — derives a second User B from the BIP-39 test vector
  `abandon … about`, then, as node1 and as the first User A, asks node1 to
  change owner through each op that can write an active contract:
  `user.bootstrap` with B's words, `user.bootstrap` generating new words,
  `user.accept_contract` with B's contract, `user.accept_membership` with a
  contract for node1 carrying B's real issuer signature, and `user.join`.
  It prints each answer and writes no facts. `prompt.md` asks an operator for
  the same change in words ("make this node a User node from another seed
  phrase"), so the oracle judges state and not the driver's record.
- **oracle** `verify.py` — node1 still reports A as the issuer of its active
  contract (fully signed, subject node1, state `claimed`), and A's token still
  answers `whoami` and `user.info` as A. The oracle then asks once itself,
  as node1, for `user.bootstrap` with B's words: the answer must be
  `node already claimed`, and node1 must still report A afterwards.

**Roster.** node1 only, in the world `bootstrap-user-software-key` leaves. It
does not set `destroys`: every attempt is refused and changes nothing on the
node, so the world stays as the next test expects it (node1 claimed by A, A's
token valid). `main.suite` lists it right after `bootstrap-user-software-key`.

**Why the oracle asks itself.** An operator driver reports no record of what it
tried, so what the oracle can read is the node. Whether a node can still be
claimed is observable only by claiming it, so the oracle makes one attempt, with
words that no User of any test holds. A node that has lost its guard answers
by claiming; one that keeps its owner by a deeper check only answers another
text, which the oracle names.

**Limits.** `user.accept_contract` cannot be fed a contract that would activate:
node1 signs as the contract's subject only for the claim rule of
`auth.sign_contract`, and that is closed on a claimed node, so the driver's
contract for B carries no valid subject signature, and a node without the
`node already claimed` check would still refuse it at validation. The same
holds for `user.join`, which would fail at routing. Their refusals are printed
by the driver and not judged; `user.accept_membership`, the one op whose guard
is the only thing between a claimed node and an overwrite (it sets the contract
without `claim`'s lock), is judged by state. No astrald op imports a key into a
claimed node (there is no `user.import_key`); `import-user-software-key` claims
through `user.accept_contract`.

Run it alone: `./tests/run claimed-node-keeps-owner`.
