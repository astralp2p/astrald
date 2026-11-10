# request-grant-scope

An app asks for discovery scoped to one service and receives exactly that
scope; a second scope adds up beside the first, and one of the two is
revoked by its ID.

## What it covers

* `apphost.set_grant_request_policy -mode accept_all` approves every request
  that passes permit validation, with no delegate asked.
* `apphost.request_grant` reads one `mod.apphost.request_grant_request`. A
  `mod.services.service_discovery_action` permit scoped to `inference` is
  written as sent: `services.discover` for `inference` reports it granted, and
  for `chat` it opens with `services.not_granted` naming `chat`.
* A discovery permit without constraints is refused with
  `invalid permit: mod.services.service_discovery_action: …` before any policy
  is asked.
* A second request scoped to `chat` adds a grant beside the first:
  `apphost.list_grants` emits two `mod.apphost.grant` objects with distinct IDs.
* `apphost.revoke -id` withdraws the `chat` grant alone; `inference` stays
  granted.

## Notes

* The driver clears the grant request policy (empty set) at the end, so the
  node leaves the test as it entered it: unset, refusing every request.
* Discovery runs at local reach on node1, so the scope decides
  `services.not_granted` without any provider or peer.
