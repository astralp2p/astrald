#!/bin/sh
# Thin shim — all verification logic lives in verify.py. netsim sets $NETSIM_TASK_DIR
# to this task's directory and only auto-runs run.sh/verify.sh, so verify.py sits
# next to us and is invoked here (the dirname fallback covers running this directly).
exec python3 "${NETSIM_TASK_DIR:-$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)}/verify.py" "$@"
