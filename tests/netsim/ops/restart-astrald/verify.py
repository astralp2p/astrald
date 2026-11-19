#!/usr/bin/env python3
"""verify restart-astrald: <vm>'s astrald runs, and its restart is pending.

Host-side check over ssh, independent of the run: the astrald unit is active
and the e2e-astrald-restart timer waits.
# note: what astrald does once it is back is the test's oracle's to judge
"""
import argparse
import os
import sys

# why: realpath crosses netsim's per-task symlink to reach the sibling _lib
sys.path.insert(0, os.path.join(
    os.path.dirname(os.path.dirname(os.path.realpath(__file__))), "_lib"))
import vmops  # noqa: E402


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--vm", default="node2")
    args, _ = ap.parse_known_args()

    errors = []
    unit = (vmops.ssh(args.vm, "systemctl is-active astrald") or "").strip()
    if unit != "active":
        errors.append(f"astrald on {args.vm} is not running ({unit or '?'})")
    timer = (vmops.ssh(args.vm, "systemctl is-active e2e-astrald-restart.timer") or "").strip()
    if timer != "active":
        errors.append(f"{args.vm}'s restart is not pending (e2e-astrald-restart.timer: {timer or '?'})")
    if vmops.report_errors(errors, "restart-astrald"):
        return 1
    print(f"restart-astrald OK: astrald runs on {args.vm}, and its restart is pending")
    return 0


if __name__ == "__main__":
    sys.exit(main())
