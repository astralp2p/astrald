astrald
=======

## Overview

**The Astral Network** is an abstract, general-purpose peer-to-peer network layer that runs on top of multiple transports. **astrald** is a daemon that provides easy access to the network via local interface. It provides connectivity, identity, authentication, encryption and a set of core protocols apps and agents can use to easily interact over the network. The full documentation is [here](https://github.com/astralp2p/astral-docs/blob/master/README.md).

## Network architecture

All actors (nodes, apps, users and agents) on the network have a self-assigned identity based on a ECDSA key on the secp256k1 curve, the same as the Lightning Network or Bitcoin. Nodes form the core infrastructure of the network by establish encrypted and authenticated links with each other, exchanging certificates, gossip data and organizing complex p2p processes. Apps, agents and users access the network via the API exposed by the daemon via varoius transports - astral apphost protocol, HTTP, WSS, etc.

Actors send each other queries (similar to HTTP query path) over the network that can be rejected or accepted to establish a connection. The connection is then used to exchange data in form of Objects (or just a raw bytestream).

## Quick start

Install the daemon and the tooling:

```shellsession
go install \
  github.com/astralp2p/astrald/cmd/astrald@latest \
  github.com/astralp2p/astrald/cmd/astral-query@latest
```

Run the daemon:

```shellsession
$ astrald
```

The node will start and generate a new identity and an alias for itself. Once the node has started verify it is accepting local API calls:

```shellsession
$ astral-query localnode:.spec
```

## Generating user identity

A new node runs without a user. One `user.bootstrap` call claims it for a user and returns an access token for that user. The [Node Setup](https://github.com/astralp2p/astral-docs/blob/master/topics/node-setup.md) topic documents the user key, who may claim a node, the step-by-step flow, and joining a swarm from a new node with `user.join`.

A session without a token reaches the setup ops before the claim, and only `user.info` and `apphost.register` after it ([apphost](https://github.com/astralp2p/astral-docs/blob/master/protocols/apphost/README.md#sessions-without-a-token)). Every other op needs a token.

### New identity

The node generates 24 words and returns the user they derive, and claims nothing. Write the words down: they are the user's backup. The same words then claim the node.

```shellsession
$ astral-query user.derive_identity -generate true -out json
{"Type":"mod.user.derive_identity_result","Object":{"Identity":"02e96bd9...","Mnemonic":"penalty stadium garlic ... rifle fitness garage"}}
$ echo '{"Type":"string16","Object":"penalty stadium garlic ... rifle fitness garage"}' | astral-query user.bootstrap -in json -out json
{"Type":"mod.user.bootstrap_result","Object":{"Identity":"02e96bd9...","Mnemonic":"","Token":"b9c2e1a3d4f5867a"}}
```

### Existing identity

The node claims itself for the user that the given BIP-39 words derive. `user.derive_identity` with the same words previews that user first.

```shellsession
$ echo '{"Type":"string16","Object":"horse soldier ... term change"}' | astral-query user.bootstrap -in json -out json
{"Type":"mod.user.bootstrap_result","Object":{"Identity":"02564ba7...","Mnemonic":"","Token":"b9c2e1a3d4f5867a"}}
```

Check the claim as the user; `user.info` names the user and the node:

```shellsession
$ ASTRALD_APPHOST_TOKEN=b9c2e1a3d4f5867a astral-query user.info -out json
```

## Generating guest identity for apps and AI agents

To create a new astral identity along with an access token for it, send a `apphost.register` query to the local node. It will send back a single `apphost.access_token` object:

```shellsession
$ astral-query apphost.register -out json | jq
{
  "Type": "apphost.access_token",
  "Object": {
    "Identity": "026923d06a51098170093fe989d30a432283f56d89d307176fd6f947c3a9d285ff",
    "Token": "Kaz3No8nYVTufIBZ6ViQsypc93SiYWJf",
    "ExpiresAt": "2036-05-17T17:41:49.042372148Z"
  }
}
```

Then use the token via env:

```shellsession
$ ASTRALD_APPHOST_TOKEN=Kaz3No8nYVTufIBZ6ViQsypc93SiYWJf astral-query apphost.whoami -out json
{"Type":"identity","Object":"026923d06a51098170093fe989d30a432283f56d89d307176fd6f947c3a9d285ff"}
```

Or over any of the supported transports (native or [HTTP](https://github.com/astralp2p/astral-docs/blob/master/topics/http-transport.md)).

## Hardware identity

Astral can use BIP-0137-compatible hardware wallets as the source for user identities. See the [coldcard module](mod/coldcard/README.md).