# add-bridge

Gives each named VM a bridge interface `--name` (default `docker0`) that
holds the address `--cidr` (default `172.17.0.1/16`), as Docker's default
bridge does on a host that runs containers.

    add-bridge node2 --name docker0 --cidr 172.17.0.1/16

On each VM the op adds the bridge unless it exists, replaces its address
with `--cidr` and sets it up. The bridge has no ports and carries no
traffic: its address among the VM's interface addresses is what astrald
reads.

astrald drops the addresses of host-only interfaces, container and
virtual-machine bridges, from the endpoints it advertises, and recognises
such an interface by its name (`mod/ip/src/advertisable.go`). The op
therefore refuses a `--name` that does not start with one of
`docker`, `podman`, `cni`, `virbr`, `veth`, `lxcbr`, `lxdbr`, `vboxnet`,
`vmnet`, and is not `br-` followed by 12 hex digits.

`verify.py` checks, independently, that `ip -o addr show dev <name>` on each
VM lists `--cidr`. Whether astrald advertises the address is the test's
oracle's to judge.

The bridge stays for the world. The op writes no module config.
