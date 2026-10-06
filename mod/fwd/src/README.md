# fwd

`fwd` lets you tunnel traffic between networks.

## Configuration

The config file for the module is `fwd.yaml`.

### Basics

This module lets you start forwarders. A forwarder creates a server on a
network and forwards all incoming connections to the target address.

Currently, TCP and astral servers/targets are supported. Additionally,
you can provide a Tor address as the target.

You can start a forwarder from the admin console or via the config file.

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

### Admin panel

Connect to the admin panel:

```text
$ anc q admin
demo@demo>
```

Start a forward from astral service `ssh` to local port 22:

```text
demo@demo> fwd start astral://ssh tcp://127.0.0.1:22
```

Start a forward the other way:

```text
demo@demo> fwd start tcp://127.0.0.1:8022 astral://demo:ssh
```

Forward astral server to a Tor address:

```text
demo@demo> fwd start astral://hidden tor://cyl3gwxjmn4mhohlpufat5n25nnm6axrb3f7i3mvoaz3cpidypmihxe5.onion:8080
```

### Config file

To start forwarders automatically with the node, add their definitions to
the config file:

```yaml
forwards:
  "astral://ssh": "tcp://127.0.0.1:22"
  "tcp://127.0.0.1:8080": "astral://alias:http"
```

### Stopping a service

Use the stop command to stop service by its server address:

```text
demo@demo> fwd stop astral://ssh
```
