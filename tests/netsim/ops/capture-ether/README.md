# capture-ether

Starts a capture of UDP 8822, ether's port, on every interface of `--vm`
(default `reflector`) for `--for` seconds (default 150), as the transient unit
`e2e-capture-<name>`, and returns once tcpdump listens. When the window
closes, `capture.py` writes its record, the window on the guest's clock, the
guest's own addresses, every datagram sent from one of them and a count per
source of those heard, to the node's tree at `/e2e/capture/<name>` with the
guest's `astral-query`, and to `/var/lib/astral-tests/capture-<name>.json`.

The tree is how a driver gets the record: a driver reaches a guest only
through its apphost, and the capture ends after this op has. `verify.sh`
checks that the unit runs tcpdump; the record is judged by the test's oracle.
