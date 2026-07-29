#!/bin/sh
# Thin shim — all verification logic lives in verify.py. Calling astral-query and
# walking its JSON streams is far cleaner in python than bash, so verify.sh just
# hands off. netsim sets $NETSIM_TASK_DIR to this task's directory and only
# auto-runs run.sh/verify.sh, so verify.py sits next to us and is invoked here
# (the dirname fallback covers running this script directly).
exec python3 "${NETSIM_TASK_DIR:-$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)}/verify.py" "$@"
