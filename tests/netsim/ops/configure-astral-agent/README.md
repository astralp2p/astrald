# configure-astral-agent

On node1, the host clones the satforge/skills repo with a deploy key, builds the linker, and links the astral-agent skill into the Qwen operator at `~<user>/.qwen/skills/astral-agent`. verify.sh asserts the linked skill is present and owned by the operator.
