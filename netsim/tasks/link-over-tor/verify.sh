#!/bin/sh
# Thin shim — verification logic lives in verify.py.
exec python3 "${NETSIM_TASK_DIR:-$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)}/verify.py" "$@"
