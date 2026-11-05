# The image every node of a Docker world runs (tests/lib/worlds/docker.py).
#
# It mirrors the lab's node1 (netsim/labs/two-node-plus-external/lab.story):
# the operator, tester, with Qwen Code configured as netsim's
# install-qwen-code configures it, so lib/agent.py drives a container with the
# scripts it sends a VM. Every node runs this one image; on nodes other than
# node1 the operator is simply never started.
#
# astrald and astral-query are not baked in: the world copies in the ones
# built from the checkout under test, as a VM world pushes them.
FROM node:22-bookworm-slim

# why python3: Operator._set_model rewrites the operator's settings with it.
# why procps: Operator._stop kills the operator with pkill, and the world
# watches astrald with pgrep.
RUN apt-get update \
    && apt-get install -y --no-install-recommends python3 procps ca-certificates git \
    && rm -rf /var/lib/apt/lists/*

RUN useradd -m -s /bin/bash tester

# why pinned: the lab installs @latest and records no version; a Docker world
# names the version it ran (QWEN_CODE in lib/worlds/docker.py)
ARG QWEN_CODE_VERSION
RUN su - tester -c "npm install --global --quiet --prefix \$HOME/.local @qwen-code/qwen-code@${QWEN_CODE_VERSION}"

# the lab's endpoint and model (lab.story's install-qwen-code line); config.toml's
# [agent] model rewrites them per world, as on node1
ARG BASE_URL=http://spark0.hq.satforge.dev:8000/v1
ARG MODEL=deepseek-v4.1-flash-1017
COPY qwen-settings.sh /tmp/qwen-settings.sh
RUN u=tester base_url="$BASE_URL" model="$MODEL" sh /tmp/qwen-settings.sh \
    && rm /tmp/qwen-settings.sh

# The operator's astral-agent skill: the Astral docs from their public repo,
# at the commit the astrald under test pins in .ai/system (docs_ref in
# lib/worlds/docker.py), and a short SKILL.md that points into them. A host
# skill ([agent] skill in tests/config.toml) replaces only the SKILL.md and
# its references at world start; the docs stay these.
# why the pinned commit: the docs then describe the astrald being tested
ARG ASTRAL_DOCS_REF
ARG ASTRAL_DOCS_REPO=https://github.com/astralp2p/astral-docs.git
COPY astral-agent.md /tmp/astral-agent.md
RUN test -n "$ASTRAL_DOCS_REF" \
    && d=/home/tester/.qwen/skills/astral-agent \
    && install -d "$d" \
    && git init -q "$d/astral-docs" \
    && git -C "$d/astral-docs" fetch -q --depth 1 "$ASTRAL_DOCS_REPO" "$ASTRAL_DOCS_REF" \
    && git -C "$d/astral-docs" checkout -q FETCH_HEAD \
    && rm -rf "$d/astral-docs/.git" \
    && install -m 644 /tmp/astral-agent.md "$d/SKILL.md" \
    && rm /tmp/astral-agent.md \
    && chown -R tester:tester /home/tester/.qwen

# why root-only: astrald's root holds the node's token, which the operator,
# tester, must not read
RUN install -d -m 700 /var/lib/astrald

CMD ["sleep", "infinity"]
