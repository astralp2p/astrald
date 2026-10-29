# lab.story — the astrald lab, built in one netsim simulation.
# start: null   save: e2e-lab-r<hash>, the first 8 hex of this file's sha256
# (a VM world bakes it on first use, so any edit here bakes a new stage)
# Result: a single stage with three nodes running astrald — node1, node2 and a
# reflector that belongs to no swarm — and a Qwen Code operator on node1,
# equipped with the astral-agent skill.
add-vm --hostname node1 --mem 2048
add-vm --hostname node2 --mem 2048
# why 2048 MiB: install-astrald builds astrald inside each guest, and
# modernc.org/sqlite/lib alone does not fit a 1 GiB default — the compiler is
# OOM-killed ("compile: signal: killed"), which is a flake this lab has always
# been one package away from. The host has 15 GiB; three guests at 2 GiB is
# not the constraint.
#
# why a third node: nat-punch needs a peer that is NOT behind NAT to observe a
# masqueraded node's public source address and reflect it back — two NAT'd
# peers cannot do that for each other before the punch. It runs astrald like
# the others and joins a roster only where a test names it (gateway-relay).
add-vm --hostname reflector --mem 2048
install-astrald
install-qwen-code --vm node1 --create-user
configure-astral-agent --vm node1
