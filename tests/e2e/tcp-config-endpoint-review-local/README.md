# tcp-config-endpoint-review-local

Ad hoc docs review for astral-docs `protocols/tcp/README.md`, section
"Advertising a public or forwarded endpoint". Not shipped.

node2 carries `configEndpoints: ["127.0.0.1:26791"]` in `tcp.yaml`. The
driver serves a TCP port forward from 127.0.0.1:26791 to node2's tcp listen
port, checks that node1, a sibling, resolves the configured endpoint for node2
with a TTL near 7 days, and links node1 to node2 through the forward.
