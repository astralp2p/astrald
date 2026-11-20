# move-subnet

Moves `--vm` (default `node1`) to a new subnet `--in` seconds from now
(default 30), while the driver watches. Each `--to <a.b.c>` names one move,
to `<a.b.c>.<octet>/24`, where the octet is the last one of the VM's 10.77
address; two `--to` move it twice, `--gap` seconds apart (default 3.5).

    move-subnet node1 --to 10.78.0 --in 30
    move-subnet node1 --to 10.79.0 --to 10.80.0 --gap 3.5 --in 30

The moves are scheduled first, as the transient timer `e2e-move-subnet` in
the guest. A move adds the new address to the LAN NIC, then deletes the old
one: astrald polls its interface addresses every 3 s (`mod/ip`) and sees the
address set change, and the kernel would flush every route over a NIC that
lost its last IPv4 address. After each move the VM routes the 10.77 LAN and
the NAT aliases `198.51.100.0/24` (`enter-nat`, `go-public`) over its LAN NIC,
and before the move every other running VM routes each new subnet over its
own. The lab's LAN is one segment with no router, so on-link routes are what
keeps each side reachable from the other. SSH and the harness's apphost
tunnel ride the WAN NIC, so the harness reaches the VM throughout.

`/run/e2e-move-subnet.env` on the VM records its LAN NIC, its LAN subnet, its
octet and the address it holds, so a later call moves it on from where the
last one left it.

No link is closed: withdrawing an address disturbs no established
connection. A test that needs the dead links gone closes them itself.

`verify.py` checks, independently, that the move is pending, that the VM
still holds the address its state file names and none in a `--to` subnet,
and that every other running VM routes each `--to` subnet. What astrald does
once the VM moves is the test's oracle's to judge.
