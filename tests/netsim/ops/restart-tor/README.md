# restart-tor

Restarts Tor on `--vm` (default `node2`) `--in` seconds from now (default
30), while the driver watches. With `--down` above 0 (default 0), Tor stops
at `--in` and starts again `--down` seconds later, so the driver sees a
window in which the node has no Tor.

    restart-tor node2 --in 60 --down 30
    restart-tor node2 --in 240 --down 20

The stop and the start are scheduled first, as the transient timers
`e2e-tor-stop` and `e2e-tor-start` in the guest. With `--down 0`,
`e2e-tor-stop` runs one `systemctl restart`. Every call stops and resets both
units first, so a second call replaces the first.

Debian and Ubuntu run the Tor daemon as the instance `tor@default` under the
multi-instance master `tor.service`. The op names `tor@default.service` and
`tor.service` together where the guest has the instance, and `tor.service`
alone where it does not, so the daemon itself stops and starts.

astrald keeps running throughout. astrald's tor module registers its onion
service over the control connection, and Tor drops the service together with
that connection (`mod/tor/src/server.go`). What astrald does while Tor is
down and once Tor is back is the test's oracle's to judge. `enable-tor` runs
first and installs Tor.

`verify.py` checks, independently, that Tor runs on the VM now and that
`e2e-tor-stop`, and with `--down` above 0 `e2e-tor-start`, wait.

The op writes no module config.
