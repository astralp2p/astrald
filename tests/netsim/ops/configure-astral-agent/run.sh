#!/bin/sh
# configure-astral-agent: install the astral-agent skill into the Qwen Code operator; the
# VM clones the private satforge/skills repo with an injected deploy key and runs the linker.
#   configure-astral-agent [--vm <host>] [--user <name>]   (default: --vm node1 --user tester)
#
# why: the host owns the deploy key so the VM never needs git credentials of its own; run.sh
#   base64-ships the key from $SATFORGE_SKILLS_DEPLOY_KEY over a single netsim ssh argv, then
#   the guest installs it, clones over SSH, builds the linker, and links astral-agent for qwen.
# note: the deploy key is LEFT in the VM (lets the operator re-clone/pull later), so it also
#   lives in the saved snapshot. May switch to wiping it pre-snapshot if that exposure matters.
set -eu

VM=node1
USER_NAME=tester
while [ $# -gt 0 ]; do
  case "$1" in
    --vm)   [ $# -ge 2 ] || { echo "need host after --vm" >&2; exit 64; }; VM=$2; shift 2 ;;
    --user) [ $# -ge 2 ] || { echo "need name after --user" >&2; exit 64; }; USER_NAME=$2; shift 2 ;;
    *) echo "usage: configure-astral-agent [--vm <host>] [--user <name>]" >&2; exit 64 ;;
  esac
done

REPO=${SATFORGE_SKILLS_REPO:-ssh://git@git.satforge.dev/satforge/skills.git}
REF=${SATFORGE_SKILLS_REF:-}      # note: optional branch/tag/sha; default is clone's default branch
KEY=${SATFORGE_SKILLS_DEPLOY_KEY:-}
[ -n "$KEY" ] || { echo "set SATFORGE_SKILLS_DEPLOY_KEY to the deploy key path for $REPO" >&2; exit 1; }
[ -r "$KEY" ] || { echo "deploy key not readable: $KEY" >&2; exit 1; }
key_b64=$(base64 -w0 "$KEY")

REMOTE_BODY=$(cat <<'EOS'
set -eu
home=$(getent passwd "$u" | cut -d: -f6)
[ -n "$home" ] || { echo "user '$u' not found on $(hostname)" >&2; exit 1; }
command -v git >/dev/null 2>&1 || { echo "git missing on $(hostname)" >&2; exit 1; }

install -d -m 700 -o "$u" -g "$u" "$home/.ssh" "$home/.netsim"
printf '%s' "$key_b64" | base64 -d > "$home/.ssh/skills_deploy"
chmod 600 "$home/.ssh/skills_deploy"
chown "$u:$u" "$home/.ssh/skills_deploy"

# Guest-side provisioning, run as the operator. Quoted heredoc: fully literal;
# repo arrives as a positional arg. The git host's key is auto-accepted on first
# connect (StrictHostKeyChecking=accept-new).
cat > "$home/.netsim/setup-skill.sh" <<'SCRIPT'
#!/bin/sh
set -eu
export PATH=/usr/local/go/bin:$PATH
export GIT_SSH_COMMAND="ssh -i $HOME/.ssh/skills_deploy -o IdentitiesOnly=yes -o StrictHostKeyChecking=accept-new"
repo=$1
ref=$2
src=$HOME/satforge-skills
[ -d "$src/.git" ] || git clone --recurse-submodules "$repo" "$src"
cd "$src"
if [ -n "$ref" ]; then
  # Fail loudly if the ref can't be fetched -- otherwise we'd silently link the
  # default-branch skill (missing whatever the ref was supposed to add).
  git fetch --quiet origin "$ref"
  git rev-parse --verify --quiet "origin/$ref" >/dev/null \
    || { echo "skills ref '$ref' not found on origin" >&2; exit 1; }
  git checkout --quiet -B "$ref" "origin/$ref"
  git reset --hard --quiet "origin/$ref"
else
  git pull --ff-only --quiet 2>/dev/null || true
fi
git submodule update --init --recursive --quiet
go build -C bin/satforge-skills -o satforge-skills .
bin="$src/bin/satforge-skills/satforge-skills"
"$bin" unlink astral-agent --target qwen >/dev/null 2>&1 || true   # idempotent re-run
"$bin" link astral-agent --target qwen
SCRIPT
chown "$u:$u" "$home/.netsim/setup-skill.sh"

su - "$u" -c "sh '$home/.netsim/setup-skill.sh' '$repo' '$ref'"
echo "configure-astral-agent: $(hostname) cloned skills + linked astral-agent (deploy key left in place)"
EOS
)

echo "configure-astral-agent: injecting deploy key + linking on $VM (user $USER_NAME) ..."
netsim ssh "$VM" -- "u='$USER_NAME' key_b64='$key_b64' repo='$REPO' ref='$REF'; $REMOTE_BODY"
echo "configure-astral-agent: done on $VM"
