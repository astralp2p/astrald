# lab.story — the astrald lab, built in one netsim simulation.
# start: null   save: astrald-lab
# Result: a single stage with two nodes running astrald and a Qwen Code
# operator on node1, equipped with the astral-agent skill.
add-vm --hostname node1
add-vm --hostname node2
install-astrald
install-qwen-code --vm node1 --create-user
configure-astral-agent --vm node1
