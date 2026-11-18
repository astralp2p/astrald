# stop-astrald

Stops astrald on each named VM and leaves it stopped.

    vm:stop-astrald <vm>...

A VM world has no `stop` step: `stop` in a test manifest runs in local and
Docker worlds only (`tests/README.md`). A node cannot stop itself over
apphost either, so stopping a node in a VM world is a vmop's job.

The VM stays up. sshd, the journal and the harness's apphost tunnel stay
reachable; a query through the tunnel is closed at once while astrald is down.

`verify.sh` checks that `systemctl is-active astrald` is not `active` on each
named VM.
