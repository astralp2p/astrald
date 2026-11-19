#!/bin/sh
# verify stop-astrald: astrald is not active on any named VM.
set -eu
bad=0
while [ $# -gt 0 ]; do
  case "$1" in
    --vm)
      state=$(netsim ssh "$2" -- "systemctl is-active astrald" || true)
      if [ "$state" = "active" ]; then
        echo "stop-astrald verify FAILED: astrald is still active on $2" >&2; bad=1
      else
        echo "stop-astrald OK: astrald is $state on $2"
      fi
      shift 2 ;;
    *) shift ;;
  esac
done
exit $bad
