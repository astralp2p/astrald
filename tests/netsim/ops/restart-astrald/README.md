# restart-astrald

Restarts astrald on `--vm` (default `node2`) `--in` seconds from now (default
30), while the driver watches. The restart is scheduled first, as the
transient timer `e2e-astrald-restart` in the guest, and runs
`systemctl restart astrald`: install-astrald runs astrald as the systemd unit
`astrald`, and a VM world cannot restart a node through the manifest's
`restart` key. The node keeps its state and config.

`--kill` kills astrald with SIGKILL instead (`systemctl kill --signal=SIGKILL
--kill-whom=main astrald`), and systemd starts it again: install-astrald's
unit sets `Restart=on-failure`, and a SIGKILL is a failure. A killed astrald
runs no shutdown path and sends its peers nothing as it goes, as a manifest's
`restart_kill` does in a local world; `kcp-restart-stale-link` uses it.

`verify.py` checks, independently, that astrald runs on the VM and that its
restart is pending. What astrald does once it is back is the test's oracle's
to judge.
