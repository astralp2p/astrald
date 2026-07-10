# install-astrald

Builds `astrald` and `astral-query` from source and runs `astrald` as a systemd service on the target VMs (all running, or `--vm <host>`; `--ref` picks a git ref). verify.sh asserts the unit is enabled and each node answers `astral-query localnode:.spec`.
