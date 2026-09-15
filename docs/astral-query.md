# Using astral-query

## Overview

`astral-query` is a command-line tool for sending queries to the Astral network. It executes methods exposed by modules on local or remote nodes.

## Installation

```shell
go install ./cmd/astral-query
```

## Basic Syntax

```
astral-query [caller@][target:]method [-arg value]...
```

**Components:**
- `caller@` - Optional identity making the query
- `target:` - Optional target identity (defaults to `ASTRAL_DEFAULT_TARGET`)
- `method` - Required method name (e.g., `user.info`, `nodes.add_endpoint`)
- `-arg value` - Optional method arguments as flag pairs

## Examples

Query user information:
```shell
astral-query user.info
```

List object repositories with JSON output:
```shell
astral-query objects.repositories -out json
```

Create a NAT traversal to a remote node:
```shell
astral-query nat.new_traversal -target 02abc...def
```

Add a TCP endpoint for a node:
```shell
astral-query nodes.add_endpoint -identity 02abc...def -endpoint tcp:192.168.1.10:8080
```

## Input Formats

The `-in` flag specifies the input format for data sent to queries.

### JSON Input (`-in json`)

Pass the full JSON representation of an astral type:

```shell
astral-query tree.set -path /mod/tcp/listen -in json
{"Type":"bool","Object":true}
```

### Text Input (`-in text`)

Text format uses the syntax:

```
#[type] value
```

Where `value` is parsed by the type's `UnmarshalText` implementation.

Examples:

```shell
astral-query tree.set -path /mod/tcp/listen -in text
#[bool] true
```

```shell
astral-query tree.set -path /mod/tcp/listen -in text
#[bool] false
```

### Binary Payload with Base64

For arbitrary binary payloads, use colon followed by base64-encoded data:

```
#[type]:base64data
```

Example:

```shell
astral-query some.method -in text
#[bytes]:SGVsbG8gV29ybGQ=
```

## Environment Variables

- `ASTRAL_DEFAULT_TARGET` - Default target identity if not specified
- `ASTRALD_APPHOST_TOKEN` - Authentication token for apphost connections

## Output Formats

Many commands support `-out` flag for output formatting:

```shell
astral-query objects.repositories -out json
astral-query nat.list_pairs -out json | jq
```

## Tips

- Use identities or aliases
- Pipe output to `jq` for JSON formatting and filtering
- Most operations return streaming results until interrupted (Ctrl+C)
- Check module documentation for available methods and arguments
