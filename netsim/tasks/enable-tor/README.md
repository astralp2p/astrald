# enable-tor

On each target VM, installs Tor with its control port, restarts astrald to publish an onion, and saves the node's own endpoint to `/root/tor.json`. verify.py asserts each VM runs tor and its saved onion matches the one astrald advertises via `nodes.resolve_endpoints`.
