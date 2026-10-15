# fwd

`fwd` lets you tunnel traffic between networks.

## Configuration

The config file for the module is `fwd.yaml`.

### Basics

This module lets you start forwarders. A forwarder creates a server on a
network and forwards all incoming connections to the target address.

Currently, TCP and astral servers/targets are supported. Additionally,
you can provide a Tor address as the target.

Forwarders are defined in the config file and start with the node.

### Astral targets

An astral target has the form:

```text
astral://[caller@][target:]query[?args]
```

* `caller` — identity or alias the query is sent as; defaults to the node.
* `target` — identity or alias the query is sent to; defaults to the node.
* `query` — the query path, for example `ssh`.
* `args` — optional query arguments, passed through as part of the query.

The `caller@` and `target:` prefixes are only recognized before the first
`?`, so arguments may contain `@` and `:`. For example,
`astral://svc?addr=192.0.2.1:80` sends `svc?addr=192.0.2.1:80` to the
node itself, and `astral://alice@bob:svc?to=tcp:1.2.3.4:80` sends
`svc?to=tcp:1.2.3.4:80` from `alice` to `bob`.

### Config file

Each entry maps a server address to a target address:

```yaml
forwards:
  "astral://ssh": "tcp://127.0.0.1:22"
  "tcp://127.0.0.1:8080": "astral://alias:http"
  "astral://hidden": "tor://cyl3gwxjmn4mhohlpufat5n25nnm6axrb3f7i3mvoaz3cpidypmihxe5.onion:8080"
```
