#!/bin/sh
# verify import-user-software-key: the node must be a User node under the imported software User.
# INDEPENDENT re-check -- reads $HOME/user.json, acts AS the User, and asserts the
# node answers as a user node. If ASTRAL_USER_ID is set, the derived User id must
# equal it (proof the EXISTING key was used, not a fresh one).
set -eu

VM="node1"
while [ $# -gt 0 ]; do
  case "$1" in
    --vm) VM=$2; shift 2 ;;
    *)    shift ;;
  esac
done
EXPECT=${ASTRAL_USER_ID:-}

REMOTE_CHECK=$(cat <<'EOS'
set -eu
info=/home/tester/user.json
[ -s "$info" ] || { echo "no $info on $(hostname)" >&2; exit 1; }
uid=$(python3 -c 'import json;print(json.load(open("/home/tester/user.json")).get("user_id",""))')
ASTRALD_APPHOST_TOKEN=$(python3 -c 'import json;print(json.load(open("/home/tester/user.json")).get("user_token",""))')
export ASTRALD_APPHOST_TOKEN
[ -n "$uid" ]                   || { echo "no user_id in $info on $(hostname)"    >&2; exit 1; }
[ -n "$ASTRALD_APPHOST_TOKEN" ] || { echo "no user_token in $info on $(hostname)" >&2; exit 1; }

# if an expected User id was supplied, the imported key must derive exactly it
if [ -n "$expect" ] && [ "$uid" != "$expect" ]; then
  echo "imported User id $uid != expected $expect on $(hostname) (wrong key derived?)" >&2
  exit 1
fi

# acting as the User: whoami must report the User identity
who=$(astral-query apphost.whoami -out json) \
  || { echo "apphost.whoami failed on $(hostname)" >&2; exit 1; }
echo "$who" | grep -q "$uid" \
  || { echo "whoami != User id on $(hostname): $who" >&2; exit 1; }

# active contract present (user.info rejects with code 2 if none)
astral-query user.info -out json \
  || { echo "user.info failed on $(hostname) -- no active contract?" >&2; exit 1; }

echo "$(hostname): user node OK (User $uid${expect:+ — matches expected})"
EOS
)

netsim ssh "$VM" -- "expect='$EXPECT'; $REMOTE_CHECK" \
  || { echo "import-user-software-key verify FAILED on $VM" >&2; exit 1; }
echo "verified imported user node on: $VM"
