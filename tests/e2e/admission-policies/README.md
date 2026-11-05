# admission-policies — each admission policy refuses, and admits nothing it refused

The five admission policies of `node-policies.md` (app register, grant
request, swarm join, swarm invite, contract accept) each refuse with their own
text: by config value, by mode, and through a stored delegate that cannot
decide. Every other test runs the harness's accepting policies, so none of
these refusal paths runs anywhere else.

- **env** node · **start** `null` · **saves** — · a guard: it destroys nothing.
- **config** — `admitter` rejects every swarm join and app registration and
  accepts invitations; `invitee` accepts joins and rejects invitations. The
  override names every key of `policies`, since it replaces the module key
  whole.
- **driver** `script.py` — links admitter with applicant and invitee, and
  acceptor with invitee, then:
  - claims `admitter` with a token-less `user.bootstrap`, and registers an app
    without a token on admitter (refused) and on applicant (the control);
  - `user.join` from applicant to admitter under the config's `reject_all`,
    under a delegate that answers the wrong object (invitee's `user.info`), and
    under a delegate no node holds; then names the node as its own delegate,
    which the setter refuses, and clears the delegate;
  - as the User, adopts invitee (refused by its invite policy) and applicant
    (the control: adoption asks the invited node, never admitter's join
    policy);
  - delegates admitter's app register policy to invitee's `user.info`,
    registers without a token (refused), and clears the delegate;
  - on `acceptor`, with a contract accept delegate that answers the wrong
    object, claims step by step to `user.accept_contract` (refused), clears
    the delegate and accepts the same contract (the control);
  - as acceptor's User, sets the grant request mode `reject_all`, has an app
    request `mod.user.see_swarm_action` (refused), sets `accept_all`, has it
    request `mod.auth.see_objects_action` (the control), and leaves the mode
    at `reject_all`.

  Every answer, its error class and its node text go into the `admission`
  fact.
- **oracle** `verify.py` — every refusal is a `RemoteError` whose text is
  exactly the policy's documented one, and every control was answered. Then,
  with each node's own token: admitter and applicant are claimed by the same
  User, admitter's swarm is admitter and applicant alone, invitee stays
  `fresh`, both admitter delegates read `nil` in the tree, acceptor is claimed
  by its own User with no contract accept delegate, the grant request mode
  reads `reject_all`, and the app holds one `mod.auth.see_objects_action`
  grant and no `mod.user.see_swarm_action` grant. The oracle asks three
  refusals again itself: a token-less register on admitter (declined, and no
  token minted), invitee's join to admitter (declined by the config once the
  delegate is cleared, invitee still `fresh`), and the app's request for
  `mod.user.see_swarm_action` (declined, its grants unchanged).

## Why each assertion

- **The exact text.** A timeout, a lost route or a codec failure also leaves
  the caller with nothing. Only the policy's own text says the policy
  decided.
- **The controls.** A refusal beside an op that fails for everyone proves
  nothing; each refused op is answered once in the same run.
- **The wrong-object and unreachable delegates.** A delegated policy that
  cannot get a decision must refuse; admitting on a fault would open the
  node to anyone who can break its delegate.
- **The roster and the node states.** A refusal that still issued a contract
  would read as a pass from the caller's side alone.
- **The `nil` delegates.** Clearing a delegate restores the config value; the
  oracle's own join from invitee shows the config decides again.
- **Two actions.** The node records a permit an app already holds as the
  same grant, so a declined request for the control's action would leave
  nothing to tell apart. The declined requests ask `mod.user.see_swarm_action`,
  which `accept_all` grants, and the control `mod.auth.see_objects_action`.
  Registration grants an app `serve_apps` and `use_blueprints` by itself, so
  the oracle judges the requested actions alone.

## Why its own nodes

The refusals need policies the rest of w0 does not run. The four roster names
are this test's alone, so their config overrides join w0 without forking it,
and the accepting policies of every other node stay as they are.
