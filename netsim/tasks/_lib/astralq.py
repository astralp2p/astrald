#!/usr/bin/env python3
"""Host-side astral-query JSON extractors shared by the netsim run.sh scripts.

Reads `astral-query <op> -out json` output on stdin (produced inside a VM and
piped back over `netsim ssh`) and prints one extracted value, using the SAME
astral-py typed interrogators the verifiers use (astralapi) rather than a
bespoke inline `python3 -c` parser per call site. Host-side only (imports
astral-py); the in-VM Go astral-query stays as-is.

Usage (each reads the piped JSON on stdin):
  ... apphost.whoami -out json          | python3 astralq.py identity
  ... nodes.resolve_endpoints ... json  | python3 astralq.py onion
  ... nodes.links -out json             | python3 astralq.py remote-id
  ... nodes.links -out json             | python3 astralq.py has-link <network> <identity>
"""
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.realpath(__file__)))
import astralapi  # noqa: E402


def main(argv):
    if not argv:
        sys.stderr.write("usage: astralq.py <identity|onion|remote-id|has-link ...>\n")
        return 2
    cmd = argv[0]
    objs = astralapi.parse_cli(sys.stdin.read())

    if cmd == "identity":
        v = astralapi.identity_of(objs)
        if v:
            print(v)
    elif cmd == "onion":
        v = astralapi.resolve_onion(objs)
        if v:
            print(v)
    elif cmd == "remote-id":
        ids = astralapi.link_remote_identities(objs)
        if ids:
            print(ids[0])
    elif cmd == "has-link":
        if len(argv) < 3:
            sys.stderr.write("usage: astralq.py has-link <network> <identity>\n")
            return 2
        network, want = argv[1], argv[2]
        if any(rid == want for rid, _ in astralapi.links_by_network(objs, network)):
            print("yes")
    else:
        sys.stderr.write(f"astralq.py: unknown command {cmd!r}\n")
        return 2
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
