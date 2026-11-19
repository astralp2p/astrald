#!/usr/bin/env python3
"""verify configure-gateway: each client registered with the gateway from its config alone.

Host-side check over ssh, independent of the run: in each client's journal the
first registration attempt after the restart logs `bound to`, within
BIND_LIMIT seconds of `starting to maintain connections to`, and the
gateway's `gateway.node_list` lists the client.
# why `bound to` is the first-attempt proof: the task logs `bound to` only when
#   its first Register succeeds; a success after a retry logs `rebound to`
#   (mod/gateway/src/maintain_gateway_connections_task.go)
# why it proves the config path: run.sh gives a client no nodes.add_endpoint,
#   so the gateway's row comes from the gateway.yaml entry alone
"""
import argparse
import json
import os
import re
import sys
import time

# why: realpath crosses netsim's per-task symlink to reach the sibling _lib
sys.path.insert(0, os.path.join(
    os.path.dirname(os.path.dirname(os.path.realpath(__file__))), "_lib"))
import vmops  # noqa: E402

BIND_LIMIT = 5.0   # seconds: the first retry of the registration task waits 1 s
WAIT = 60          # seconds to wait for the client's first `bound to`
START = "starting to maintain connections to"
BOUND = re.compile(r"(?<!re)bound to ")


def bind_delay(vm: str):
    """Seconds from the last task start to the next `bound to`, or None."""
    out = vmops.ssh(vm, "journalctl -u astrald -o short-unix --no-pager -n 5000")
    start = None
    for line in out.splitlines():
        stamp = line.split(" ", 1)[0]
        if START in line:
            start = float(stamp)
        elif start is not None and BOUND.search(line):
            return float(stamp) - start
    return None


def listed(gw: str) -> list:
    """The identities the gateway's public list holds."""
    seen = []
    for line in vmops.query(gw, "gateway.node_list").splitlines():
        try:
            seen.append(str(json.loads(line).get("Object")))
        except (json.JSONDecodeError, AttributeError):
            continue
    return seen


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--vm", action="append", default=[])
    ap.add_argument("--gateway", default="")
    args, _ = ap.parse_known_args()

    bad = False
    for vm in args.vm:
        errs = []
        delay = None
        end = time.monotonic() + WAIT
        while delay is None and time.monotonic() < end:
            delay = bind_delay(vm)
            if delay is None:
                time.sleep(2)
        if delay is None:
            errs.append(f"no `bound to` after the registration task started, in {WAIT} s")
        elif delay > BIND_LIMIT:
            errs.append(f"`bound to` {delay:.2f} s after the task started, over {BIND_LIMIT} s")

        identity = os.environ.get(f"ASTRAL_ID_{vm}", "")
        if args.gateway and identity and identity not in listed(args.gateway):
            errs.append(f"{args.gateway}'s gateway.node_list does not list {vm}")

        if vmops.report_errors(errs, f"configure-gateway on {vm}"):
            bad = True
        else:
            print(f"configure-gateway OK: {vm} bound to {args.gateway} "
                  f"{delay:.2f} s after its task started, from its config alone")
    return 1 if bad else 0


if __name__ == "__main__":
    sys.exit(main())
