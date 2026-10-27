# astrald

`astrald` is an astral node launcher. On the first run it creates a config directory for config files
and a data directory for private keys, databases and crash logs:

| Platform | Config directory                                         | Data directory                                               |
|----------|----------------------------------------------------------|--------------------------------------------------------------|
| Linux    | `$XDG_CONFIG_HOME/astrald`, else `$HOME/.config/astrald` | `$XDG_DATA_HOME/astrald`, else `$HOME/.local/share/astrald` |
| macOS    | `$HOME/Library/Application Support/astrald`              | the config directory                                         |
| Windows  | `%AppData%\astrald`                                      | the config directory                                         |

`astrald -root <dir>` uses `<dir>/config` and `<dir>/data` instead, on every platform. A relative `<dir>` is
resolved against the working directory at startup.

A relative `$XDG_CONFIG_HOME` or `$XDG_DATA_HOME` is ignored, and a relative `$HOME` counts as unset. Without
`-root`, `astrald` exits with an error when no absolute directory is found. A path in a config file is absolute or
starts with `~/`; a relative one is an error that names the file and the key.

Removing the data directory resets the node to a new identity. On macOS and Windows the data directory is the
config directory, so removing it also removes the config files.
