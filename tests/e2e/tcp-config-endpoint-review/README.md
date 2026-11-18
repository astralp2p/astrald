# tcp-config-endpoint-review

Ad hoc docs review for astral-docs `protocols/tcp/README.md`, section
"Advertising a public or forwarded endpoint". Not shipped.

node2 carries `configEndpoints: ["198.51.100.12:1791"]` in `tcp.yaml`. After
`enter-nat node1 node2`, node2's astrald sits in a netns at 192.168.99.2
behind a NAT whose public alias 198.51.100.12 forwards inbound traffic into
the netns. The test checks that node1, a sibling, resolves the configured
endpoint for node2 with a TTL near 7 days, and that node1 links to node2
through that forwarded address.
