# lan-outage

Takes `--vm` (default `node2`) off the 10.77 LAN now and brings it back at the
same address after `--for` seconds (default 420). The return is scheduled
first, as the transient timer `e2e-lan-return` in the guest, so the node comes
back while the driver watches and even if the cut stops half way. The cut is
leave-lan's: the address is flushed and the NIC taken down, which astrald sees
as leaving the network. An `unreachable` route into the LAN makes the node's
own dials fail at once instead of leaving through slirp. Every link that rode
the address is closed on every VM, so each side's link task sees the drop.

`verify.py` checks, independently, that the node holds no LAN address, its
route into the LAN is unreachable, its return is pending, and no VM lists a
link to it. What astrald does once it is back is the test's oracle's to judge.

Unlike leave-lan, the world is whole again after `--for` seconds; the op still
ends its world for anything but its test's descendants, as every op does.
