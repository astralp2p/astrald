# lab

Builds the shared test lab: two astrald nodes plus an AI operator.

- **Kind:** fixture · **Family:** foundation
- **Chain:** `null` → `astrald-lab`
- **Steps:** add-vm · install-astrald · install-qwen-code · configure-astral-agent
- **Run:** `netsim story --stage null --save astrald-lab netsim/scenarios/lab/lab.story`

Creates two virtual machines and installs astrald on each so they can work together over a network. It also sets up Qwen Code, an AI assistant, on the first machine with a skill for talking to astrald. This baseline is the foundation every other scenario starts from.
