# apphost

apphost provides APIs for apps to connect to and interact with the node.

## Configuration

The config file for the module is `apphost.yaml`.

### Listen

You can specify the endpoints on which apphost should listen for incoming app
connections:

```yaml
listen:
  - "unix:~/.apphost.sock"
  - "tcp:127.0.0.1:8625"
```

For now `tcp` and `unix` sockets are supported.


### Access tokens

You can define static access tokens for identities:

```yaml
tokens:
  mysecrettoken: demo
```

An app can use access tokens to authenticate to the apphost module. Default
apps (such as anc) will use the token from ASTRALD_APPHOST_TOKEN env variable:

```shell
$ export ASTRALD_APPHOST_TOKEN="mysecrettoken"
$ astral-query user.info
```
