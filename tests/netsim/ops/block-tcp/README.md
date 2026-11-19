# block-tcp

Drops every tcp packet between the named VMs on the 10.77 LAN and closes the
tcp links between them. Every other path stays: kcp over UDP among them, SSH
and the harness's apphost tunnel over the WAN NIC.

    block-tcp <vm> <vm>...

The rules live in the nft table `e2e_block_tcp` on each named VM: a set of the
named VMs' LAN addresses, and input and output chains that drop tcp to and
from it. A dropped SYN is what a filtered network does, so a tcp dial waits
instead of failing. An established tcp link has no keepalive that sees the
drop soon, so the op closes every tcp link one named VM holds to another.

`verify.py` checks, independently, that each named VM holds the table with
the others' addresses, that a tcp connect from each to another's astrald port
(1791) does not complete within 3 s, and that no named VM lists a tcp link to
another. What astrald does over the paths left is the test's oracle's to
judge.
