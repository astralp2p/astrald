# restart-tor

Restarts Tor on `--vm` (default `node2`) `--in` seconds from now (default
30), while the driver watches. With `--down` above 0 (default 0), Tor stops
at `--in` and starts again `--down` seconds later, so the driver sees a
window in which the node has no Tor.

    restart-tor node2 --in 60 --down 30
    restart-tor node2 --in 240 --down 20

The op schedules one transient timer, `e2e-tor-restart`, in the guest. With
`--down` above 0 its unit runs `systemctl stop`, sleeps `--down` seconds and
runs `systemctl start`, so Tor stays down for `--down` seconds from the
stop's end and always starts after it. With `--down 0` it runs one
`systemctl restart`. The timer sets `AccuracySec=1s`: a transient timer
otherwise keeps systemd's default of 1 min and fires up to 60 s late
(`systemd-run(1)`). Every call stops and resets the unit first, so a second
call replaces the first.

Debian and Ubuntu run the Tor daemon as the instance `tor@default` under the
multi-instance master `tor.service`. The op names `tor@default.service` and
`tor.service` together where the guest has the instance, and `tor.service`
alone where it does not, so the daemon itself stops and starts.

astrald keeps running throughout. astrald's tor module registers its onion
service over the control connection, and Tor drops the service together with
that connection (`mod/tor/src/server.go`). What astrald does while Tor is
down and once Tor is back is the test's oracle's to judge. `enable-tor` runs
first and installs Tor.

`verify.py` checks, independently, that Tor runs on the VM now and that the
`e2e-tor-restart` timer waits.

The op writes no module config.
