#!/bin/sh
# Write the operator's Qwen Code settings as netsim's install-qwen-code does
# (satforge/netsim tasks/install-qwen-code/run.sh): the same keys, owner and
# modes, so lib/agent.py's model rewrite finds what it finds on node1.
#   u=<user> base_url=<url> model=<name> sh qwen-settings.sh
set -eu
home=$(getent passwd "$u" | cut -d: -f6)
install -d -m 700 -o "$u" -g "$u" "$home/.qwen"
cat > "$home/.qwen/settings.json" <<JSON
{
  "security": { "auth": { "selectedType": "openai" } },
  "modelProviders": {
    "openai": [
      {
        "id": "$model",
        "name": "$model",
        "baseUrl": "$base_url",
        "envKey": "OPENAI_API_KEY"
      }
    ]
  },
  "model": { "name": "$model" }
}
JSON
cat > "$home/.qwen/.env" <<ENV
OPENAI_API_KEY=netsim-local
OPENAI_BASE_URL=$base_url
OPENAI_MODEL=$model
ENV
chown "$u:$u" "$home/.qwen/settings.json" "$home/.qwen/.env"
chmod 600 "$home/.qwen/.env"
