#!/usr/bin/env python3
"""verify restart-tor: Tor runs on <vm> now, and its stop (and start) is pending.

Host-side check over ssh, independent of the run: the tor unit, and the
tor@default instance where the guest has one, is active, the e2e-tor-stop
timer waits, and with --down above 0 the e2e-tor-start timer waits too.
# note: what astrald does while Tor is down and once it is back is the test's
#   oracle's to judge
"""
import argparse
import os
import sys

# why: realpath crosses netsim's per-task symlink to reach the sibling _lib
sys.path.insert(0, os.path.join(
    os.path.dirname(os.path.dirname(os.path.realpath(__file__))), "_lib"))
import vmops  # noqa: E402


def active(vm: str, unit: str) -> str:
    return (vmops.ssh(vm, f"systemctl is-active {unit}") or "").strip()


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--vm", default="node2")
    ap.add_argument("--down", type=int, default=0)
    args, _ = ap.parse_known_args()

    errors = []
    units = ["tor.service"]
    if (vmops.ssh(args.vm, "systemctl cat tor@default.service >/dev/null 2>&1 && echo yes")
            or "").strip() == "yes":
        units.insert(0, "tor@default.service")
    for unit in units:
        state = active(args.vm, unit)
        if state != "active":
            errors.append(f"{unit} on {args.vm} is not running ({state or '?'})")
    timers = ["e2e-tor-stop.timer"] + (["e2e-tor-start.timer"] if args.down > 0 else [])
    for timer in timers:
        state = active(args.vm, timer)
        if state != "active":
            errors.append(f"{timer} on {args.vm} is not pending ({state or '?'})")
    if vmops.report_errors(errors, "restart-tor"):
        return 1
    print(f"restart-tor OK: {' and '.join(units)} run on {args.vm}, and {' and '.join(timers)} wait")
    return 0


if __name__ == "__main__":
    sys.exit(main())
