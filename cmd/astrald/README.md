# astrald

`astrald` is an astral node launcher. On the first run it creates a config directory for config files
(`$HOME/.config/astrald` on Linux) and a data directory for private keys, databases and crash logs
(`$HOME/.local/share/astrald` on Linux). `astrald -root <dir>` uses `<dir>/config` and `<dir>/data` instead.