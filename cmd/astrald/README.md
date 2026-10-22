# astrald

`astrald` is an astral node launcher. On the first run it creates a config directory for config files
and a data directory for private keys, databases and crash logs:

| Platform | Config directory                                         | Data directory                                               |
|----------|----------------------------------------------------------|--------------------------------------------------------------|
| Linux    | `$XDG_CONFIG_HOME/astrald`, else `$HOME/.config/astrald` | `$XDG_DATA_HOME/astrald`, else `$HOME/.local/share/astrald` |
| macOS    | `$HOME/Library/Application Support/astrald`              | the config directory                                         |
| Windows  | `%AppData%\astrald`                                      | the config directory                                         |

`astrald -root <dir>` uses `<dir>/config` and `<dir>/data` instead, on every platform.

Removing the data directory resets the node to a new identity. On macOS and Windows the data directory is the
config directory, so removing it also removes the config files.
